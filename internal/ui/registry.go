package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/observedgen"
	"github.com/rydzu/ainfra/guardian/internal/registry"
)

type pusherListResponse struct {
	Pushers []registry.Pusher `json:"pushers"`
}

type repoListResponse struct {
	Repos []registry.Repo `json:"repos"`
}

func (s *Server) handlePushers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		pushers, err := registry.New(s.store).ListPushers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, pusherListResponse{Pushers: pushers})
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
	name := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/pushers/"), "/")
	if name == "" {
		writeError(w, http.StatusBadRequest, fmt.Errorf("pusher name is required"))
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
			if errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusNotFound, fmt.Errorf("repo %q not found", name))
				return
			}
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, repo)
	case action == "" && r.Method == http.MethodDelete:
		if err := registryStore.DeleteRepo(r.Context(), name); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "name": name})
	case action == "ingest" && r.Method == http.MethodPost:
		s.handleRepoIngest(w, r, name)
	case action == "notify" && r.Method == http.MethodPost:
		s.handleRepoNotify(w, r, name)
	default:
		writeMethodNotAllowed(w, http.MethodGet, http.MethodDelete)
	}
}

func (s *Server) handleRepoIngest(w http.ResponseWriter, r *http.Request, name string) {
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
		"repo":      repo.Name,
		"partition": repo.Partition,
		"intents":   created,
	})
}

// handleRepoNotify records a CI merge notification and, when the cloud
// assembly is already in MonoFS, regenerates the observed partition intents.
func (s *Server) handleRepoNotify(w http.ResponseWriter, r *http.Request, name string) {
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
	pusher, err := registryStore.GetPusher(ctx, repo.Pusher)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, fmt.Errorf("repo %q references unknown pusher %q", repo.Name, repo.Pusher)
	}
	account := firstNonEmpty(repo.Account, pusher.Account)
	region := firstNonEmpty(repo.Region, pusher.Region)
	if account == "" || region == "" {
		err := fmt.Errorf("account and region must be set on the repo or pusher")
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, err
	}
	assemblyDir, err := s.resolveRepoAssembly(ctx, repo)
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, err
	}
	created, err := observedgen.GenerateToStore(ctx, s.store, observedgen.StoreOptions{
		AssemblyDir: assemblyDir,
		Partition:   repo.Partition,
		Account:     account,
		Region:      region,
		Pusher:      repo.Pusher,
		PayloadDir:  repo.ObservedPartitionPath(),
	})
	if err != nil {
		_ = registryStore.RecordSync(ctx, repo.Name, "", err)
		return nil, err
	}
	if err := registryStore.RecordSync(ctx, repo.Name, "", nil); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *Server) resolveRepoAssembly(ctx context.Context, repo *registry.Repo) (string, error) {
	candidates := make([]string, 0, 4)
	if source := strings.TrimSpace(repo.Source); strings.HasPrefix(source, "/") {
		candidates = append(candidates, source, strings.TrimRight(source, "/")+"/cdk.out")
	}
	payloadDir := repo.ObservedPartitionPath()
	candidates = append(candidates, strings.TrimRight(payloadDir, "/")+"/cdk.out", payloadDir)
	for _, candidate := range candidates {
		if _, err := s.store.ReadFile(ctx, strings.TrimRight(candidate, "/")+"/manifest.json"); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no cloud assembly found for repo %s; push a cdk.out to %s or run `guardianctl repo sync --name %s`", repo.Name, strings.TrimRight(payloadDir, "/")+"/cdk.out", repo.Name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
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
