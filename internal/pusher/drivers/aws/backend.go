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
