package registry

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
)

// Store persists pusher and repo registrations.
type Store struct {
	store guardianapi.Store
	now   func() time.Time
}

// New builds a registry store.
func New(store guardianapi.Store) *Store {
	return &Store{store: store, now: func() time.Time { return time.Now().UTC() }}
}

// ListPushers returns all registered pushers sorted by name.
func (s *Store) ListPushers(ctx context.Context) ([]Pusher, error) {
	entries, err := s.store.ListDir(ctx, paths.PushersRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Pusher, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		pusher, err := s.GetPusher(ctx, name)
		if err != nil {
			continue
		}
		out = append(out, *pusher)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetPusher loads one pusher registration.
func (s *Store) GetPusher(ctx context.Context, name string) (*Pusher, error) {
	data, err := s.store.ReadFile(ctx, paths.PusherRecord(name))
	if err != nil {
		return nil, err
	}
	var pusher Pusher
	if err := json.Unmarshal(data, &pusher); err != nil {
		return nil, err
	}
	pusher.Name = strings.TrimSpace(pusher.Name)
	if pusher.Name == "" {
		pusher.Name = name
	}
	return &pusher, nil
}

// PutPusher creates or updates a pusher registration.
func (s *Store) PutPusher(ctx context.Context, pusher *Pusher) error {
	if err := pusher.Validate(); err != nil {
		return err
	}
	existing, err := s.GetPusher(ctx, pusher.Name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := s.now()
	if existing != nil {
		pusher.CreatedAt = existing.CreatedAt
	}
	if pusher.CreatedAt.IsZero() {
		pusher.CreatedAt = now
	}
	pusher.UpdatedAt = now
	return s.write(ctx, paths.PusherRecord(pusher.Name), pusher, "put pusher registration")
}

// DeletePusher removes a pusher registration.
func (s *Store) DeletePusher(ctx context.Context, name string) error {
	_, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.PusherRecord(name)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "delete pusher registration"},
	})
	return err
}

// ListRepos returns all registered repos sorted by name.
func (s *Store) ListRepos(ctx context.Context) ([]Repo, error) {
	entries, err := s.store.ListDir(ctx, paths.ReposRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Repo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name, ".json")
		repo, err := s.GetRepo(ctx, name)
		if err != nil {
			continue
		}
		out = append(out, *repo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// GetRepo loads one repo registration.
func (s *Store) GetRepo(ctx context.Context, name string) (*Repo, error) {
	data, err := s.store.ReadFile(ctx, paths.RepoRecord(name))
	if err != nil {
		return nil, err
	}
	var repo Repo
	if err := json.Unmarshal(data, &repo); err != nil {
		return nil, err
	}
	repo.Name = strings.TrimSpace(repo.Name)
	if repo.Name == "" {
		repo.Name = name
	}
	repo.Normalize()
	return &repo, nil
}

// PutRepo creates or updates a repo registration. The referenced pusher must
// exist.
func (s *Store) PutRepo(ctx context.Context, repo *Repo) error {
	if err := repo.Validate(); err != nil {
		return err
	}
	if repo.Pusher != "" {
		if _, err := s.GetPusher(ctx, repo.Pusher); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &UnknownPusherError{Name: repo.Pusher}
			}
			return err
		}
	}
	existing, err := s.GetRepo(ctx, repo.Name)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	now := s.now()
	if existing != nil {
		repo.CreatedAt = existing.CreatedAt
		repo.LastSyncedAt = existing.LastSyncedAt
		repo.LastSyncCommit = existing.LastSyncCommit
		repo.LastSyncError = existing.LastSyncError
		repo.LastNotifiedAt = existing.LastNotifiedAt
		repo.LastNotifiedCommit = existing.LastNotifiedCommit
	}
	if repo.CreatedAt.IsZero() {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "put repo registration")
}

// RecordSync stores the outcome of a repo sync.
func (s *Store) RecordSync(ctx context.Context, name, commit string, syncErr error) error {
	repo, err := s.GetRepo(ctx, name)
	if err != nil {
		return err
	}
	now := s.now()
	repo.UpdatedAt = now
	repo.LastSyncCommit = strings.TrimSpace(commit)
	if syncErr != nil {
		repo.LastSyncError = syncErr.Error()
	} else {
		repo.LastSyncedAt = now
		repo.LastSyncError = ""
	}
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "record repo sync")
}

// RecordNotification stores a CI merge notification for a repo.
func (s *Store) RecordNotification(ctx context.Context, name, commit string) error {
	repo, err := s.GetRepo(ctx, name)
	if err != nil {
		return err
	}
	now := s.now()
	repo.UpdatedAt = now
	repo.LastNotifiedAt = now
	repo.LastNotifiedCommit = strings.TrimSpace(commit)
	return s.write(ctx, paths.RepoRecord(repo.Name), repo, "record repo notification")
}

// DeleteRepo removes a repo registration.
func (s *Store) DeleteRepo(ctx context.Context, name string) error {
	_, err := s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.RepoRecord(name)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "delete repo registration"},
	})
	return err
}

func (s *Store) write(ctx context.Context, logicalPath string, value any, reason string) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes: []guardianapi.PathWrite{{LogicalPath: logicalPath, Content: payload}},
		Context: guardianapi.MutationContext{
			PrincipalID: "guardiand",
			Reason:      reason,
		},
	})
	return err
}

// UnknownPusherError reports a repo referencing a missing pusher.
type UnknownPusherError struct {
	Name string
}

func (e *UnknownPusherError) Error() string {
	return "pusher " + e.Name + " is not registered"
}
