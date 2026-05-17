package awsdriver

import (
	"context"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
)

type BackendAPI interface {
	Synthesize(ctx context.Context, req StackRequest) error
	CheckEnvironment(ctx context.Context, req StackRequest) error
	GetStack(ctx context.Context, req StackRequest) (StackState, bool, error)
	DetectDrift(ctx context.Context, req StackRequest) (StackDriftStatus, error)
	DeployStack(ctx context.Context, req StackRequest) (StackState, error)
	DeleteStack(ctx context.Context, req StackRequest) error
}

type StackRequest struct {
	PartitionName string
	IntentName    string
	AssetName     string
	Target        targetdomain.Placement
	Manifest      stackPayload
	WorkspaceDir  string
	AppCommand    string
	Context       map[string]string
	Env           map[string]string
	DesiredHash   string
	Tags          map[string]string
}

type StackState struct {
	ID      string
	Name    string
	Status  string
	Tags    map[string]string
	Outputs map[string]string
}

type StackDriftStatus string

const (
	StackDriftUnknown StackDriftStatus = "UNKNOWN"
	StackDriftInSync  StackDriftStatus = "IN_SYNC"
	StackDriftDrifted StackDriftStatus = "DRIFTED"
)
