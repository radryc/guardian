package awsdriver

import (
	"context"
	"fmt"
	"sync"
)

type Backend struct {
	mu             sync.Mutex
	bootstrapReady map[string]bool
	stacks         map[string]StackState
	drift          map[string]StackDriftStatus
	lastRequest    StackRequest
}

func NewBackend() *Backend {
	return &Backend{
		bootstrapReady: map[string]bool{},
		stacks:         map[string]StackState{},
		drift:          map[string]StackDriftStatus{},
	}
}

func (b *Backend) SetBootstrapReady(account, region string, ready bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.bootstrapReady[envKey(account, region)] = ready
}

func (b *Backend) SetDriftStatus(account, region, stackName string, status StackDriftStatus) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.drift[stackKey(account, region, stackName)] = status
}

func (b *Backend) SetStackOutputs(account, region, stackName string, outputs map[string]string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	state := b.stacks[stackKey(account, region, stackName)]
	state.Name = stackName
	state.Outputs = cloneStringMap(outputs)
	b.stacks[stackKey(account, region, stackName)] = state
}

func (b *Backend) LastRequest() StackRequest {
	b.mu.Lock()
	defer b.mu.Unlock()
	return StackRequest{
		PartitionName: b.lastRequest.PartitionName,
		IntentName:    b.lastRequest.IntentName,
		AssetName:     b.lastRequest.AssetName,
		Target:        b.lastRequest.Target,
		Manifest:      b.lastRequest.Manifest,
		WorkspaceDir:  b.lastRequest.WorkspaceDir,
		AppCommand:    b.lastRequest.AppCommand,
		Context:       cloneStringMap(b.lastRequest.Context),
		Env:           cloneStringMap(b.lastRequest.Env),
		DesiredHash:   b.lastRequest.DesiredHash,
		Tags:          cloneStringMap(b.lastRequest.Tags),
	}
}

func (b *Backend) Synthesize(_ context.Context, req StackRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	return nil
}

func (b *Backend) CheckEnvironment(_ context.Context, req StackRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	if b.bootstrapReady[envKey(req.Target.Account, req.Target.Region)] {
		return nil
	}
	return fmt.Errorf("cdk bootstrap stack %q not found in %s/%s", "CDKToolkit", req.Target.Account, req.Target.Region)
}

func (b *Backend) GetStack(_ context.Context, req StackRequest) (StackState, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	state, ok := b.stacks[stackKey(req.Target.Account, req.Target.Region, req.Manifest.StackName)]
	return cloneStackState(state), ok, nil
}

func (b *Backend) DetectDrift(_ context.Context, req StackRequest) (StackDriftStatus, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	if status, ok := b.drift[stackKey(req.Target.Account, req.Target.Region, req.Manifest.StackName)]; ok {
		return status, nil
	}
	return StackDriftInSync, nil
}

func (b *Backend) DeployStack(_ context.Context, req StackRequest) (StackState, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	key := stackKey(req.Target.Account, req.Target.Region, req.Manifest.StackName)
	state := b.stacks[key]
	state.ID = fmt.Sprintf("arn:aws:cloudformation:%s:%s:stack/%s/test", req.Target.Region, req.Target.Account, req.Manifest.StackName)
	state.Name = req.Manifest.StackName
	state.Status = "CREATE_COMPLETE"
	state.Tags = cloneStringMap(req.Tags)
	if len(state.Outputs) == 0 {
		state.Outputs = map[string]string{}
	}
	b.stacks[key] = state
	b.drift[key] = StackDriftInSync
	return cloneStackState(state), nil
}

func (b *Backend) DeleteStack(_ context.Context, req StackRequest) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastRequest = cloneRequest(req)
	key := stackKey(req.Target.Account, req.Target.Region, req.Manifest.StackName)
	delete(b.stacks, key)
	delete(b.drift, key)
	return nil
}

func envKey(account, region string) string {
	return account + "/" + region
}

func stackKey(account, region, stackName string) string {
	return envKey(account, region) + "/" + stackName
}

func cloneStackState(in StackState) StackState {
	return StackState{
		ID:      in.ID,
		Name:    in.Name,
		Status:  in.Status,
		Tags:    cloneStringMap(in.Tags),
		Outputs: cloneStringMap(in.Outputs),
	}
}

func cloneRequest(in StackRequest) StackRequest {
	return StackRequest{
		PartitionName: in.PartitionName,
		IntentName:    in.IntentName,
		AssetName:     in.AssetName,
		Target:        in.Target,
		Manifest:      in.Manifest,
		WorkspaceDir:  in.WorkspaceDir,
		AppCommand:    in.AppCommand,
		Context:       cloneStringMap(in.Context),
		Env:           cloneStringMap(in.Env),
		DesiredHash:   in.DesiredHash,
		Tags:          cloneStringMap(in.Tags),
	}
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func (b *Backend) UpsertFileSystem(ctx context.Context, fs FileSystem) (string, error) { return "fs-test", nil }
func (b *Backend) GetFileSystem(ctx context.Context, fsID string) (FileSystem, bool, error) {
	return FileSystem{ID: fsID, Hash: "test"}, true, nil
}
func (b *Backend) DeleteFileSystem(ctx context.Context, fsID string) error { return nil }
func (b *Backend) UpsertParameter(ctx context.Context, param Parameter) error { return nil }
func (b *Backend) GetParameter(ctx context.Context, name string) (Parameter, bool, error) {
	return Parameter{Name: name, Hash: "test"}, true, nil
}
func (b *Backend) DeleteParameter(ctx context.Context, name string) error { return nil }
func (b *Backend) UpsertSecret(ctx context.Context, secret Secret) (string, error) {
	return "arn:aws:secretsmanager:test:test:secret:test", nil
}
func (b *Backend) GetSecret(ctx context.Context, secretID string) (Secret, bool, error) {
	return Secret{ID: secretID, Hash: "test"}, true, nil
}
func (b *Backend) DeleteSecret(ctx context.Context, secretID string) error { return nil }
func (b *Backend) UpsertService(ctx context.Context, svc ECSService) error { return nil }
func (b *Backend) GetService(ctx context.Context, cluster, name string) (ECSService, bool, error) {
	return ECSService{Name: name, Hash: "test"}, true, nil
}
func (b *Backend) DeleteService(ctx context.Context, cluster, name string) error { return nil }
func (b *Backend) UpsertLoadBalancer(ctx context.Context, lb LoadBalancer) (string, error) {
	return "arn:aws:elasticloadbalancing:test:test:loadbalancer/test", nil
}
func (b *Backend) GetLoadBalancer(ctx context.Context, name string) (LoadBalancer, bool, error) {
	return LoadBalancer{Name: name, Hash: "test"}, true, nil
}
func (b *Backend) DeleteLoadBalancer(ctx context.Context, arn string) error { return nil }
func (b *Backend) UpsertTargetGroup(ctx context.Context, tg TargetGroup) (string, error) {
	return "arn:aws:elasticloadbalancing:test:test:targetgroup/test", nil
}
func (b *Backend) GetTargetGroup(ctx context.Context, name string) (TargetGroup, bool, error) {
	return TargetGroup{Name: name, Hash: "test"}, true, nil
}
func (b *Backend) DeleteTargetGroup(ctx context.Context, arn string) error { return nil }
func (b *Backend) UpsertListener(ctx context.Context, listener Listener) (string, error) {
	return "arn:aws:elasticloadbalancing:test:test:listener/test", nil
}
func (b *Backend) GetListener(ctx context.Context, lbARN string, port int) (Listener, bool, error) {
	return Listener{Hash: "test"}, true, nil
}
func (b *Backend) DeleteListener(ctx context.Context, arn string) error { return nil }
func (b *Backend) UpsertBucket(ctx context.Context, bucket BucketSpec) error { return nil }
func (b *Backend) GetBucket(ctx context.Context, name string) (BucketSpec, bool, error) {
	return BucketSpec{Name: name, Hash: "test"}, true, nil
}
func (b *Backend) DeleteBucket(ctx context.Context, name string) error { return nil }
func (b *Backend) UpsertLogGroup(ctx context.Context, group LogGroup) error { return nil }
func (b *Backend) GetLogGroup(ctx context.Context, name string) (LogGroup, bool, error) {
	return LogGroup{Name: name, Hash: "test"}, true, nil
}
