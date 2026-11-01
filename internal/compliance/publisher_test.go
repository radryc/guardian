package compliance

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type fakePutter struct {
	mu       sync.Mutex
	calls    int
	failures int
	inputs   []*s3.PutObjectInput
	err      error
}

func (f *fakePutter) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.inputs = append(f.inputs, in)
	if f.failures > 0 {
		f.failures--
		if f.err != nil {
			return nil, f.err
		}
		return nil, errors.New("temporary failure")
	}
	return &s3.PutObjectOutput{}, nil
}

func TestS3PublisherRetriesAndSucceeds(t *testing.T) {
	fake := &fakePutter{failures: 2}
	pub := newS3Publisher(fake, Config{S3Bucket: "bucket", MaxRetries: 3, RetryBaseDelay: time.Millisecond})
	pub.Publish("/archive/state.json", []byte("{}"), "application/json")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 3 {
		t.Fatalf("calls = %d, want 3", fake.calls)
	}
}

func TestS3PublisherSurfacesTerminalFailure(t *testing.T) {
	fake := &fakePutter{failures: 100, err: errors.New("permanent")}
	var captured error
	pub := newS3Publisher(fake, Config{
		S3Bucket:       "bucket",
		MaxRetries:     1,
		RetryBaseDelay: time.Millisecond,
		OnError:        func(err error) { captured = err },
	})
	pub.Publish("/archive/state.json", []byte("{}"), "application/json")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := pub.Close(ctx)
	if err == nil {
		t.Fatal("Close() = nil, want terminal error")
	}
	if captured == nil {
		t.Fatal("OnError was not called")
	}
}

func TestS3PublisherAppliesEncryptionAndObjectLock(t *testing.T) {
	fake := &fakePutter{}
	retain := time.Now().UTC().Add(time.Hour)
	pub := newS3Publisher(fake, Config{
		S3Bucket:              "bucket",
		ServerSideEncryption:  "aws:kms",
		SSEKMSKeyID:           "key-1",
		ObjectLockMode:        "GOVERNANCE",
		ObjectLockRetainUntil: retain,
	})
	pub.Publish("/archive/state.json", []byte("{}"), "application/json")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.inputs) != 1 {
		t.Fatalf("inputs = %d", len(fake.inputs))
	}
	in := fake.inputs[0]
	if string(in.ServerSideEncryption) != "aws:kms" {
		t.Fatalf("SSE = %q", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId == nil || *in.SSEKMSKeyId != "key-1" {
		t.Fatalf("KMS key = %v", in.SSEKMSKeyId)
	}
	if string(in.ObjectLockMode) != "GOVERNANCE" {
		t.Fatalf("object lock mode = %q", in.ObjectLockMode)
	}
	if in.ObjectLockRetainUntilDate == nil || !in.ObjectLockRetainUntilDate.Equal(retain) {
		t.Fatalf("retain until = %v", in.ObjectLockRetainUntilDate)
	}
}

func TestS3PublisherCloseIsIdempotent(t *testing.T) {
	fake := &fakePutter{}
	pub := newS3Publisher(fake, Config{S3Bucket: "bucket"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestS3PublisherPublishAfterCloseIsDropped(t *testing.T) {
	fake := &fakePutter{}
	pub := newS3Publisher(fake, Config{S3Bucket: "bucket"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	pub.Publish("/archive/late.json", []byte("{}"), "application/json")
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 0 {
		t.Fatalf("calls = %d, want 0", fake.calls)
	}
}
