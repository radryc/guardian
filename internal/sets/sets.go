// Package sets stores virtual navigation sets. A set is metadata only: it
// groups partitions for the UI and never changes ownership or deployment.
package sets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/paths"
	"github.com/radryc/guardian/pkg/guardianapi"
)

var (
	idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

	ErrNotEmpty       = errors.New("set is not empty")
	ErrCycle          = errors.New("set parent would create a cycle")
	ErrParentNotFound = errors.New("parent set not found")
)

type Set struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	ParentID    string            `json:"parentId,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Pinned      bool              `json:"pinned,omitempty"`
	Partitions  []string          `json:"partitions,omitempty"`
	CreatedAt   time.Time         `json:"createdAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
}

type Store struct {
	store guardianapi.Store
	now   func() time.Time
}

func New(store guardianapi.Store) *Store {
	return &Store{store: store, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Store) List(ctx context.Context) ([]Set, error) {
	entries, err := s.store.ListDir(ctx, paths.SetsRoot())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Set, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir || !strings.HasSuffix(entry.Name, ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name, ".json")
		set, err := s.Get(ctx, id)
		if err != nil {
			continue
		}
		out = append(out, *set)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pinned != out[j].Pinned {
			return out[i].Pinned
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// FindByName returns the first set with the given display name. Set names are
// not unique in storage, but the demo/default sets use names as stable keys.
func (s *Store) FindByName(ctx context.Context, name string) (*Set, error) {
	name = strings.TrimSpace(name)
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Name == name {
			return &all[i], nil
		}
	}
	return nil, os.ErrNotExist
}

func (s *Store) Get(ctx context.Context, id string) (*Set, error) {
	data, err := s.store.ReadFile(ctx, paths.SetRecord(id))
	if err != nil {
		return nil, err
	}
	var set Set
	if err := json.Unmarshal(data, &set); err != nil {
		return nil, err
	}
	set.ID = strings.TrimSpace(set.ID)
	if set.ID == "" {
		set.ID = id
	}
	set.Normalize()
	return &set, nil
}

func (s *Store) Create(ctx context.Context, set *Set) (*Set, error) {
	if set == nil {
		return nil, fmt.Errorf("set is nil")
	}
	set.Normalize()
	if set.Name == "" {
		return nil, fmt.Errorf("set name is required")
	}
	if set.ID == "" {
		set.ID = slugify(set.Name)
	}
	if !idPattern.MatchString(set.ID) {
		return nil, fmt.Errorf("invalid set id %q", set.ID)
	}
	// Keep IDs unique by suffixing a counter.
	base := set.ID
	for i := 2; ; i++ {
		if _, err := s.Get(ctx, set.ID); errors.Is(err, os.ErrNotExist) {
			break
		} else if err != nil {
			return nil, err
		}
		set.ID = fmt.Sprintf("%s-%d", base, i)
	}
	if err := s.validateParent(ctx, set.ID, set.ParentID); err != nil {
		return nil, err
	}
	set.APIVersion = "guardian/v1alpha1"
	set.Kind = "Set"
	now := s.now()
	set.CreatedAt = now
	set.UpdatedAt = now
	if err := s.write(ctx, set); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *Store) Update(ctx context.Context, set *Set) (*Set, error) {
	if set == nil {
		return nil, fmt.Errorf("set is nil")
	}
	existing, err := s.Get(ctx, set.ID)
	if err != nil {
		return nil, err
	}
	set.Normalize()
	if set.Name == "" {
		return nil, fmt.Errorf("set name is required")
	}
	if err := s.validateParent(ctx, set.ID, set.ParentID); err != nil {
		return nil, err
	}
	set.APIVersion = "guardian/v1alpha1"
	set.Kind = "Set"
	set.CreatedAt = existing.CreatedAt
	set.UpdatedAt = s.now()
	if err := s.write(ctx, set); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *Store) Move(ctx context.Context, id, parentID string) (*Set, error) {
	set, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	set.ParentID = strings.TrimSpace(parentID)
	if err := s.validateParent(ctx, id, set.ParentID); err != nil {
		return nil, err
	}
	return s.Update(ctx, set)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	set, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	all, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, candidate := range all {
		if candidate.ParentID == id {
			return ErrNotEmpty
		}
	}
	if len(set.Partitions) > 0 {
		return ErrNotEmpty
	}
	_, err = s.store.DeletePaths(ctx, guardianapi.DeleteBatch{
		Deletes: []guardianapi.PathDelete{{LogicalPath: paths.SetRecord(id)}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "delete virtual set"},
	})
	return err
}

func (s *Store) AddPartitions(ctx context.Context, id string, add []string, remove []string, primary bool) (*Set, error) {
	set, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	removeSet := map[string]bool{}
	for _, name := range remove {
		name = strings.TrimSpace(name)
		if name != "" {
			removeSet[name] = true
		}
	}
	keep := make([]string, 0, len(set.Partitions))
	for _, name := range set.Partitions {
		if !removeSet[name] {
			keep = append(keep, name)
		}
	}
	seen := map[string]bool{}
	for _, name := range keep {
		seen[name] = true
	}
	for _, name := range add {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		keep = append(keep, name)
	}
	sort.Strings(keep)
	set.Partitions = keep
	updated, err := s.Update(ctx, set)
	if err != nil {
		return nil, err
	}
	if primary {
		for _, name := range keep {
			if err := s.SetPrimary(ctx, id, name); err != nil {
				return nil, err
			}
		}
	}
	return updated, nil
}

// SyncPartitionConfig applies the set membership declared by a partition
// manifest. Membership is additive: sets that already contain the partition
// are left alone, and manually-added memberships are preserved. A missing set
// is created using the reference as its display name so a partition config can
// define its own grouping without a separate seed step.
func (s *Store) SyncPartitionConfig(ctx context.Context, partition string, refs []string, primary string) error {
	partition = strings.TrimSpace(partition)
	if partition == "" {
		return fmt.Errorf("partition name is required")
	}
	primary = strings.TrimSpace(primary)
	refs = normalizeList(refs)
	if primary != "" {
		refs = normalizeList(append(refs, primary))
	}
	for _, ref := range refs {
		set, err := s.resolveOrCreate(ctx, ref)
		if err != nil {
			return err
		}
		isPrimary := primary != "" && (ref == primary || set.ID == primary || set.Name == primary)
		if err := s.ensureMembership(ctx, set.ID, partition, isPrimary); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) resolveOrCreate(ctx context.Context, ref string) (*Set, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, fmt.Errorf("set name is required")
	}
	if set, err := s.Get(ctx, ref); err == nil {
		return set, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if set, err := s.FindByName(ctx, ref); err == nil {
		return set, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return s.Create(ctx, &Set{Name: ref})
}

func (s *Store) ensureMembership(ctx context.Context, setID, partition string, primary bool) error {
	set, err := s.Get(ctx, setID)
	if err != nil {
		return err
	}
	if !containsString(set.Partitions, partition) {
		set.Partitions = append(set.Partitions, partition)
		sort.Strings(set.Partitions)
		if _, err := s.Update(ctx, set); err != nil {
			return err
		}
	}
	if !primary {
		return nil
	}
	current, err := s.PrimarySet(ctx, partition)
	if err != nil {
		return err
	}
	if current == setID {
		return nil
	}
	return s.SetPrimary(ctx, setID, partition)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func (s *Store) SetPrimary(ctx context.Context, setID, partition string) error {
	record := primaryRecord{SetID: strings.TrimSpace(setID)}
	payload, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.SetPrimaryRecord(partition), Content: payload}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "set primary virtual set"},
	})
	return err
}

func (s *Store) PrimarySet(ctx context.Context, partition string) (string, error) {
	data, err := s.store.ReadFile(ctx, paths.SetPrimaryRecord(partition))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	var record primaryRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return "", err
	}
	return record.SetID, nil
}

func (s *Store) validateParent(ctx context.Context, id, parentID string) error {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return nil
	}
	if parentID == id {
		return ErrCycle
	}
	if _, err := s.Get(ctx, parentID); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrParentNotFound
		}
		return err
	}
	seen := map[string]bool{id: true}
	current := parentID
	for current != "" {
		if seen[current] {
			return ErrCycle
		}
		seen[current] = true
		parent, err := s.Get(ctx, current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		current = strings.TrimSpace(parent.ParentID)
	}
	return nil
}

func (s *Store) write(ctx context.Context, set *Set) error {
	payload, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	_, err = s.store.UpsertFiles(ctx, guardianapi.MutationBatch{
		Writes:  []guardianapi.PathWrite{{LogicalPath: paths.SetRecord(set.ID), Content: payload}},
		Context: guardianapi.MutationContext{PrincipalID: "guardiand", Reason: "put virtual set"},
	})
	return err
}

func (s *Set) Normalize() {
	if s == nil {
		return
	}
	s.APIVersion = "guardian/v1alpha1"
	s.Kind = "Set"
	s.ID = strings.TrimSpace(s.ID)
	s.Name = strings.TrimSpace(s.Name)
	s.Description = strings.TrimSpace(s.Description)
	s.ParentID = strings.TrimSpace(s.ParentID)
	s.Partitions = normalizeList(s.Partitions)
	if s.Labels == nil {
		s.Labels = map[string]string{}
	}
	for key, value := range s.Labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			delete(s.Labels, key)
			continue
		}
		s.Labels[key] = value
	}
}

func normalizeList(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

type primaryRecord struct {
	SetID string `json:"setId"`
}
