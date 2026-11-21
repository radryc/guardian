package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const proposalsVersion = 1
const maxProposals = 100

// Proposal is one locally recorded submission. It is a status cache, never a
// source of truth: the PR and MonoFS job remain authoritative.
type Proposal struct {
	Partition          string    `json:"partition"`
	WorkspaceID        string    `json:"workspaceId,omitempty"`
	StorageID          string    `json:"storageId,omitempty"`
	DisplayPath        string    `json:"displayPath,omitempty"`
	RepoURL            string    `json:"repoUrl,omitempty"`
	TargetBranch       string    `json:"targetBranch,omitempty"`
	PushedCommit       string    `json:"pushedCommit,omitempty"`
	PullRequestURL     string    `json:"pullRequestUrl,omitempty"`
	PullRequestCreated bool      `json:"pullRequestCreated,omitempty"`
	JobID              string    `json:"jobId,omitempty"`
	JobState           string    `json:"jobState,omitempty"`
	Status             string    `json:"status,omitempty"`
	Message            string    `json:"message,omitempty"`
	SubmittedAt        time.Time `json:"submittedAt"`
}

type proposalsFile struct {
	Version   int        `json:"version"`
	Proposals []Proposal `json:"proposals"`
}

// ProposalsPath is the local status cache location.
func ProposalsPath(root string) string {
	return filepath.Join(root, ".forge", "proposals.json")
}

// RecordProposal appends or replaces the entry for a partition and writes the
// cache atomically.
func RecordProposal(root string, proposal Proposal) error {
	if proposal.Partition == "" {
		return fmt.Errorf("proposal partition is required")
	}
	existing, err := LoadProposals(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	kept := make([]Proposal, 0, len(existing)+1)
	for _, item := range existing {
		if item.Partition == proposal.Partition && item.JobID != "" && item.JobID == proposal.JobID {
			continue
		}
		kept = append(kept, item)
	}
	kept = append(kept, proposal)
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].SubmittedAt.After(kept[j].SubmittedAt) })
	if len(kept) > maxProposals {
		kept = kept[:maxProposals]
	}

	data, err := json.MarshalIndent(proposalsFile{Version: proposalsVersion, Proposals: kept}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	path := ProposalsPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "proposals-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		// Windows cannot rename over an existing file.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return err
		}
		return os.Rename(tmpName, path)
	}
	return nil
}

// LoadProposals reads the local status cache, newest first.
func LoadProposals(root string) ([]Proposal, error) {
	data, err := os.ReadFile(ProposalsPath(root))
	if err != nil {
		return nil, err
	}
	var file proposalsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("decode proposals: %w", err)
	}
	out := append([]Proposal(nil), file.Proposals...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].SubmittedAt.After(out[j].SubmittedAt) })
	return out, nil
}
