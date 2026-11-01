package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/observedgen"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoimport"
)

type pusherListResponse struct {
	Pushers    []registry.Pusher            `json:"pushers"`
	Total      int                          `json:"total,omitempty"`
	Matched    int                          `json:"matched,omitempty"`
	NextCursor string                       `json:"nextCursor,omitempty"`
	HasMore    bool                         `json:"hasMore,omitempty"`
	Summary    *registry.PusherFleetSummary `json:"summary,omitempty"`
}

type repoListResponse struct {
	Repos []registry.Repo `json:"repos"`
}

func parseUpdatedSinceParam(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil && ms > 0 {
		if ms > 1e12 {
			return time.UnixMilli(ms).UTC()
		}
		return time.Unix(ms, 0).UTC()
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func (s *Server) handlePushers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		regStore := registry.New(s.store)
		q := r.URL.Query()
		if q.Get("summary_only") == "1" || q.Get("summary_only") == "true" {
			summary, err := regStore.SummarizePushers(r.Context())
			if err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
			writeJSON(w, http.StatusOK, summary)
			return
		}

		limit := 0
		if rawLimit := strings.TrimSpace(q.Get("limit")); rawLimit != "" {
			if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 {
				limit = n
			}
		}
		opts := registry.PusherListOptions{
			Provider:      q.Get("provider"),
			Region:        q.Get("region"),
			Account:       q.Get("account"),
			Cluster:       q.Get("cluster"),
			Status:        q.Get("status"),
			Version:       q.Get("version"),
			Query:         q.Get("q"),
			UpdatedSince:  parseUpdatedSinceParam(q.Get("updated_since")),
			OutliersFirst: q.Get("outliers") == "1" || q.Get("outliers") == "true",
			Limit:         limit,
			Cursor:        q.Get("cursor"),
		}
		all, err := regStore.ListPushers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		page := registry.FilterAndPaginatePushers(all, opts, time.Now().UTC())
		resp := pusherListResponse{
			Pushers:    page.Pushers,
			Total:      page.Total,
			Matched:    page.Matched,
			NextCursor: page.NextCursor,
			HasMore:    page.HasMore,
		}
		if q.Get("summary") == "1" || q.Get("summary") == "true" {
			summary := registry.ComputePusherFleetSummary(all, time.Now().UTC())
			resp.Summary = &summary
		}
		writeJSON(w, http.StatusOK, resp)
	case http.MethodPost:
		var pusher registry.Pusher
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&pusher); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode pusher: %w", err))
			return
		}
		if err := registry.New(s.store).PutPusher(r.Context(), &pusher); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, pusher)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handlePusherRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/pushers/"), "/")
	if rest == "summary" && r.Method == http.MethodGet {
		summary, err := registry.New(s.store).SummarizePushers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}
	name := rest
	action := ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		name = rest[:idx]
		action = strings.Trim(rest[idx+1:], "/")
	}
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("pusher name is required"))
		return
	}
	if action != "" && action != "metadata" {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown pusher action %q", action))
		return
	}
	switch r.Method {
	case http.MethodGet:
		pusher, err := registry.New(s.store).GetPusher(r.Context(), name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, fmt.Errorf("pusher %q not found", name))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, pusher)
	case http.MethodDelete:
		if err := registry.New(s.store).DeletePusher(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleRepos(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		repos, err := registry.New(s.store).ListRepos(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if !hasRepo(repos, registry.NativeRepoName) {
			repos = append(repos, nativeRepo())
			sort.Slice(repos, func(i, j int) bool { return repos[i].Name < repos[j].Name })
		}
		writeJSON(w, http.StatusOK, repoListResponse{Repos: repos})
	case http.MethodPost:
		var repo registry.Repo
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&repo); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode repo: %w", err))
			return
		}
		if err := registry.New(s.store).PutRepo(r.Context(), &repo); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusOK, repo)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodPost)
	}
}

func (s *Server) handleRepoRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/repos/"), "/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("repo name is required"))
		return
	}
	name := rest
	action := ""
	if idx := strings.Index(rest, "/"); idx >= 0 {
		name = rest[:idx]
		action = rest[idx+1:]
	}
	registryStore := registry.New(s.store)
	switch {
	case action == "" && r.Method == http.MethodGet:
		repo, err := registryStore.GetRepo(r.Context(), name)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && name == registry.NativeRepoName {
				writeJSON(w, http.StatusOK, nativeRepo())
				return
			}
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, repo)
	case action == "" && r.Method == http.MethodDelete:
		if name == registry.NativeRepoName {
			writeError(w, http.StatusBadRequest, fmt.Errorf("native source is built in and cannot be removed"))
			return
		}
		if err := registryStore.DeleteRepo(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	case action == "ingest" && r.Method == http.MethodPost:
		s.handleRepoIngest(w, r, name)
	case action == "bindings" && r.Method == http.MethodPut:
		s.handleRepoBindings(w, r, name)
	case action == "notify" && r.Method == http.MethodPost:
		s.handleRepoNotify(w, r, name)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete, http.MethodPut)
	}
}

// handleRepoBindings replaces a repo's partition assignments. Bindings carry no
// credentials, so they can be mirrored into shared files; the repo record keeps
// the source URL and pusher link private.
func (s *Server) handleRepoBindings(w http.ResponseWriter, r *http.Request, name string) {
	var body struct {
		Bindings *[]registry.RepoBinding `json:"bindings"`
		Pusher   *string                 `json:"pusher"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode bindings: %w", err))
		return
	}
	store := registry.New(s.store)
	repo, err := store.GetRepo(r.Context(), name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && name == registry.NativeRepoName {
			native := nativeRepo()
			repo = &native
		} else {
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
	}
	if body.Bindings != nil {
		repo.Bindings = *body.Bindings
		repo.Partition = ""
		repo.Path = ""
	}
	if body.Pusher != nil {
		repo.Pusher = strings.TrimSpace(*body.Pusher)
	}
	if err := store.PutRepo(r.Context(), repo); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, repo)
}

func (s *Server) handleRepoIngest(w http.ResponseWriter, r *http.Request, name string) {
	if name == registry.NativeRepoName {
		writeError(w, http.StatusBadRequest, fmt.Errorf("native source has no repository to ingest"))
		return
	}
	registryStore := registry.New(s.store)
	repo, err := registryStore.GetRepo(r.Context(), name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	created, err := s.ingestRepoFromStore(r.Context(), registryStore, repo)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"repo":       repo.Name,
		"partitions": repo.PartitionNames(),
		"intents":    created,
	})
}

// handleRepoNotify records a CI merge notification and, when the cloud
// assembly is already in MonoFS, regenerates the observed partition intents.
func (s *Server) handleRepoNotify(w http.ResponseWriter, r *http.Request, name string) {
	if name == registry.NativeRepoName {
		writeError(w, http.StatusBadRequest, fmt.Errorf("native source has no repository to notify"))
		return
	}
	registryStore := registry.New(s.store)
	repo, err := registryStore.GetRepo(r.Context(), name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
			return
		}
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	var body struct {
		Commit string `json:"commit,omitempty"`
		Branch string `json:"branch,omitempty"`
		Ingest *bool  `json:"ingest,omitempty"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, fmt.Errorf("decode notify request: %w", err))
			return
		}
	}
	if err := registryStore.RecordNotification(r.Context(), repo.Name, body.Commit); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	ingest := body.Ingest == nil || *body.Ingest
	response := map[string]any{
		"repo":   repo.Name,
		"commit": strings.TrimSpace(body.Commit),
		"branch": strings.TrimSpace(body.Branch),
		"status": "notified",
	}
	if ingest {
		created, ingestErr := s.ingestRepoFromStore(r.Context(), registryStore, repo)
		if ingestErr != nil {
			// Notification is recorded; report the ingest gap so CI can retry
			// after MonoFS has mirrored the merge.
			response["status"] = "notified_ingest_pending"
			response["ingestError"] = ingestErr.Error()
			writeJSON(w, http.StatusAccepted, response)
			return
		}
		response["status"] = "ingested"
		response["intents"] = created
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) ingestRepoFromStore(ctx context.Context, registryStore *registry.Store, repo *registry.Repo) ([]string, error) {
	if strings.TrimSpace(repo.Pusher) == "" {
		return nil, fmt.Errorf("repo %q has no pusher; assign a pusher before ingest", repo.Name)
	}
	imported, err := s.importRepoPartitionsFromStore(ctx, repo)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, err
	}
	if len(imported) > 0 {
		log.Printf("guardian ui: imported %d repo-authored partition(s) for repo %s", len(imported), repo.Name)
	}

	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, fmt.Errorf("repo %q references unknown pusher %q", repo.Name, repo.Pusher)
	}
	created := make([]string, 0)
	for _, binding := range repo.BindingList() {
		assemblyDir, err := s.resolveRepoAssembly(ctx, repo, binding)
		if err != nil {
			if containsString(imported, binding.Partition) {
				// GitOps-only partition: imported from the repo, no CDK assembly.
				continue
			}
			_ = registryStore.RecordSync(ctx, repo.Name, "", err)
			return nil, err
		}
		account := firstNonEmpty(binding.Account, repo.Account, pusher.Account)
		region := firstNonEmpty(binding.Region, repo.Region, pusher.Region)
		if account == "" || region == "" {
			err := fmt.Errorf("binding %s: account and region must be set on the binding, repo or pusher", binding.Partition)
			_ = registryStore.RecordSync(ctx, repo.Name, "", err)
			return nil, err
		}
		intents, err := observedgen.GenerateToStore(ctx, s.store, observedgen.StoreOptions{
			AssemblyDir: assemblyDir,
			Partition:   binding.Partition,
			Account:     account,
			Region:      region,
			Pusher:      repo.Pusher,
			PayloadDir:  binding.PayloadDir(),
			Selector:    binding.Selector,
		})
		if err != nil {
			_ = registryStore.RecordSync(ctx, repo.Name, "", err)
			return nil, err
		}
		created = append(created, intents...)
	}
	if err := registryStore.RecordSync(ctx, repo.Name, "", nil); err != nil {
		return nil, err
	}
	s.autoDeployPartitions(ctx, repo.PartitionNames())
	return created, nil
}

func (s *Server) resolveRepoAssembly(ctx context.Context, repo *registry.Repo, binding registry.RepoBinding) (string, error) {
	candidates := make([]string, 0, 4)
	if source := strings.TrimSpace(repo.Source); strings.HasPrefix(source, "/") {
		candidates = append(candidates, source, strings.TrimRight(source, "/")+"/cdk.out")
	}
	payloadDir := binding.PayloadDir()
	candidates = append(candidates, strings.TrimRight(payloadDir, "/")+"/cdk.out", payloadDir)
	for _, candidate := range candidates {
		if _, err := s.store.ReadFile(ctx, strings.TrimRight(candidate, "/")+"/manifest.json"); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no cloud assembly found for repo %s partition %s; push a cdk.out to %s or run `guardianctl repo sync --name %s`", repo.Name, binding.Partition, strings.TrimRight(payloadDir, "/")+"/cdk.out", repo.Name)
}

func nativeRepo() registry.Repo {
	return registry.Repo{
		APIVersion:  "guardian/v1alpha1",
		Kind:        "RepoRegistration",
		Name:        registry.NativeRepoName,
		SourceType:  "native",
		Description: "Native platform source; no repository required",
	}
}

func hasRepo(repos []registry.Repo, name string) bool {
	for _, repo := range repos {
		if repo.Name == name {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// importRepoPartitionsFromStore imports repo-authored partition bundles from
// guardian/partitions/<name>/ when the repo source is a MonoFS logical path.
// It delegates to repoimport so the UI and daemon share one importer.
func (s *Server) importRepoPartitionsFromStore(ctx context.Context, repo *registry.Repo) ([]string, error) {
	source := strings.TrimSpace(repo.Source)
	if !strings.HasPrefix(source, "/") {
		return nil, nil
	}
	src := repoimport.NewStoreSource(s.store, source)
	imported := make([]string, 0)
	for _, binding := range repo.BindingList() {
		result, err := repoimport.Import(ctx, src, s.store, repoimport.Options{
			RepoName:      repo.Name,
			Partition:     binding.Partition,
			PartitionPath: binding.PartitionDir(),
			Principal:     s.principalID,
			Source:        repo.Source,
			Prune:         true,
			IgnoreMissing: true,
		})
		if err != nil {
			return imported, err
		}
		if result.Changed {
			imported = append(imported, binding.Partition)
		}
	}
	return imported, nil
}

// knownPushers merges configured pushers with store-registered pushers so
// intent validation accepts observed intents targeting dynamically registered
// AWS pushers.
func (s *Server) knownPushers(ctx context.Context) []string {
	names := append([]string(nil), s.pushers...)
	registered, err := registry.New(s.store).ListPushers(ctx)
	if err == nil {
		for _, pusher := range registered {
			names = append(names, pusher.Name)
		}
	}
	sort.Strings(names)
	out := names[:0]
	for _, name := range names {
		if len(out) == 0 || out[len(out)-1] != name {
			out = append(out, name)
		}
	}
	return out
}
