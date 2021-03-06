package xfer

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/distribution/digest"
	"github.com/docker/docker/layer"
	"github.com/docker/docker/pkg/progress"
	"golang.org/x/net/context"
)

const maxUploadConcurrency = 3

type mockUploadDescriptor struct {
	currentUploads  *int32
	diffID          layer.DiffID
	simulateRetries int
}

// Key returns the key used to deduplicate downloads.
func (u *mockUploadDescriptor) Key() string {
	return u.diffID.String()
}

// ID returns the ID for display purposes.
func (u *mockUploadDescriptor) ID() string {
	return u.diffID.String()
}

// DiffID should return the DiffID for this layer.
func (u *mockUploadDescriptor) DiffID() layer.DiffID {
	return u.diffID
}

// Upload is called to perform the upload.
func (u *mockUploadDescriptor) Upload(ctx context.Context, progressOutput progress.Output) error {
	if u.currentUploads != nil {
		defer atomic.AddInt32(u.currentUploads, -1)

		if atomic.AddInt32(u.currentUploads, 1) > maxUploadConcurrency {
			return errors.New("concurrency limit exceeded")
		}
	}

	// Sleep a bit to simulate a time-consuming upload.
	for i := int64(0); i <= 10; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
			progressOutput.WriteProgress(progress.Progress{ID: u.ID(), Current: i, Total: 10})
		}
	}

	if u.simulateRetries != 0 {
		u.simulateRetries--
		return errors.New("simulating retry")
	}

	return nil
}

func uploadDescriptors(currentUploads *int32) []UploadDescriptor {
	return []UploadDescriptor{
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:cbbf2f9a99b47fc460d422812b6a5adff7dfee951d8fa2e4a98caa0382cfbdbf"), 0},
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:1515325234325236634634608943609283523908626098235490238423902343"), 0},
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:6929356290463485374960346430698374523437683470934634534953453453"), 0},
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:cbbf2f9a99b47fc460d422812b6a5adff7dfee951d8fa2e4a98caa0382cfbdbf"), 0},
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:8159352387436803946235346346368745389534789534897538734598734987"), 1},
		&mockUploadDescriptor{currentUploads, layer.DiffID("sha256:4637863963478346897346987346987346789346789364879364897364987346"), 0},
	}
}

var expectedDigests = map[layer.DiffID]digest.Digest{
	layer.DiffID("sha256:cbbf2f9a99b47fc460d422812b6a5adff7dfee951d8fa2e4a98caa0382cfbdbf"): digest.Digest("sha256:c5095d6cf7ee42b7b064371dcc1dc3fb4af197f04d01a60009d484bd432724fc"),
	layer.DiffID("sha256:1515325234325236634634608943609283523908626098235490238423902343"): digest.Digest("sha256:968cbfe2ff5269ea1729b3804767a1f57ffbc442d3bc86f47edbf7e688a4f36e"),
	layer.DiffID("sha256:6929356290463485374960346430698374523437683470934634534953453453"): digest.Digest("sha256:8a5e56ab4b477a400470a7d5d4c1ca0c91235fd723ab19cc862636a06f3a735d"),
	layer.DiffID("sha256:8159352387436803946235346346368745389534789534897538734598734987"): digest.Digest("sha256:5e733e5cd3688512fc240bd5c178e72671c9915947d17bb8451750d827944cb2"),
	layer.DiffID("sha256:4637863963478346897346987346987346789346789364879364897364987346"): digest.Digest("sha256:ec4bb98d15e554a9f66c3ef9296cf46772c0ded3b1592bd8324d96e2f60f460c"),
}

func TestSuccessfulUpload(t *testing.T) {
	lum := NewLayerUploadManager(maxUploadConcurrency)

	progressChan := make(chan progress.Progress)
	progressDone := make(chan struct{})
	receivedProgress := make(map[string]int64)

	go func() {
		for p := range progressChan {
			receivedProgress[p.ID] = p.Current
		}
		close(progressDone)
	}()

	var currentUploads int32
	descriptors := uploadDescriptors(&currentUploads)

	err := lum.Upload(context.Background(), descriptors, progress.ChanOutput(progressChan))
	if err != nil {
		t.Fatalf("upload error: %v", err)
	}

	close(progressChan)
	<-progressDone
}

func TestCancelledUpload(t *testing.T) {
	lum := NewLayerUploadManager(maxUploadConcurrency)

	progressChan := make(chan progress.Progress)
	progressDone := make(chan struct{})

	go func() {
		for range progressChan {
		}
		close(progressDone)
	}()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-time.After(time.Millisecond)
		cancel()
	}()

	descriptors := uploadDescriptors(nil)
	err := lum.Upload(ctx, descriptors, progress.ChanOutput(progressChan))
	if err != context.Canceled {
		t.Fatal("expected upload to be cancelled")
	}

	close(progressChan)
	<-progressDone
}
