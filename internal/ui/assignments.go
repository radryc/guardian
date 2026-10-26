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
	// PartitionPath is where the partition bundle lives in the repo.
	PartitionPath string `json:"partitionPath,omitempty"`
	// SourcePath is the repo source (Git URL, checkout, or MonoFS path) used by
	// the partition. RepoPath is kept as a compatibility alias.
	SourcePath   string `json:"sourcePath,omitempty"`
	RepoPath     string `json:"repoPath,omitempty"`
	RepoPathHint string `json:"repoPathHint,omitempty"`
	MonoFSPath   string `json:"monofsPath"`
	PayloadPath  string `json:"payloadPath"`
	ConfigPath   string `json:"configPath"`
	IntentsPath  string `json:"intentsPath"`
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
	// Queue pushers (for example k8s-main and docker-main) are configured in
	// GUARDIAN_PUSHERS rather than the AWS read/registry store. Treat them as
	// registered so native control-plane partitions do not show a false
	// "target pusher is not registered" warning.
	configuredPushers := make(map[string]struct{}, len(s.pushers))
	for _, name := range s.pushers {
		if name = strings.TrimSpace(name); name != "" {
			configuredPushers[name] = struct{}{}
		}
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
			// Older/imported intents may only carry the placement cluster.
			// Treat that as the target pusher so the targets block stays useful.
			pusher = strings.TrimSpace(intent.Manifest.Spec.Target.Cluster)
		}
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
			partitionPath := binding.PartitionDir()
			sourcePath := firstNonEmpty(binding.SourcePath, repo.Source)
			assignment := PartitionRepoAssignment{
				Name:          repo.Name,
				SourceType:    repo.SourceType,
				Source:        repo.Source,
				Branch:        repo.Branch,
				Commit:        repo.LastSyncCommit,
				LastSyncedAt:  repo.LastSyncedAt,
				LastSyncError: repo.LastSyncError,
				Binding:       binding,
				PartitionPath: partitionPath,
				SourcePath:    sourcePath,
				RepoPath:      sourcePath,
				RepoPathHint:  "partition path is the repo-relative bundle; source path is the checkout/MonoFS source used by the partition",
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
		// A partition without a repo binding is native/control-plane state, not
		// an error. Represent it with the built-in native source so every
		// partition has an explicit assignment. If the partition's ImageBuild
		// assets name local build contexts, surface those as the source paths.
		sourcePaths := intentSourcePaths(intents)
		sourcePath := "native/control-plane"
		if len(sourcePaths) > 0 {
			sourcePath = strings.Join(sourcePaths, ", ")
		}
		out.Repos = append(out.Repos, PartitionRepoAssignment{
			Name:          registry.NativeRepoName,
			SourceType:    "native",
			Source:        "native",
			PartitionPath: paths.PartitionRoot(partition),
			SourcePath:    sourcePath,
			RepoPath:      sourcePath,
			RepoPathHint:  "no repository required; source paths come from the partition's image build contexts",
			MonoFSPath:    paths.PartitionRoot(partition),
			ConfigPath:    paths.PartitionConfig(partition),
			IntentsPath:   paths.PartitionIntentsDir(partition),
		})
	}

	for pusherName, intentNames := range intentsByPusher {
		sort.Strings(intentNames)
		pusher, registered := pusherByName[pusherName]
		_, configured := configuredPushers[pusherName]
		item := PartitionPusherAssignment{
			Name:       pusherName,
			Registered: registered || configured,
			Intents:    intentNames,
		}
		if registered {
			item.Account = pusher.Account
			item.Region = pusher.Region
			item.ObservedAPIURL = pusher.ObservedAPIURL
		} else if !configured {
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
			if registered && account != "" && pusher.Account != "" && account != pusher.Account {
				out.Warnings = append(out.Warnings, "intent "+intentName+" targets account "+account+", pusher "+pusherName+" is "+pusher.Account)
			}
			if registered && region != "" && pusher.Region != "" && region != pusher.Region {
				out.Warnings = append(out.Warnings, "intent "+intentName+" targets region "+region+", pusher "+pusherName+" is "+pusher.Region)
			}
		}
		out.Pushers = append(out.Pushers, item)
	}
	sort.Slice(out.Repos, func(i, j int) bool { return out.Repos[i].Name < out.Repos[j].Name })
	sort.Slice(out.Pushers, func(i, j int) bool { return out.Pushers[i].Name < out.Pushers[j].Name })
	return out
}

// intentSourcePaths returns the distinct local build contexts declared by a
// partition's ImageBuild assets. These are the source paths used to produce
// the partition's images when no repository binding exists.
func intentSourcePaths(intents []IntentDocument) []string {
	seen := map[string]bool{}
	for _, intent := range intents {
		for _, asset := range intent.Manifest.Spec.Assets {
			if !strings.EqualFold(strings.TrimSpace(asset.Type), "ImageBuild") {
				continue
			}
			value, _ := asset.Properties["buildContext"].(string)
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
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
