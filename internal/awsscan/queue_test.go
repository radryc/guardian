package awsscan

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

type fakeScanner struct {
	calls   int
	results []*ScanResult
}

func (f *fakeScanner) Scan(_ context.Context, req *ScanRequest) *ScanResult {
	f.calls++
	if len(f.results) > 0 {
		result := f.results[0]
		f.results = f.results[1:]
		result.ScanID = req.ScanID
		return result
	}
	return &ScanResult{
		APIVersion: APIVersion,
		Kind:       ResultKind,
		ScanID:     req.ScanID,
		Status:     ScanStatusSucceeded,
		StartedAt:  time.Now().UTC(),
		FinishedAt: time.Now().UTC(),
		Request:    req,
	}
}

func newTestRunner(store guardianapi.Store, scanner Scanner) *ScanRunner {
	return &ScanRunner{
		PusherName: "aws-123456789012",
		Account:    "123456789012",
		WorkerID:   "test-worker",
		PrincipalID: "test-principal",
		Store:      store,
		NewScanner: func() Scanner { return scanner },
	}
}

func writeScanRequest(t *testing.T, store guardianapi.Store, pusher string, request *ScanRequest) {
	t.Helper()
	content, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("marshal scan request: %v", err)
	}
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.ScanRequest(pusher, request.ScanID), Content: content}},
	}); err != nil {
		t.Fatalf("write scan request: %v", err)
	}
}

func readScanResult(t *testing.T, store guardianapi.Store, pusher, scanID string) (*ScanResult, error) {
	t.Helper()
	raw, err := store.ReadFile(context.Background(), paths.ScanResult(pusher, scanID))
	if err != nil {
		return nil, err
	}
	var result ScanResult
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("decode scan result: %v", err)
	}
	return &result, nil
}

func TestScanRunnerExecutesQueuedScan(t *testing.T) {
	store := memory.New()
	scanner := &fakeScanner{}
	runner := newTestRunner(store, scanner)
	pusher := runner.PusherName

	writeScanRequest(t, store, pusher, &ScanRequest{
		APIVersion: APIVersion,
		Kind:       RequestKind,
		ScanID:     "scan-1",
		Account:    "123456789012",
		Regions:    []string{"eu-west-1"},
	})

	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("process pending: %v", err)
	}
	if scanner.calls != 1 {
		t.Fatalf("expected 1 scanner call, got %d", scanner.calls)
	}
	result, err := readScanResult(t, store, pusher, "scan-1")
	if err != nil {
		t.Fatalf("scan result missing: %v", err)
	}
	if result.Status != ScanStatusSucceeded {
		t.Fatalf("expected Succeeded, got %s", result.Status)
	}
	if result.Pusher != pusher {
		t.Fatalf("expected pusher %s, got %s", pusher, result.Pusher)
	}

	if _, err := store.Stat(context.Background(), paths.ScanClaim(pusher, "scan-1")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected claim to be released, got err=%v", err)
	}

	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("second process pending: %v", err)
	}
	if scanner.calls != 1 {
		t.Fatalf("scan should not rerun after result exists, calls=%d", scanner.calls)
	}
}

func TestScanRunnerRejectsAccountMismatch(t *testing.T) {
	store := memory.New()
	scanner := &fakeScanner{}
	runner := newTestRunner(store, scanner)
	pusher := runner.PusherName

	writeScanRequest(t, store, pusher, &ScanRequest{
		APIVersion: APIVersion,
		Kind:       RequestKind,
		ScanID:     "scan-mismatch",
		Account:    "999999999999",
		Regions:    []string{"eu-west-1"},
	})

	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("process pending: %v", err)
	}
	if scanner.calls != 0 {
		t.Fatalf("scanner must not run for account mismatch, calls=%d", scanner.calls)
	}
	result, err := readScanResult(t, store, pusher, "scan-mismatch")
	if err != nil {
		t.Fatalf("scan result missing: %v", err)
	}
	if result.Status != ScanStatusFailed {
		t.Fatalf("expected Failed, got %s", result.Status)
	}
	if len(result.Errors) == 0 {
		t.Fatalf("expected error detail")
	}
}

func TestScanRunnerReclaimsExpiredClaim(t *testing.T) {
	store := memory.New()
	scanner := &fakeScanner{}
	runner := newTestRunner(store, scanner)
	pusher := runner.PusherName

	writeScanRequest(t, store, pusher, &ScanRequest{
		APIVersion: APIVersion,
		Kind:       RequestKind,
		ScanID:     "scan-stale",
		Account:    "123456789012",
		Regions:    []string{"eu-west-1"},
	})

	staleClaim := ScanClaim{
		ScanID:       "scan-stale",
		WorkerID:     "other-worker",
		ClaimedAt:    time.Now().UTC().Add(-3 * time.Hour),
		LeaseSeconds: 60,
	}
	content, err := json.Marshal(staleClaim)
	if err != nil {
		t.Fatalf("marshal claim: %v", err)
	}
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.ScanClaim(pusher, "scan-stale"), Content: content}},
	}); err != nil {
		t.Fatalf("write stale claim: %v", err)
	}

	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("process pending: %v", err)
	}
	if scanner.calls != 1 {
		t.Fatalf("expected scanner to reclaim expired claim, calls=%d", scanner.calls)
	}
	if _, err := readScanResult(t, store, pusher, "scan-stale"); err != nil {
		t.Fatalf("scan result missing: %v", err)
	}
}

func TestScanRunnerSkipsLiveClaim(t *testing.T) {
	store := memory.New()
	scanner := &fakeScanner{}
	runner := newTestRunner(store, scanner)
	pusher := runner.PusherName

	writeScanRequest(t, store, pusher, &ScanRequest{
		APIVersion: APIVersion,
		Kind:       RequestKind,
		ScanID:     "scan-live",
		Account:    "123456789012",
		Regions:    []string{"eu-west-1"},
	})

	liveClaim := ScanClaim{
		ScanID:       "scan-live",
		WorkerID:     "other-worker",
		ClaimedAt:    time.Now().UTC(),
		LeaseSeconds: int(scanClaimLease.Seconds()),
	}
	content, err := json.Marshal(liveClaim)
	if err != nil {
		t.Fatalf("marshal claim: %v", err)
	}
	if _, err := store.UpsertFiles(context.Background(), guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: paths.ScanClaim(pusher, "scan-live"), Content: content}},
	}); err != nil {
		t.Fatalf("write live claim: %v", err)
	}

	if err := runner.ProcessPending(context.Background()); err != nil {
		t.Fatalf("process pending: %v", err)
	}
	if scanner.calls != 0 {
		t.Fatalf("scanner must not run while another worker holds the claim, calls=%d", scanner.calls)
	}
}

func TestScanRunnerRequiresStoreAndPusher(t *testing.T) {
	runner := &ScanRunner{}
	if err := runner.Run(context.Background()); err == nil {
		t.Fatalf("expected error for missing store")
	}
	runner = &ScanRunner{Store: memory.New()}
	if err := runner.Run(context.Background()); err == nil {
		t.Fatalf("expected error for missing pusher name")
	}
}

func TestMatchesAnyPattern(t *testing.T) {
	cases := []struct {
		patterns []string
		value    string
		want     bool
	}{
		{[]string{"*"}, "AWS::EC2::Vpc", true},
		{[]string{"AWS::EC2::*"}, "AWS::EC2::Vpc", true},
		{[]string{"AWS::EC2::*"}, "AWS::S3::Bucket", false},
		{[]string{"AWS::S3::Bucket"}, "AWS::S3::Bucket", true},
		{[]string{"", "AWS::IAM::*"}, "AWS::IAM::Role", true},
		{[]string{"AWS::IAM::*"}, "AWS::IAM::Role", true},
		{[]string{}, "AWS::IAM::Role", false},
	}
	for _, tc := range cases {
		got := matchesAnyPattern(tc.patterns, tc.value)
		if got != tc.want {
			t.Fatalf("matchesAnyPattern(%v, %s) = %v, want %v", tc.patterns, tc.value, got, tc.want)
		}
	}
}

func TestCommonTypePrefix(t *testing.T) {
	cases := []struct {
		groups []([]string)
		want   string
	}{
		{[][]string{{"AWS::EC2::*"}, {"AWS::EC2::Vpc"}}, "AWS::EC2::"},
		{[][]string{{"*"}, {}}, ""},
		{[][]string{{"AWS::S3::*"}, {"AWS::IAM::*"}}, "AWS::"},
		{[][]string{{}, {}}, ""},
	}
	for _, tc := range cases {
		got := commonTypePrefix(tc.groups...)
		if got != tc.want {
			t.Fatalf("commonTypePrefix(%v) = %q, want %q", tc.groups, got, tc.want)
		}
	}
}

func TestFilterResourceTypes(t *testing.T) {
	supported := []string{"AWS::EC2::Vpc", "AWS::EC2::Subnet", "AWS::IAM::Role", "AWS::S3::Bucket"}
	got := filterResourceTypes(supported, []string{"AWS::EC2::*"}, []string{"AWS::EC2::Subnet"})
	if len(got) != 1 || got[0] != "AWS::EC2::Vpc" {
		t.Fatalf("unexpected filtered types: %v", got)
	}
}

func TestIsDefaultInventoryResource(t *testing.T) {
	cases := []struct {
		typeName   string
		identifier string
		tags       map[string]string
		want       bool
	}{
		{"AWS::IAM::ManagedPolicy", "arn:aws:iam::aws:policy/AdministratorAccess", nil, true},
		{"AWS::IAM::ManagedPolicy", "arn:aws:iam::1234:policy/custom", nil, false},
		{"AWS::KMS::Alias", "alias/aws/s3", nil, true},
		{"AWS::KMS::Alias", "alias/my-key", nil, false},
		{"AWS::Events::EventBus", "default", nil, true},
		{"AWS::Events::EventBus", "bus-custom", nil, false},
		{"AWS::ECS::CapacityProvider", "FARGATE", nil, true},
		{"AWS::ECS::CapacityProvider", "my-provider", nil, false},
		{"AWS::EC2::PrefixList", "pl-123", map[string]string{"OwnerId": "AWS"}, true},
		{"AWS::EC2::PrefixList", "pl-123", map[string]string{"OwnerId": "1234"}, false},
	}
	for _, tc := range cases {
		got := isDefaultInventoryResource(tc.typeName, tc.identifier, tc.tags)
		if got != tc.want {
			t.Fatalf("isDefaultInventoryResource(%s, %s) = %v, want %v", tc.typeName, tc.identifier, got, tc.want)
		}
	}
}

func TestManagedFromTags(t *testing.T) {
	managed := ManagedFromTags(map[string]string{
		"guardian-managed":   "true",
		"guardian-partition": "demo",
		"guardian-intent":    "network",
		"guardian-asset":     "store",
		"guardian-type":      "ObjectStore",
	})
	if !managed.Managed || managed.Partition != "demo" || managed.Intent != "network" || managed.Asset != "store" {
		t.Fatalf("unexpected managed info: %+v", managed)
	}
	unmanaged := ManagedFromTags(map[string]string{"Name": "thing"})
	if unmanaged.Managed {
		t.Fatalf("expected unmanaged")
	}
}

func TestTagsFromProperties(t *testing.T) {
	raw := []byte(`{"BucketName":"data","Tags":[{"Key":"team","Value":"platform"}]}`)
	tags := tagsFromProperties(raw)
	if tags["team"] != "platform" {
		t.Fatalf("unexpected tags: %v", tags)
	}
	if tagsFromProperties(nil) != nil {
		t.Fatalf("expected nil tags for empty properties")
	}
}

func TestClusterNameFromARN(t *testing.T) {
	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:ecs:eu-west-1:123:cluster/tools", "tools"},
		{"", "default"},
		{"arn:aws:ecs:eu-west-1:123:cluster/", "default"},
	}
	for _, tc := range cases {
		if got := clusterNameFromARN(tc.arn); got != tc.want {
			t.Fatalf("clusterNameFromARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}

func TestNormalizeBucketRegion(t *testing.T) {
	cases := []struct {
		location string
		want     string
	}{
		{"", "us-east-1"},
		{"EU", "eu-west-1"},
		{"eu-central-1", "eu-central-1"},
	}
	for _, tc := range cases {
		if got := normalizeBucketRegion(tc.location); got != tc.want {
			t.Fatalf("normalizeBucketRegion(%q) = %q, want %q", tc.location, got, tc.want)
		}
	}
}
