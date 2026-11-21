// Package display stores and compiles presentation-only display profiles.
// Profiles never change values, ACLs, storage, or audit rows.
package display

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/pkg/guardianapi"
	"github.com/radryc/monofs/pkg/display"
)

const (
	profilesDir  = "/.display/profiles"
	defaultsPath = "/.display/defaults.json"
	bundlesDir   = "/.display/bundles"
)

// Service reads and writes display profiles in the Guardian store.
type Service struct {
	store guardianapi.Store

	mu       sync.Mutex
	bundle   *display.Bundle
	version  string
	loadedAt time.Time
}

func NewService(store guardianapi.Store) *Service {
	return &Service{store: store}
}

// ProfilePath returns the logical path for a profile id.
func ProfilePath(id string) string { return path.Join(profilesDir, id+".json") }

// List returns the profiles visible to subject. Admins see every profile.
func (s *Service) List(ctx context.Context, subjects []string, admin bool) ([]display.Profile, error) {
	entries, err := s.store.ListDir(ctx, profilesDir)
	if err != nil {
		return nil, err
	}
	out := make([]display.Profile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		profile, err := s.Get(ctx, subjects, strings.TrimSuffix(entry.Name, ".json"), admin)
		if err != nil {
			continue
		}
		out = append(out, profile)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Get returns one profile if the caller may read it.
func (s *Service) Get(ctx context.Context, subjects []string, id string, admin bool) (display.Profile, error) {
	raw, err := s.store.ReadFile(ctx, ProfilePath(id))
	if err != nil {
		return display.Profile{}, err
	}
	var profile display.Profile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return display.Profile{}, fmt.Errorf("decode display profile: %w", err)
	}
	if !canRead(profile, subjects, admin) {
		return display.Profile{}, fmt.Errorf("display profile %q is not visible", id)
	}
	return profile, nil
}

// Create validates and stores a profile owned by subject.
func (s *Service) Create(ctx context.Context, subjects []string, profile display.Profile, admin bool) (display.Profile, error) {
	if err := profile.Validate(); err != nil {
		return display.Profile{}, err
	}
	if profile.Scope == "" {
		profile.Scope = "personal"
	}
	if !admin && profile.Scope != "personal" {
		return display.Profile{}, fmt.Errorf("only platform admins may create %s profiles", profile.Scope)
	}
	if profile.Scope == "personal" && profile.Owner == "" {
		profile.Owner = primarySubject(subjects)
	}
	if profile.Scope == "team" {
		profile.Owner = "team:" + profile.Team
		if len(profile.Readers) == 0 {
			profile.Readers = []string{profile.Owner}
		}
		if len(profile.Maintainers) == 0 {
			profile.Maintainers = []string{"team:" + profile.Team + "-maintainers"}
		}
	}
	if _, err := s.store.ReadFile(ctx, ProfilePath(profile.ID)); err == nil {
		return display.Profile{}, fmt.Errorf("display profile %q already exists", profile.ID)
	}
	if err := s.write(ctx, primarySubject(subjects), profile); err != nil {
		return display.Profile{}, err
	}
	s.invalidate()
	return profile, nil
}

// Update replaces an existing profile after authorization.
func (s *Service) Update(ctx context.Context, subjects []string, id string, profile display.Profile, admin bool) (display.Profile, error) {
	current, err := s.Get(ctx, subjects, id, admin)
	if err != nil {
		return display.Profile{}, err
	}
	if !canWrite(current, subjects, admin) {
		return display.Profile{}, fmt.Errorf("not authorized to update display profile %q", id)
	}
	profile.ID = id
	if profile.Scope == "" {
		profile.Scope = current.Scope
	}
	if profile.Owner == "" {
		profile.Owner = current.Owner
	}
	if err := profile.Validate(); err != nil {
		return display.Profile{}, err
	}
	if err := s.write(ctx, primarySubject(subjects), profile); err != nil {
		return display.Profile{}, err
	}
	s.invalidate()
	return profile, nil
}

// Delete removes a profile after authorization.
func (s *Service) Delete(ctx context.Context, subjects []string, id string, admin bool) error {
	current, err := s.Get(ctx, subjects, id, admin)
	if err != nil {
		return err
	}
	if !canWrite(current, subjects, admin) {
		return fmt.Errorf("not authorized to delete display profile %q", id)
	}
	_, err = s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: ProfilePath(id)}},
		Context: mutationContext(primarySubject(subjects), "delete display profile"),
	})
	if err != nil {
		return err
	}
	s.invalidate()
	return nil
}

// Default returns the caller's default profile id, or "".
func (s *Service) Default(ctx context.Context, subjects []string) (string, error) {
	defaults, err := s.defaults(ctx)
	if err != nil {
		return "", err
	}
	for _, subject := range subjects {
		if id := strings.TrimSpace(defaults[subject]); id != "" {
			return id, nil
		}
	}
	return "", nil
}

// SetDefault stores the caller's default profile.
func (s *Service) SetDefault(ctx context.Context, subjects []string, profileID string) error {
	if strings.TrimSpace(profileID) == "" || profileID == "canonical" {
		return fmt.Errorf("profile id is required")
	}
	if _, err := s.Get(ctx, subjects, profileID, false); err != nil {
		return err
	}
	defaults, err := s.defaults(ctx)
	if err != nil {
		return err
	}
	defaults[primarySubject(subjects)] = profileID
	payload, _ := json.MarshalIndent(defaults, "", "  ")
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: defaultsPath, Content: append(payload, '\n')}},
		Context: mutationContext(primarySubject(subjects), "set default display profile"),
	})
	return err
}

func (s *Service) defaults(ctx context.Context) (map[string]string, error) {
	raw, err := s.store.ReadFile(ctx, defaultsPath)
	if err != nil {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Bundle compiles every visible profile into the versioned bundle consumed by
// Doctor and the CLIs.
func (s *Service) Bundle(ctx context.Context) (*display.Bundle, string, error) {
	s.mu.Lock()
	if s.bundle != nil && time.Since(s.loadedAt) < 5*time.Second {
		bundle, version := s.bundle, s.version
		s.mu.Unlock()
		return bundle, version, nil
	}
	s.mu.Unlock()

	profiles, err := s.List(ctx, []string{"system"}, true)
	if err != nil {
		return nil, "", err
	}
	bundle := &display.Bundle{Profiles: []display.CompiledProfile{}, Defaults: map[string]string{}}
	if repos, repoErr := registry.New(s.store).ListRepos(ctx); repoErr == nil {
		for _, repo := range repos {
			for _, binding := range repo.Bindings {
				if binding.Display == nil || len(binding.Display.Keys) == 0 {
					continue
				}
				bundle.Assignments = append(bundle.Assignments, display.Assignment{
					Repo:      repo.Name,
					Partition: binding.Partition,
					Profile:   binding.Display.Profile,
					Keys:      binding.Display.Keys,
				})
			}
		}
	}
	if defaults, err := s.defaults(ctx); err == nil {
		bundle.Defaults = defaults
	}
	for _, profile := range profiles {
		subjects := []string{}
		if profile.Owner != "" {
			subjects = append(subjects, profile.Owner)
		}
		for _, reader := range profile.Readers {
			subjects = append(subjects, reader)
		}
		subjects = append(subjects, profile.Maintainers...)
		bundle.Profiles = append(bundle.Profiles, display.CompiledProfile{
			ID:               profile.ID,
			Subjects:         subjects,
			Maintainers:      profile.Maintainers,
			Keys:             profile.Keys,
			Surfaces:         profile.Surfaces,
			IncludeCanonical: profile.IncludeCanonical,
		})
	}
	// Defaults are per-subject files; list them and decode the subject from the
	// file name is not possible (hash only), so the UI writes defaults into the
	// bundle via the effective endpoint instead. The bundle is still useful for
	// explicit X-Strata-Display-Profile requests.
	encoded, _ := json.Marshal(bundle)
	sum := sha256.Sum256(encoded)
	version := time.Now().UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(sum[:4])
	bundle.Version = version

	s.mu.Lock()
	s.bundle = bundle
	s.version = version
	s.loadedAt = time.Now()
	s.mu.Unlock()
	return bundle, version, nil
}

// Resolve returns the profile for a request. requested may be empty, in which
// case the caller's default is used.
func (s *Service) Resolve(ctx context.Context, subjects []string, requested string) (*display.CompiledProfile, string, error) {
	bundle, version, err := s.Bundle(ctx)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(requested) == "" {
		requested, _ = s.Default(ctx, subjects)
	}
	profile, ok := bundle.ProfileFor(subjects, requested)
	if !ok {
		return nil, version, fmt.Errorf("display profile %q is not available", requested)
	}
	return profile, version, nil
}

func (s *Service) write(ctx context.Context, subject string, profile display.Profile) error {
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: ProfilePath(profile.ID), Content: raw}},
		Context: mutationContext(subject, "upsert display profile"),
	})
	return err
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.bundle = nil
	s.loadedAt = time.Time{}
	s.mu.Unlock()
}

func mutationContext(subject, reason string) guardianapi.MutationContext {
	return guardianapi.MutationContext{
		PrincipalID: subject,
		Reason:      reason,
	}
}

func primarySubject(subjects []string) string {
	for _, subject := range subjects {
		if strings.TrimSpace(subject) != "" {
			return strings.TrimSpace(subject)
		}
	}
	return ""
}

func subjectMatches(candidates []string, subject string) bool {
	for _, candidate := range candidates {
		if candidate == "*" || candidate == subject {
			return true
		}
	}
	return false
}

func canRead(profile display.Profile, subjects []string, admin bool) bool {
	if admin || profile.Scope == "global" {
		return true
	}
	for _, subject := range subjects {
		if profile.Owner == subject || subjectMatches(profile.Readers, subject) || subjectMatches(profile.Maintainers, subject) {
			return true
		}
	}
	return false
}

func canWrite(profile display.Profile, subjects []string, admin bool) bool {
	if admin || profile.Scope == "global" {
		return true
	}
	for _, subject := range subjects {
		if profile.Owner == subject || subjectMatches(profile.Maintainers, subject) {
			return true
		}
	}
	return false
}
