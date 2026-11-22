// Package quota exposes the predefined quota profiles Forge may assign to a
// partition. Profiles come from the built-in size classes and from
// quota_profiles declared in repository access policy files.
package quota

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	policy "github.com/radryc/monofs/pkg/authz/policy"
)

// Profile is one selectable quota profile.
type Profile struct {
	Name    string       `json:"name"`
	Builtin bool         `json:"builtin"`
	Quota   policy.Quota `json:"quota"`
}

// AccessFiles lists the repository files Forge reads for custom profiles.
// Missing files are ignored.
func AccessFiles(root string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	files := []string{
		filepath.Join(root, "access", "base.yaml"),
		filepath.Join(root, ".forge", "quota-profiles.yaml"),
	}
	if entries, err := filepath.Glob(filepath.Join(root, "access", "envs", "*.yaml")); err == nil {
		sort.Strings(entries)
		files = append(files, entries...)
	}
	if entries, err := filepath.Glob(filepath.Join(root, "access", "envs", "*.yml")); err == nil {
		sort.Strings(entries)
		files = append(files, entries...)
	}
	return files
}

// Catalog returns built-in profiles in size order followed by custom profiles.
// A custom profile with the same name overrides the built-in definition.
func Catalog(root string) []Profile {
	custom := map[string]policy.Quota{}
	for _, path := range AccessFiles(root) {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		file, err := policy.ParseFile(data)
		if err != nil {
			continue
		}
		for name, profile := range file.QuotaProfiles {
			custom[strings.TrimSpace(name)] = profile.Clone()
		}
	}

	builtins := policy.BuiltinQuotaProfiles()
	out := make([]Profile, 0, len(builtins)+len(custom))
	seen := make(map[string]bool, len(builtins)+len(custom))
	for _, name := range policy.QuotaProfileNames() {
		profile, builtin := builtins[name], true
		if override, ok := custom[name]; ok {
			profile, builtin = override, false
			delete(custom, name)
		}
		out = append(out, Profile{Name: name, Builtin: builtin, Quota: profile.Clone()})
		seen[name] = true
	}
	extra := make([]string, 0, len(custom))
	for name := range custom {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	for _, name := range extra {
		out = append(out, Profile{Name: name, Builtin: false, Quota: custom[name].Clone()})
	}
	return out
}

// Names returns the selectable profile names in catalog order.
func Names(root string) []string {
	profiles := Catalog(root)
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	return names
}

// Find resolves one profile by name.
func Find(root, name string) (Profile, bool) {
	name = strings.TrimSpace(name)
	for _, profile := range Catalog(root) {
		if profile.Name == name {
			return profile, true
		}
	}
	return Profile{}, false
}

// Validate enforces that a partition quota selects a predefined profile and
// does not smuggle inline limits past the catalog.
func Validate(root string, q *policy.Quota) error {
	if q == nil {
		return nil
	}
	name := strings.TrimSpace(q.Profile)
	if name == "" {
		return fmt.Errorf("quota profile is required; choose one of: %s", strings.Join(Names(root), ", "))
	}
	if _, ok := Find(root, name); !ok {
		return fmt.Errorf("unknown quota profile %q; choose one of: %s", name, strings.Join(Names(root), ", "))
	}
	if q.Ingest != nil || q.Query != nil || q.Rules != nil || q.Storage != nil {
		return fmt.Errorf("inline quota limits are not allowed; select a predefined profile")
	}
	return nil
}
