package awsobserve

import (
	"context"
	"fmt"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cdktemplate"
	awsdriver "github.com/rydzu/ainfra/guardian/internal/pusher/drivers/aws"
)

// SourceDiffRequest asks the pusher to synthesize an in-memory CDK source tree
// and diff it against the live observed template.
type SourceDiffRequest struct {
	Account        string            `json:"account"`
	Region         string            `json:"region"`
	StackName      string            `json:"stackName"`
	StackID        string            `json:"stackId,omitempty"`
	SourceFiles    map[string]string `json:"sourceFiles"`
	AppCommand     string            `json:"appCommand,omitempty"`
	PackageManager string            `json:"packageManager,omitempty"`
	Context        map[string]string `json:"context,omitempty"`
}

// SourceDiffResult is the outcome of a source-upload diff.
type SourceDiffResult struct {
	StackName        string                 `json:"stackName"`
	ChangedResources []string               `json:"changedResources"`
	Resources        []cdktemplate.Resource `json:"resources,omitempty"`
	Summary          string                 `json:"summary"`
	Observed         StackState             `json:"observed"`
}

// DiffSource synthesizes the uploaded source, compares it with the live stack
// template and returns the changed resources. All connections are inbound to
// the pusher.
func (s *Service) DiffSource(ctx context.Context, req SourceDiffRequest) (*SourceDiffResult, error) {
	if s == nil || s.Refresher == nil {
		return nil, fmt.Errorf("observed diff is not configured")
	}
	stackName := strings.TrimSpace(req.StackName)
	if stackName == "" {
		return nil, fmt.Errorf("stackName is required")
	}
	if strings.TrimSpace(req.Region) == "" {
		return nil, fmt.Errorf("region is required")
	}
	if len(req.SourceFiles) == 0 {
		return nil, fmt.Errorf("sourceFiles is required")
	}
	stackID := strings.TrimSpace(req.StackID)
	if stackID == "" {
		stackID = stackName
	}
	desired, err := awsdriver.SynthTemplateFromFiles(ctx, s.StateDir, req.SourceFiles, req.AppCommand, req.PackageManager, stackID, req.Context)
	if err != nil {
		return nil, err
	}
	resources, err := cdktemplate.Inventory(desired)
	if err != nil {
		return nil, fmt.Errorf("inventory synthesized template: %w", err)
	}
	observedTemplate, err := s.Template(ctx, req.Account, req.Region, stackName)
	if err != nil {
		return nil, err
	}
	changed, err := cdktemplate.Compare(desired, observedTemplate)
	if err != nil {
		return nil, err
	}
	state, _ := s.Cache.Get(req.Account, req.Region, stackName)
	summary := "observed state matches the desired CDK app"
	if len(changed) > 0 {
		summary = fmt.Sprintf("%d resource(s) differ from desired", len(changed))
	}
	return &SourceDiffResult{
		StackName:        stackName,
		ChangedResources: changed,
		Resources:        resources,
		Summary:          summary,
		Observed:         state,
	}, nil
}
