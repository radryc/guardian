package auditanchor

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	// ManifestVersion is the current schema version of an anchored audit manifest.
	ManifestVersion = 1

	// DefaultRetentionDays is used when S3AnchorConfig.RetentionDays <= 0.
	DefaultRetentionDays = 365
)

// GenesisPrevHash is the all-zero previous hash that starts every writer chain.
const GenesisPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

var emptySHA256 = func() string {
	sum := sha256.Sum256(nil)
	return hex.EncodeToString(sum[:])
}()

// EventRecord represents a single audit event stored in a WAL JSONL file or
// the ClickHouse audit.events table. RawJSON is the exact canonical bytes that
// were hashed (without the hash and raw_json fields).
type EventRecord struct {
	EventID   string            `json:"event_id,omitempty"`
	WriterID  string            `json:"writer_id,omitempty"`
	Sequence  uint64            `json:"seq"`
	Timestamp time.Time         `json:"timestamp"`
	Actor     string            `json:"actor_subject,omitempty"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource,omitempty"`
	Partition string            `json:"partition,omitempty"`
	Outcome   string            `json:"decision,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	PrevHash  string            `json:"prev_hash,omitempty"`
	EventHash string            `json:"hash,omitempty"`
	RawJSON   string            `json:"raw_json,omitempty"`
}

// Manifest is the daily tamper-evident anchor summary for a batch of WAL events.
type Manifest struct {
	Version          int       `json:"version"`
	Date             string    `json:"date"`
	EventCount       int       `json:"eventCount"`
	MerkleRoot       string    `json:"merkleRoot"`
	PrevManifestHash string    `json:"prevManifestHash,omitempty"`
	ManifestHash     string    `json:"manifestHash"`
	CreatedAt        time.Time `json:"createdAt"`
}

// ComputeEventHash computes the deterministic SHA-256 hex digest of an
// EventRecord. When RawJSON is present it hashes the exact canonical bytes
// (prev_hash || raw_json); otherwise it marshals a canonical event without the
// hash and raw_json fields.
func ComputeEventHash(ev EventRecord) (string, error) {
	canonical := ev.RawJSON
	if canonical == "" {
		ev.EventHash = ""
		ev.RawJSON = ""
		if !ev.Timestamp.IsZero() {
			ev.Timestamp = ev.Timestamp.UTC()
		}
		payload, err := json.Marshal(ev)
		if err != nil {
			return "", fmt.Errorf("marshal event for hashing: %w", err)
		}
		canonical = string(payload)
	}
	sum := sha256.Sum256([]byte(ev.PrevHash + canonical))
	return hex.EncodeToString(sum[:]), nil
}

// ChainEvents populates PrevHash, Seq, EventHash, and RawJSON across a slice of
// EventRecords, starting from initialPrevHash for the first event.
func ChainEvents(events []EventRecord, initialPrevHash string) ([]EventRecord, error) {
	out := make([]EventRecord, len(events))
	prev := strings.ToLower(strings.TrimSpace(initialPrevHash))
	for i, ev := range events {
		ev.PrevHash = prev
		ev.Sequence = uint64(i)
		canonical := ev.RawJSON
		if canonical == "" {
			ev.EventHash = ""
			ev.RawJSON = ""
			if !ev.Timestamp.IsZero() {
				ev.Timestamp = ev.Timestamp.UTC()
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				return nil, fmt.Errorf("event %d: %w", i, err)
			}
			canonical = string(payload)
			ev.RawJSON = canonical
		}
		sum := sha256.Sum256([]byte(ev.PrevHash + canonical))
		ev.EventHash = hex.EncodeToString(sum[:])
		out[i] = ev
		prev = ev.EventHash
	}
	return out, nil
}

// ReadWALFile parses a JSONL WAL file into a slice of EventRecords.
func ReadWALFile(path string) ([]EventRecord, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open WAL file: %w", err)
	}
	defer f.Close()

	var events []EventRecord
	scanner := bufio.NewScanner(f)
	// Allow up to 10 MiB per JSONL event line.
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev EventRecord
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("decode WAL line %d: %w", lineNum, err)
		}
		events = append(events, ev)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan WAL file: %w", err)
	}
	return events, nil
}

// ComputeMerkleRoot computes a deterministic SHA-256 binary Merkle tree root
// over the provided hex-encoded event hashes.
func ComputeMerkleRoot(hashes []string) string {
	if len(hashes) == 0 {
		return emptySHA256
	}
	level := make([]string, len(hashes))
	for i, h := range hashes {
		level[i] = strings.ToLower(strings.TrimSpace(h))
	}
	if len(level) == 1 {
		return level[0]
	}
	for len(level) > 1 {
		if len(level)%2 == 1 {
			level = append(level, level[len(level)-1])
		}
		next := make([]string, 0, len(level)/2)
		for i := 0; i < len(level); i += 2 {
			sum := sha256.Sum256([]byte("node:" + level[i] + ":" + level[i+1]))
			next = append(next, hex.EncodeToString(sum[:]))
		}
		level = next
	}
	return level[0]
}

// ComputeManifestHash computes the deterministic SHA-256 hex digest of a Manifest
// excluding ManifestHash itself.
func ComputeManifestHash(m Manifest) (string, error) {
	m.ManifestHash = ""
	if !m.CreatedAt.IsZero() {
		m.CreatedAt = m.CreatedAt.UTC()
	}
	payload, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("marshal manifest for hashing: %w", err)
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// BuildManifest validates the event chain, computes the Merkle root, and returns
// a signed/hashed Manifest for the given UTC date (YYYY-MM-DD).
func BuildManifest(date string, events []EventRecord, prevManifestHash string) (Manifest, error) {
	date = strings.TrimSpace(date)
	if date == "" {
		return Manifest{}, errors.New("date is required")
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return Manifest{}, fmt.Errorf("invalid date %q (expected YYYY-MM-DD): %w", date, err)
	}

	hashes, err := verifyAndCollectEventHashes(events, false)
	if err != nil {
		return Manifest{}, err
	}

	m := Manifest{
		Version:          ManifestVersion,
		Date:             date,
		EventCount:       len(events),
		MerkleRoot:       ComputeMerkleRoot(hashes),
		PrevManifestHash: strings.ToLower(strings.TrimSpace(prevManifestHash)),
		CreatedAt:        time.Now().UTC(),
	}
	manifestHash, err := ComputeManifestHash(m)
	if err != nil {
		return Manifest{}, err
	}
	m.ManifestHash = manifestHash
	return m, nil
}

// VerifyBundle verifies a Manifest against its WAL events and an optional
// expectedPrevManifestHash, failing on any hash, chain, count, or Merkle root mismatch.
func VerifyBundle(manifest Manifest, events []EventRecord, expectedPrevManifestHash string) error {
	if manifest.Version != ManifestVersion {
		return fmt.Errorf("unsupported manifest version %d", manifest.Version)
	}
	if strings.TrimSpace(manifest.Date) == "" {
		return errors.New("manifest date is empty")
	}
	if manifest.EventCount != len(events) {
		return fmt.Errorf("event count mismatch: manifest=%d, events=%d", manifest.EventCount, len(events))
	}
	expectedPrev := strings.ToLower(strings.TrimSpace(expectedPrevManifestHash))
	if expectedPrev != "" && strings.ToLower(strings.TrimSpace(manifest.PrevManifestHash)) != expectedPrev {
		return fmt.Errorf("prevManifestHash mismatch: got %q, want %q", manifest.PrevManifestHash, expectedPrev)
	}

	recomputedManifestHash, err := ComputeManifestHash(manifest)
	if err != nil {
		return err
	}
	if manifest.ManifestHash == "" || strings.ToLower(strings.TrimSpace(manifest.ManifestHash)) != recomputedManifestHash {
		return fmt.Errorf("manifestHash mismatch: got %q, want %q", manifest.ManifestHash, recomputedManifestHash)
	}

	hashes, err := verifyAndCollectEventHashes(events, true)
	if err != nil {
		return err
	}
	merkleRoot := ComputeMerkleRoot(hashes)
	if strings.ToLower(strings.TrimSpace(manifest.MerkleRoot)) != merkleRoot {
		return fmt.Errorf("merkleRoot mismatch: got %q, want %q", manifest.MerkleRoot, merkleRoot)
	}
	return nil
}

func verifyAndCollectEventHashes(events []EventRecord, strictChain bool) ([]string, error) {
	hashes := make([]string, len(events))
	var (
		prevHash      string
		currentWriter string
		expectedSeq   uint64
	)
	for i, ev := range events {
		if err := verifyRawJSONFields(ev); err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}
		// Each writer has its own genesis -> head chain. ClickHouse rows are
		// ordered by (writer_id, seq), so a writer change starts a new chain.
		if i == 0 || ev.WriterID != currentWriter {
			currentWriter = ev.WriterID
			prevHash = ""
			expectedSeq = 0
			if ev.PrevHash != "" && strings.ToLower(strings.TrimSpace(ev.PrevHash)) != GenesisPrevHash {
				return nil, fmt.Errorf("event %d: first event of writer %q has prevHash %q, want genesis", i, ev.WriterID, ev.PrevHash)
			}
		} else if ev.PrevHash != "" && strings.ToLower(strings.TrimSpace(ev.PrevHash)) != prevHash {
			return nil, fmt.Errorf("event %d prevHash mismatch: got %q, want %q", i, ev.PrevHash, prevHash)
		}
		if ev.Sequence != expectedSeq {
			return nil, fmt.Errorf("event %d seq mismatch for writer %q: got %d, want %d", i, ev.WriterID, ev.Sequence, expectedSeq)
		}
		recomputed, err := ComputeEventHash(ev)
		if err != nil {
			return nil, fmt.Errorf("event %d: %w", i, err)
		}
		if ev.EventHash != "" && strings.ToLower(strings.TrimSpace(ev.EventHash)) != recomputed {
			return nil, fmt.Errorf("event %d hash mismatch: got %q, want %q", i, ev.EventHash, recomputed)
		}
		hashes[i] = recomputed
		prevHash = recomputed
		expectedSeq++
	}
	return hashes, nil
}

// verifyRawJSONFields catches an edit to an indexed column that did not also
// rewrite raw_json (or vice versa). Only the string fields the anchor indexes
// are compared; the hash chain authenticates raw_json itself.
func verifyRawJSONFields(ev EventRecord) error {
	if strings.TrimSpace(ev.RawJSON) == "" {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal([]byte(ev.RawJSON), &raw); err != nil {
		return fmt.Errorf("decode raw_json: %w", err)
	}
	checks := []struct {
		key   string
		value string
	}{
		{"event_id", ev.EventID},
		{"writer_id", ev.WriterID},
		{"actor_subject", ev.Actor},
		{"action", ev.Action},
		{"resource", ev.Resource},
		{"partition", ev.Partition},
		{"decision", ev.Outcome},
		{"prev_hash", ev.PrevHash},
	}
	for _, check := range checks {
		rawValue, _ := raw[check.key].(string)
		if rawValue != check.value {
			return fmt.Errorf("%s does not match raw_json: got %q, want %q", check.key, check.value, rawValue)
		}
	}
	return nil
}

type putObjectAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

// S3AnchorConfig configures S3 Object Lock COMPLIANCE anchoring for audit manifests.
type S3AnchorConfig struct {
	Bucket         string
	Region         string
	Prefix         string
	Endpoint       string
	ForcePathStyle bool
	KMSKeyID       string
	RetentionDays  int
}

// S3AnchorUploader uploads audit manifests to S3 with Object Lock COMPLIANCE mode
// and aws:kms server-side encryption.
type S3AnchorUploader struct {
	client        putObjectAPI
	bucket        string
	prefix        string
	kmsKeyID      string
	retentionDays int
	now           func() time.Time
}

// NewS3AnchorUploader constructs an S3AnchorUploader using the default AWS SDK v2 config.
func NewS3AnchorUploader(ctx context.Context, cfg S3AnchorConfig) (*S3AnchorUploader, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("S3 bucket is required")
	}
	loadOpts := make([]func(*awsconfig.LoadOptions) error, 0, 1)
	if strings.TrimSpace(cfg.Region) != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(strings.TrimSpace(cfg.Region)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(opts *s3.Options) {
		opts.UsePathStyle = cfg.ForcePathStyle
		if strings.TrimSpace(cfg.Endpoint) != "" {
			opts.BaseEndpoint = stringPtr(strings.TrimSpace(cfg.Endpoint))
		}
	})
	return newS3AnchorUploader(client, cfg)
}

func newS3AnchorUploader(client putObjectAPI, cfg S3AnchorConfig) (*S3AnchorUploader, error) {
	if client == nil {
		return nil, errors.New("S3 client is required")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, errors.New("S3 bucket is required")
	}
	retentionDays := cfg.RetentionDays
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	return &S3AnchorUploader{
		client:        client,
		bucket:        strings.TrimSpace(cfg.Bucket),
		prefix:        strings.Trim(strings.TrimSpace(cfg.Prefix), "/"),
		kmsKeyID:      strings.TrimSpace(cfg.KMSKeyID),
		retentionDays: retentionDays,
		now:           func() time.Time { return time.Now().UTC() },
	}, nil
}

// UploadManifest serializes and uploads the Manifest to S3 with ObjectLockMode=COMPLIANCE,
// ServerSideEncryption=aws:kms, and ObjectLockRetainUntilDate.
func (u *S3AnchorUploader) UploadManifest(ctx context.Context, m Manifest) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(m.Date) == "" {
		return "", errors.New("manifest date is required")
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}
	body = append(body, '\n')

	key := u.objectKey(m.Date)
	retainUntil := u.now().UTC().Add(time.Duration(u.retentionDays) * 24 * time.Hour)
	contentType := "application/json"

	in := &s3.PutObjectInput{
		Bucket:                    &u.bucket,
		Key:                       &key,
		Body:                      bytes.NewReader(body),
		ContentType:               &contentType,
		ServerSideEncryption:      s3types.ServerSideEncryptionAwsKms,
		ObjectLockMode:            s3types.ObjectLockModeCompliance,
		ObjectLockRetainUntilDate: &retainUntil,
	}
	if u.kmsKeyID != "" {
		in.SSEKMSKeyId = &u.kmsKeyID
	}

	if _, err := u.client.PutObject(ctx, in); err != nil {
		return "", fmt.Errorf("upload manifest to s3://%s/%s: %w", u.bucket, key, err)
	}
	return key, nil
}

func (u *S3AnchorUploader) objectKey(date string) string {
	filename := date + "/manifest.json"
	if u.prefix == "" {
		return filename
	}
	return u.prefix + "/" + filename
}

func stringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}
