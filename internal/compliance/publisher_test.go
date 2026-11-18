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

func TestS3PublisherDynamicObjectLockRetention(t *testing.T) {
	fake := &fakePutter{}
	t0 := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	retention := 24 * time.Hour
	pub := newS3Publisher(fake, Config{
		S3Bucket:                 "bucket",
		ObjectLockMode:           "COMPLIANCE",
		ObjectLockRetainDuration: retention,
		// Static timestamp in the past should be ignored when duration is set.
		ObjectLockRetainUntil: t0.Add(-time.Hour),
	})
	var mu sync.Mutex
	current := t0
	pub.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}

	in1 := pub.input(uploadRequest{key: "a.json", content: []byte("{}"), contentType: "application/json"})
	mu.Lock()
	current = t0.Add(2 * time.Hour)
	mu.Unlock()
	in2 := pub.input(uploadRequest{key: "b.json", content: []byte("{}"), contentType: "application/json"})

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pub.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if in1.ObjectLockRetainUntilDate == nil || !in1.ObjectLockRetainUntilDate.Equal(t0.Add(retention)) {
		t.Fatalf("first retain until = %v, want %v", in1.ObjectLockRetainUntilDate, t0.Add(retention))
	}
	wantSecond := t0.Add(2*time.Hour + retention)
	if in2.ObjectLockRetainUntilDate == nil || !in2.ObjectLockRetainUntilDate.Equal(wantSecond) {
		t.Fatalf("second retain until = %v, want %v", in2.ObjectLockRetainUntilDate, wantSecond)
	}
	if string(in1.ObjectLockMode) != "COMPLIANCE" || string(in2.ObjectLockMode) != "COMPLIANCE" {
		t.Fatalf("unexpected object lock modes: %q, %q", in1.ObjectLockMode, in2.ObjectLockMode)
	}
}

func TestS3PublisherFailClosedReturnsError(t *testing.T) {
	wantErr := errors.New("s3 unavailable")
	fake := &fakePutter{failures: 100, err: wantErr}
	var captured error
	pub := newS3Publisher(fake, Config{
		S3Bucket:       "bucket",
		MaxRetries:     1,
		RetryBaseDelay: time.Millisecond,
		FailClosed:     true,
		OnError:        func(err error) { captured = err },
	})

	// SyncPublish returns the terminal error directly to the caller.
	if err := pub.SyncPublish(context.Background(), "/archive/state.json", []byte("{}"), "application/json"); !errors.Is(err, wantErr) {
		t.Fatalf("SyncPublish() error = %v, want %v", err, wantErr)
	}
	if !errors.Is(captured, wantErr) {
		t.Fatalf("OnError captured = %v, want %v", captured, wantErr)
	}

	// Publish in FailClosed mode runs synchronously before returning.
	captured = nil
	pub.Publish("/archive/state2.json", []byte("{}"), "application/json")
	if !errors.Is(captured, wantErr) {
		t.Fatalf("OnError after synchronous Publish = %v, want %v", captured, wantErr)
	}

	fake.mu.Lock()
	calls := fake.calls
	fake.mu.Unlock()
	if calls != 4 {
		t.Fatalf("calls = %d, want 4 (2 attempts per publish)", calls)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pub.Close(ctx); !errors.Is(err, wantErr) {
		t.Fatalf("Close() error = %v, want %v", err, wantErr)
	}
}

func TestS3PublisherStatsCounters(t *testing.T) {
	// 2 transient failures -> 1st publish succeeds on 3rd attempt;
	// then 10 failures -> 2nd publish fails after exhausting retries.
	fake := &fakePutter{failures: 2}
	pub := newS3Publisher(fake, Config{
		S3Bucket:       "bucket",
		MaxRetries:     2,
		RetryBaseDelay: time.Millisecond,
		FailClosed:     true,
	})

	if err := pub.SyncPublish(context.Background(), "/archive/ok.json", []byte("{}"), "application/json"); err != nil {
		t.Fatalf("first SyncPublish() error = %v", err)
	}
	published, failed := pub.Stats()
	if published != 1 || failed != 0 {
		t.Fatalf("after success Stats() = (%d, %d), want (1, 0)", published, failed)
	}

	fake.mu.Lock()
	fake.failures = 10
	fake.err = errors.New("terminal failure")
	fake.mu.Unlock()

	if err := pub.SyncPublish(context.Background(), "/archive/fail.json", []byte("{}"), "application/json"); err == nil {
		t.Fatal("second SyncPublish() = nil, want error")
	}
	published, failed = pub.Stats()
	if published != 1 || failed != 1 {
		t.Fatalf("after failure Stats() = (%d, %d), want (1, 1)", published, failed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := pub.Close(ctx); err == nil {
		t.Fatal("Close() = nil, want terminal failure")
	}
}
