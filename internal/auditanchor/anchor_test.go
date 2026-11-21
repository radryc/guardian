package auditanchor

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

func sampleEvents(t *testing.T, count int) []EventRecord {
	t.Helper()
	base := time.Date(2026, 11, 18, 8, 0, 0, 0, time.UTC)
	raw := make([]EventRecord, count)
	for i := 0; i < count; i++ {
		raw[i] = EventRecord{
			EventID:   "evt-" + string(rune('a'+i)),
			Sequence:  uint64(i + 1),
			Timestamp: base.Add(time.Duration(i) * time.Minute),
			Actor:     "alice@example.com",
			Action:    "partition.push",
			Resource:  "prod-us-east-1",
			Partition: "prod",
			Outcome:   "success",
			Details:   map[string]string{"revision": "r1"},
		}
	}
	chained, err := ChainEvents(raw, "")
	if err != nil {
		t.Fatalf("ChainEvents() error = %v", err)
	}
	return chained
}

func TestComputeMerkleRoot(t *testing.T) {
	empty := ComputeMerkleRoot(nil)
	if len(empty) != 64 {
		t.Fatalf("empty merkle root len = %d, want 64", len(empty))
	}
	if empty != ComputeMerkleRoot([]string{}) {
		t.Fatal("empty merkle root is not deterministic")
	}

	single := ComputeMerkleRoot([]string{"abc123"})
	if single != "abc123" {
		t.Fatalf("single merkle root = %q, want abc123", single)
	}

	r1 := ComputeMerkleRoot([]string{"h1", "h2", "h3"})
	r2 := ComputeMerkleRoot([]string{"h1", "h2", "h3"})
	if r1 != r2 {
		t.Fatalf("merkle root not deterministic: %q != %q", r1, r2)
	}
	if len(r1) != 64 {
		t.Fatalf("merkle root len = %d, want 64", len(r1))
	}

	reordered := ComputeMerkleRoot([]string{"h2", "h1", "h3"})
	if reordered == r1 {
		t.Fatal("merkle root did not change on leaf reorder")
	}
	modified := ComputeMerkleRoot([]string{"h1", "h2", "h4"})
	if modified == r1 {
		t.Fatal("merkle root did not change on leaf modification")
	}
}

func TestBuildAndVerifyBundle(t *testing.T) {
	for _, count := range []int{0, 1, 2, 3, 4} {
		events := sampleEvents(t, count)
		prevManifestHash := strings.Repeat("a", 64)
		m, err := BuildManifest("2026-11-18", events, prevManifestHash)
		if err != nil {
			t.Fatalf("count=%d BuildManifest() error = %v", count, err)
		}
		if m.Version != ManifestVersion {
			t.Fatalf("count=%d Version = %d, want %d", count, m.Version, ManifestVersion)
		}
		if m.EventCount != count {
			t.Fatalf("count=%d EventCount = %d, want %d", count, m.EventCount, count)
		}
		if err := VerifyBundle(m, events, prevManifestHash); err != nil {
			t.Fatalf("count=%d VerifyBundle() error = %v", count, err)
		}
		// Optional expectedPrevManifestHash empty should also succeed.
		if err := VerifyBundle(m, events, ""); err != nil {
			t.Fatalf("count=%d VerifyBundle(empty prev) error = %v", count, err)
		}
	}
}

func TestVerifyBundleTamperDetection(t *testing.T) {
	prevHash := strings.Repeat("b", 64)
	events := sampleEvents(t, 3)
	m, err := BuildManifest("2026-11-18", events, prevHash)
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	t.Run("tampered event payload", func(t *testing.T) {
		tampered := append([]EventRecord(nil), events...)
		tampered[1].Actor = "mallory@example.com"
		if err := VerifyBundle(m, tampered, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for tampered event payload")
		}
	})

	t.Run("tampered event hash", func(t *testing.T) {
		tampered := append([]EventRecord(nil), events...)
		tampered[0].EventHash = strings.Repeat("f", 64)
		if err := VerifyBundle(m, tampered, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for tampered eventHash")
		}
	})

	t.Run("broken prevHash chain", func(t *testing.T) {
		tampered := append([]EventRecord(nil), events...)
		tampered[2].PrevHash = strings.Repeat("0", 64)
		h, _ := ComputeEventHash(tampered[2])
		tampered[2].EventHash = h
		if err := VerifyBundle(m, tampered, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for broken prevHash chain")
		}
	})

	t.Run("deleted event", func(t *testing.T) {
		if err := VerifyBundle(m, events[:2], prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for deleted event")
		}
	})

	t.Run("reordered events", func(t *testing.T) {
		reordered := []EventRecord{events[0], events[2], events[1]}
		if err := VerifyBundle(m, reordered, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for reordered events")
		}
	})

	t.Run("tampered manifest merkle root", func(t *testing.T) {
		bad := m
		bad.MerkleRoot = strings.Repeat("c", 64)
		if err := VerifyBundle(bad, events, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for tampered merkleRoot")
		}
	})

	t.Run("tampered manifest hash", func(t *testing.T) {
		bad := m
		bad.ManifestHash = strings.Repeat("d", 64)
		if err := VerifyBundle(bad, events, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for tampered manifestHash")
		}
	})

	t.Run("mismatched prevManifestHash", func(t *testing.T) {
		if err := VerifyBundle(m, events, strings.Repeat("e", 64)); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for mismatched expectedPrevManifestHash")
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		bad := m
		bad.Version = 99
		if err := VerifyBundle(bad, events, prevHash); err == nil {
			t.Fatal("VerifyBundle() = nil, want error for unsupported version")
		}
	})
}

func TestReadWALFile(t *testing.T) {
	events := sampleEvents(t, 2)
	dir := t.TempDir()
	walPath := filepath.Join(dir, "audit.wal.jsonl")

	var buf strings.Builder
	for _, ev := range events {
		b, err := json.Marshal(ev)
		if err != nil {
			t.Fatalf("Marshal() error = %v", err)
		}
		buf.Write(b)
		buf.WriteString("\n\n")
	}
	if err := os.WriteFile(walPath, []byte(buf.String()), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := ReadWALFile(walPath)
	if err != nil {
		t.Fatalf("ReadWALFile() error = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].EventHash != events[0].EventHash || got[1].PrevHash != events[1].PrevHash {
		t.Fatalf("parsed events mismatch: %+v", got)
	}

	badPath := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(badPath, []byte("{not-json}\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := ReadWALFile(badPath); err == nil {
		t.Fatal("ReadWALFile(bad) = nil, want error")
	}
}

type fakeS3Client struct {
	inputs []*s3.PutObjectInput
	err    error
}

func (f *fakeS3Client) PutObject(_ context.Context, in *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &s3.PutObjectOutput{}, nil
}

func TestS3AnchorUploaderComplianceAndKMS(t *testing.T) {
	fake := &fakeS3Client{}
	uploader, err := newS3AnchorUploader(fake, S3AnchorConfig{
		Bucket:        "audit-anchor-bucket",
		Prefix:        "/guardian/anchors/",
		KMSKeyID:      "arn:aws:kms:us-east-1:123456789012:key/test-key",
		RetentionDays: 90,
	})
	if err != nil {
		t.Fatalf("newS3AnchorUploader() error = %v", err)
	}
	fixedNow := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	uploader.now = func() time.Time { return fixedNow }

	events := sampleEvents(t, 2)
	m, err := BuildManifest("2026-11-18", events, "")
	if err != nil {
		t.Fatalf("BuildManifest() error = %v", err)
	}

	key, err := uploader.UploadManifest(context.Background(), m)
	if err != nil {
		t.Fatalf("UploadManifest() error = %v", err)
	}
	if key != "guardian/anchors/2026-11-18/manifest.json" {
		t.Fatalf("key = %q, want guardian/anchors/2026-11-18/manifest.json", key)
	}
	if len(fake.inputs) != 1 {
		t.Fatalf("PutObject calls = %d, want 1", len(fake.inputs))
	}

	in := fake.inputs[0]
	if in.ObjectLockMode != s3types.ObjectLockModeCompliance {
		t.Fatalf("ObjectLockMode = %q, want COMPLIANCE", in.ObjectLockMode)
	}
	if in.ServerSideEncryption != s3types.ServerSideEncryptionAwsKms {
		t.Fatalf("ServerSideEncryption = %q, want aws:kms", in.ServerSideEncryption)
	}
	if in.SSEKMSKeyId == nil || *in.SSEKMSKeyId != "arn:aws:kms:us-east-1:123456789012:key/test-key" {
		t.Fatalf("SSEKMSKeyId = %v", in.SSEKMSKeyId)
	}
	wantRetain := fixedNow.Add(90 * 24 * time.Hour)
	if in.ObjectLockRetainUntilDate == nil || !in.ObjectLockRetainUntilDate.Equal(wantRetain) {
		t.Fatalf("ObjectLockRetainUntilDate = %v, want %v", in.ObjectLockRetainUntilDate, wantRetain)
	}

	fakeErr := &fakeS3Client{err: errors.New("access denied")}
	uploaderErr, _ := newS3AnchorUploader(fakeErr, S3AnchorConfig{Bucket: "b"})
	if _, err := uploaderErr.UploadManifest(context.Background(), m); err == nil {
		t.Fatal("UploadManifest() = nil, want error on S3 failure")
	}
}
