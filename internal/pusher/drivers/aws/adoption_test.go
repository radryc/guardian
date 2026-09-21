package awsdriver

import (
	"context"
	"testing"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
	"github.com/rydzu/ainfra/guardian/internal/store/memory"
)

type adoptionBackend struct {
	buckets     map[string]BucketSpec
	fileSystems map[string]FileSystem
	parameters  map[string]Parameter
	secrets     map[string]Secret
	services    map[string]ECSService
	lbs         map[string]LoadBalancer

	upsertedBuckets  []BucketSpec
	upsertedParams   []Parameter
	lastService      *ECSService
	deletedBucket    string
	deletedParameter string
}

func newAdoptionBackend() *adoptionBackend {
	return &adoptionBackend{
		buckets:     map[string]BucketSpec{},
		fileSystems: map[string]FileSystem{},
		parameters:  map[string]Parameter{},
		secrets:     map[string]Secret{},
		services:    map[string]ECSService{},
		lbs:         map[string]LoadBalancer{},
	}
}

func (b *adoptionBackend) Synthesize(ctx context.Context, req StackRequest) error { return nil }
func (b *adoptionBackend) CheckEnvironment(ctx context.Context, req StackRequest) error {
	return nil
}
func (b *adoptionBackend) GetStack(ctx context.Context, req StackRequest) (StackState, bool, error) {
	return StackState{}, false, nil
}
func (b *adoptionBackend) DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error) {
	return StackDriftInSync, nil
}
func (b *adoptionBackend) DeployStack(ctx context.Context, req StackRequest) (StackState, error) {
	return StackState{}, nil
}
func (b *adoptionBackend) DeleteStack(ctx context.Context, req StackRequest) error { return nil }

func (b *adoptionBackend) UpsertFileSystem(ctx context.Context, fs FileSystem) (string, error) {
	return "fs-new", nil
}
func (b *adoptionBackend) GetFileSystem(ctx context.Context, fsID string) (FileSystem, bool, error) {
	fs, ok := b.fileSystems[fsID]
	return fs, ok, nil
}
func (b *adoptionBackend) DeleteFileSystem(ctx context.Context, fsID string) error {
	delete(b.fileSystems, fsID)
	return nil
}

func (b *adoptionBackend) UpsertParameter(ctx context.Context, param Parameter) error {
	b.upsertedParams = append(b.upsertedParams, param)
	b.parameters[param.Name] = param
	return nil
}
func (b *adoptionBackend) GetParameter(ctx context.Context, name string) (Parameter, bool, error) {
	param, ok := b.parameters[name]
	return param, ok, nil
}
func (b *adoptionBackend) DeleteParameter(ctx context.Context, name string) error {
	b.deletedParameter = name
	delete(b.parameters, name)
	return nil
}

func (b *adoptionBackend) UpsertSecret(ctx context.Context, secret Secret) (string, error) {
	b.secrets[secret.Name] = secret
	return "arn:aws:secretsmanager:eu-west-1:123456789012:secret:" + secret.Name, nil
}
func (b *adoptionBackend) GetSecret(ctx context.Context, secretID string) (Secret, bool, error) {
	secret, ok := b.secrets[secretID]
	return secret, ok, nil
}
func (b *adoptionBackend) DeleteSecret(ctx context.Context, secretID string) error {
	delete(b.secrets, secretID)
	return nil
}

func (b *adoptionBackend) UpsertService(ctx context.Context, svc ECSService) error {
	copied := svc
	b.lastService = &copied
	b.services[svc.Cluster+"/"+svc.Name] = svc
	return nil
}
func (b *adoptionBackend) GetService(ctx context.Context, cluster, name string) (ECSService, bool, error) {
	svc, ok := b.services[cluster+"/"+name]
	return svc, ok, nil
}
func (b *adoptionBackend) DeleteService(ctx context.Context, cluster, name string) error {
	delete(b.services, cluster+"/"+name)
	return nil
}

func (b *adoptionBackend) UpsertLoadBalancer(ctx context.Context, lb LoadBalancer) (string, error) {
	b.lbs[lb.Name] = lb
	return "arn:aws:elasticloadbalancing:eu-west-1:123456789012:loadbalancer/" + lb.Name, nil
}
func (b *adoptionBackend) GetLoadBalancer(ctx context.Context, name string) (LoadBalancer, bool, error) {
	lb, ok := b.lbs[name]
	return lb, ok, nil
}
func (b *adoptionBackend) DeleteLoadBalancer(ctx context.Context, arn string) error {
	for name, lb := range b.lbs {
		if lb.ARN == arn {
			delete(b.lbs, name)
		}
	}
	return nil
}

func (b *adoptionBackend) UpsertTargetGroup(ctx context.Context, tg TargetGroup) (string, error) {
	return "tg-arn", nil
}
func (b *adoptionBackend) GetTargetGroup(ctx context.Context, name string) (TargetGroup, bool, error) {
	return TargetGroup{}, false, nil
}
func (b *adoptionBackend) DeleteTargetGroup(ctx context.Context, arn string) error { return nil }
func (b *adoptionBackend) UpsertListener(ctx context.Context, listener Listener) (string, error) {
	return "listener-arn", nil
}
func (b *adoptionBackend) GetListener(ctx context.Context, lbARN string, port int) (Listener, bool, error) {
	return Listener{}, false, nil
}
func (b *adoptionBackend) DeleteListener(ctx context.Context, arn string) error { return nil }

func (b *adoptionBackend) UpsertBucket(ctx context.Context, bucket BucketSpec) error {
	b.upsertedBuckets = append(b.upsertedBuckets, bucket)
	b.buckets[bucket.Name] = bucket
	return nil
}
func (b *adoptionBackend) GetBucket(ctx context.Context, name string) (BucketSpec, bool, error) {
	bucket, ok := b.buckets[name]
	return bucket, ok, nil
}
func (b *adoptionBackend) DeleteBucket(ctx context.Context, name string) error {
	b.deletedBucket = name
	delete(b.buckets, name)
	return nil
}

func (b *adoptionBackend) UpsertLogGroup(ctx context.Context, group LogGroup) error { return nil }
func (b *adoptionBackend) GetLogGroup(ctx context.Context, name string) (LogGroup, bool, error) {
	return LogGroup{}, false, nil
}

func adoptionInput(assetType, assetName string, properties map[string]any) registry.AssetInput {
	return registry.AssetInput{
		PartitionName: "imported",
		IntentName:    "import-demo",
		Asset: taskdomain.AbstractAsset{
			Type:       assetType,
			Name:       assetName,
			Properties: properties,
		},
		Assets: map[string]taskdomain.AbstractAsset{},
		Target: targetdomain.Placement{
			Account: "123456789012",
			Region:  "eu-west-1",
		},
		Store:   memory.New(),
		WorkerID: "test-worker",
	}
}

func TestAdoptedBucketDiffInSyncAndConverges(t *testing.T) {
	backend := newAdoptionBackend()
	backend.buckets["legacy-bucket"] = BucketSpec{Name: "legacy-bucket", Versioning: true}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}

	in := adoptionInput("ObjectStore", "legacy-store", map[string]any{
		"engine":         "s3",
		"existingBucket": "legacy-bucket",
		"region":         "eu-west-1",
		"versioning":     true,
	})

	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected adopted bucket in sync, got %s (%s)", report.Status, report.Summary)
	}

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Outputs["bucket"] != "legacy-bucket" {
		t.Fatalf("expected bucket output legacy-bucket, got %v", result.Outputs)
	}
	if len(backend.upsertedBuckets) != 1 || backend.upsertedBuckets[0].Name != "legacy-bucket" {
		t.Fatalf("expected upsert at explicit bucket name, got %+v", backend.upsertedBuckets)
	}

	stored := backend.buckets["legacy-bucket"]
	if stored.Hash == "" || stored.Tags["guardian-managed"] != "true" {
		t.Fatalf("expected guardian tags applied to adopted bucket, got %+v", stored)
	}

	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff after apply: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected in sync after apply, got %s (%s)", report.Status, report.Summary)
	}

	if err := driver.Destroy(context.Background(), in); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if backend.deletedBucket != "legacy-bucket" {
		t.Fatalf("expected destroy at explicit bucket name, got %q", backend.deletedBucket)
	}
}

func TestAdoptedBucketDiffReportsVersioningDrift(t *testing.T) {
	backend := newAdoptionBackend()
	backend.buckets["legacy-bucket"] = BucketSpec{Name: "legacy-bucket", Versioning: false}
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}

	in := adoptionInput("ObjectStore", "legacy-store", map[string]any{
		"engine":         "s3",
		"existingBucket": "legacy-bucket",
		"versioning":     true,
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("expected versioning drift, got %s", report.Status)
	}
}

func TestAdoptedBucketMissingReportsDrift(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ObjectStoreDriver{baseDriver{backend: backend}}

	in := adoptionInput("ObjectStore", "legacy-store", map[string]any{
		"engine":         "s3",
		"existingBucket": "gone-bucket",
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("expected missing adopted bucket drift, got %s", report.Status)
	}
}

func TestAdoptedFileSystemDiffAndApply(t *testing.T) {
	backend := newAdoptionBackend()
	backend.fileSystems["fs-123"] = FileSystem{ID: "fs-123", Name: "legacy"}
	driver := &VolumeDriver{baseDriver{backend: backend}}

	in := adoptionInput("Volume", "legacy-volume", map[string]any{
		"existingID": "fs-123",
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected adopted EFS in sync, got %s (%s)", report.Status, report.Summary)
	}

	result, err := driver.Apply(context.Background(), in)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if result.Outputs["efsId"] != "fs-123" {
		t.Fatalf("expected efsId output fs-123, got %v", result.Outputs)
	}

	if err := driver.Destroy(context.Background(), in); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, ok := backend.fileSystems["fs-123"]; !ok {
		t.Fatalf("adopted EFS must not be destroyed")
	}
}

func TestAdoptedParameterDiffInSync(t *testing.T) {
	backend := newAdoptionBackend()
	backend.parameters["/app/legacy"] = Parameter{Name: "/app/legacy", Value: "config-value", Type: "String"}
	driver := &ConfigDriver{baseDriver{backend: backend}}

	in := adoptionInput("Config", "legacy-config", map[string]any{
		"existingParameter": "/app/legacy",
		"content":           "config-value",
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected adopted parameter in sync, got %s (%s)", report.Status, report.Summary)
	}

	in.Asset.Properties["content"] = "changed-value"
	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff after edit: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("expected drift after content edit, got %s", report.Status)
	}

	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if len(backend.upsertedParams) != 1 || backend.upsertedParams[0].Name != "/app/legacy" {
		t.Fatalf("expected upsert at explicit parameter name, got %+v", backend.upsertedParams)
	}
}

func TestAdoptedSecretDiffAndApplyGuard(t *testing.T) {
	backend := newAdoptionBackend()
	backend.secrets["legacy/secret"] = Secret{ID: "arn:1", Name: "legacy/secret"}
	driver := &SecretDriver{baseDriver{backend: backend}}

	in := adoptionInput("Secret", "legacy-secret", map[string]any{
		"existingSecret": "legacy/secret",
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected adopted secret in sync, got %s (%s)", report.Status, report.Summary)
	}

	if _, err := driver.Apply(context.Background(), in); err == nil {
		t.Fatalf("expected apply guard error for adopted secret without value")
	}

	in.Asset.Properties["value"] = "managed-value"
	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("apply with value: %v", err)
	}
}

func TestAdoptedLoadBalancerDiffAndApplyGuard(t *testing.T) {
	backend := newAdoptionBackend()
	backend.lbs["legacy-alb"] = LoadBalancer{
		ARN:    "arn:aws:elasticloadbalancing:eu-west-1:1:loadbalancer/legacy-alb",
		Name:   "legacy-alb",
		Type:   "application",
		Scheme: "internet-facing",
	}
	driver := &LoadBalancerDriver{baseDriver{backend: backend}}

	in := adoptionInput("LoadBalancer", "legacy-lb", map[string]any{
		"existingName": "legacy-alb",
		"listeners": []any{
			map[string]any{"port": 443, "protocol": "https"},
		},
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected adopted load balancer in sync, got %s (%s)", report.Status, report.Summary)
	}

	if _, err := driver.Apply(context.Background(), in); err == nil {
		t.Fatalf("expected apply guard error for adopted load balancer")
	}

	if err := driver.Destroy(context.Background(), in); err != nil {
		t.Fatalf("destroy: %v", err)
	}
	if _, ok := backend.lbs["legacy-alb"]; !ok {
		t.Fatalf("adopted load balancer must not be destroyed")
	}
}

func TestObservedServiceDiffAndApply(t *testing.T) {
	backend := newAdoptionBackend()
	backend.services["tools/web"] = ECSService{
		Name:         "web",
		Cluster:      "tools",
		DesiredCount: 2,
		TaskFamily:   "web-family",
	}
	driver := &ComputeDriver{baseDriver{backend: backend}}

	in := adoptionInput("Compute", "web", map[string]any{
		"image":               "nginx:1.27",
		"observeExisting":     true,
		"existingServiceName": "web",
		"cluster":             "tools",
		"replicas":            2,
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "InSync" {
		t.Fatalf("expected observed service in sync, got %s (%s)", report.Status, report.Summary)
	}

	in.Asset.Properties["replicas"] = 3
	report, err = driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff after edit: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("expected drift after replica edit, got %s", report.Status)
	}

	if _, err := driver.Apply(context.Background(), in); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if backend.lastService == nil {
		t.Fatalf("expected service upsert")
	}
	if backend.lastService.Name != "web" || backend.lastService.Cluster != "tools" {
		t.Fatalf("expected upsert at explicit service/cluster, got %+v", backend.lastService)
	}
	if backend.lastService.DesiredCount != 3 {
		t.Fatalf("expected desired count 3, got %d", backend.lastService.DesiredCount)
	}
}

func TestObservedServiceMissingReportsDrift(t *testing.T) {
	backend := newAdoptionBackend()
	driver := &ComputeDriver{baseDriver{backend: backend}}

	in := adoptionInput("Compute", "web", map[string]any{
		"image":               "nginx:1.27",
		"observeExisting":     true,
		"existingServiceName": "gone",
		"cluster":             "tools",
	})
	report, err := driver.Diff(context.Background(), in)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if report.Status != "Changed" {
		t.Fatalf("expected missing observed service drift, got %s", report.Status)
	}
}

func TestTaskFamilyFromDefinitionARN(t *testing.T) {
	cases := []struct {
		arn  string
		want string
	}{
		{"arn:aws:ecs:eu-west-1:123:task-definition/web-family:42", "web-family"},
		{"arn:aws:ecs:eu-west-1:123:task-definition/family-no-rev", "family-no-rev"},
		{"", ""},
		{"no-slashes", ""},
	}
	for _, tc := range cases {
		if got := taskFamilyFromDefinitionARN(tc.arn); got != tc.want {
			t.Fatalf("taskFamilyFromDefinitionARN(%q) = %q, want %q", tc.arn, got, tc.want)
		}
	}
}
