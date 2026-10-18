package ui

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/internal/registry"
)

// PartitionAssignments is the partition-scoped ownership view: the repos that
// feed the partition, the pushers its intents target, and the canonical paths.
// It is computed server-side so the browser never needs the global registry.
type PartitionAssignments struct {
	Repos    []PartitionRepoAssignment   `json:"repos,omitempty"`
	Pushers  []PartitionPusherAssignment `json:"pushers,omitempty"`
	Warnings []string                    `json:"warnings,omitempty"`
}

type PartitionRepoAssignment struct {
	Name          string               `json:"name"`
	SourceType    string               `json:"sourceType,omitempty"`
	Source        string               `json:"source,omitempty"`
	Branch        string               `json:"branch,omitempty"`
	Commit        string               `json:"commit,omitempty"`
	LastSyncedAt  time.Time            `json:"lastSyncedAt,omitempty"`
	LastSyncError string               `json:"lastSyncError,omitempty"`
	Binding       registry.RepoBinding `json:"binding"`
	RepoPath      string               `json:"repoPath"`
	RepoPathHint  string               `json:"repoPathHint,omitempty"`
	MonoFSPath    string               `json:"monofsPath"`
	PayloadPath   string               `json:"payloadPath"`
	ConfigPath    string               `json:"configPath"`
	IntentsPath   string               `json:"intentsPath"`
}

type PartitionPusherAssignment struct {
	Name           string   `json:"name"`
	Account        string   `json:"account,omitempty"`
	Region         string   `json:"region,omitempty"`
	ObservedAPIURL string   `json:"observedApiUrl,omitempty"`
	Registered     bool     `json:"registered"`
	Intents        []string `json:"intents,omitempty"`
}

func (s *Server) buildPartitionAssignments(ctx context.Context, partition string, intents []IntentDocument) *PartitionAssignments {
	out := &PartitionAssignments{}
	store := registry.New(s.store)

	repos, err := store.ListRepos(ctx)
	if err != nil {
		out.Warnings = append(out.Warnings, "registry: "+err.Error())
	}
	pushers, perr := store.ListPushers(ctx)
	if perr != nil {
		out.Warnings = append(out.Warnings, "pushers: "+perr.Error())
	}
	pusherByName := map[string]registry.Pusher{}
	for _, pusher := range pushers {
		pusherByName[pusher.Name] = pusher
	}

	intentsByPusher := map[string][]string{}
	intentsByName := map[string]IntentDocument{}
	for _, intent := range intents {
		name := strings.TrimSpace(intent.Name)
		if name == "" {
			name = strings.TrimSpace(intent.Manifest.Metadata.Name)
		}
		intentsByName[name] = intent
		pusher := strings.TrimSpace(intent.Manifest.Spec.TargetPusher)
		if pusher == "" {
			continue
		}
		intentsByPusher[pusher] = append(intentsByPusher[pusher], name)
	}

	for _, repo := range repos {
		for _, binding := range repo.BindingList() {
			if strings.TrimSpace(binding.Partition) != partition {
				continue
			}
			payloadPath := binding.PayloadDir()
			assignment := PartitionRepoAssignment{
				Name:          repo.Name,
				SourceType:    repo.SourceType,
				Source:        repo.Source,
				Branch:        repo.Branch,
				Commit:        repo.LastSyncCommit,
				LastSyncedAt:  repo.LastSyncedAt,
				LastSyncError: repo.LastSyncError,
				Binding:       binding,
				RepoPath:      "guardian/partitions/" + partition + "/",
				RepoPathHint:  "or .guardian/config.yaml at the repository root for CDK app repos",
				MonoFSPath:    paths.PartitionRoot(partition),
				PayloadPath:   payloadPath,
				ConfigPath:    paths.PartitionConfig(partition),
				IntentsPath:   paths.PartitionIntentsDir(partition),
			}
			out.Repos = append(out.Repos, assignment)
			if strings.TrimSpace(repo.LastSyncError) != "" {
				out.Warnings = append(out.Warnings, "repo "+repo.Name+" sync failed: "+repo.LastSyncError)
			}
			if strings.TrimSpace(binding.Account) != "" && !assignmentBindingAccountMatches(intents, binding.Account) {
				out.Warnings = append(out.Warnings, "repo "+repo.Name+" binding account "+binding.Account+" does not match intent targets")
			}
			if strings.TrimSpace(binding.Region) != "" && !assignmentBindingRegionMatches(intents, binding.Region) {
				out.Warnings = append(out.Warnings, "repo "+repo.Name+" binding region "+binding.Region+" does not match intent targets")
			}
		}
	}
	if len(out.Repos) == 0 {
		out.Warnings = append(out.Warnings, "no repository assigned; desired state has no source of truth")
	}

	for pusherName, intentNames := range intentsByPusher {
		sort.Strings(intentNames)
		pusher, ok := pusherByName[pusherName]
		item := PartitionPusherAssignment{
			Name:       pusherName,
			Registered: ok,
			Intents:    intentNames,
		}
		if ok {
			item.Account = pusher.Account
			item.Region = pusher.Region
			item.ObservedAPIURL = pusher.ObservedAPIURL
		} else {
			out.Warnings = append(out.Warnings, "target pusher "+pusherName+" is not registered")
		}
		// Check each intent's explicit account/region against the pusher.
		for _, intentName := range intentNames {
			intent, found := intentsByName[intentName]
			if !found {
				continue
			}
			account := strings.TrimSpace(intent.Manifest.Spec.Target.Account)
			region := strings.TrimSpace(intent.Manifest.Spec.Target.Region)
			if ok && account != "" && pusher.Account != "" && account != pusher.Account {
				out.Warnings = append(out.Warnings, "intent "+intentName+" targets account "+account+", pusher "+pusherName+" is "+pusher.Account)
			}
			if ok && region != "" && pusher.Region != "" && region != pusher.Region {
				out.Warnings = append(out.Warnings, "intent "+intentName+" targets region "+region+", pusher "+pusherName+" is "+pusher.Region)
			}
		}
		out.Pushers = append(out.Pushers, item)
	}
	sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Name < out.Repos[j].Name })
	sort.Slice(out.Pushers, func(i, j int) bool { return out.Pushers[i].Name < out.Pushers[j].Name })
	return out
}

func assignmentBindingAccountMatches(intents []IntentDocument, account string) bool {
	for _, intent := range intents {
		target := strings.TrimSpace(intent.Manifest.Spec.Target.Account)
		if target != "" && target != account {
			return false
		}
	}
	return true
}

func assignmentBindingRegionMatches(intents []IntentDocument, region string) bool {
	for _, intent := range intents {
		target := strings.TrimSpace(intent.Manifest.Spec.Target.Region)
		if target != "" && target != region {
			return false
		}
	}
	return true
}
