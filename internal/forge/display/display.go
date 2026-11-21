// Package display authors and lints repo-owned display-key aliases. It only
// touches .guardian/config.yaml; manifests always keep canonical keys.
package display

import (
	"fmt"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/forge/diagnostic"
	"github.com/radryc/guardian/internal/registry"
	"github.com/radryc/guardian/internal/repoconfig"
	displaypkg "github.com/radryc/monofs/pkg/display"
	"gopkg.in/yaml.v3"
)

// Bind sets one display-key alias on a repo-owned partition assignment.
func Bind(root, partition, canonical, alias, profile string) error {
	cfg, path, found, err := repoconfig.Load(root)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no .guardian/config.yaml under %s", root)
	}
	for i := range cfg.Repo.Bindings {
		binding := &cfg.Repo.Bindings[i]
		if binding.Partition != partition {
			continue
		}
		if binding.Display == nil {
			binding.Display = &registry.DisplayBinding{Keys: map[string]string{}}
		}
		if binding.Display.Keys == nil {
			binding.Display.Keys = map[string]string{}
		}
		if strings.TrimSpace(profile) != "" {
			binding.Display.Profile = strings.TrimSpace(profile)
		}
		binding.Display.Keys[canonical] = alias
		if err := validate(binding); err != nil {
			return err
		}
		data, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	return fmt.Errorf("partition %q is not bound to this repository", partition)
}

// Show returns the aliases for one assignment.
func Show(root, partition string) (map[string]string, error) {
	cfg, _, found, err := repoconfig.Load(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("no .guardian/config.yaml under %s", root)
	}
	for _, binding := range cfg.Repo.Bindings {
		if binding.Partition != partition {
			continue
		}
		out := map[string]string{}
		if binding.Display != nil {
			for key, value := range binding.Display.Keys {
				out[key] = value
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("partition %q is not bound to this repository", partition)
}

// Lint validates every repo-owned display binding.
func Lint(root string) (diagnostic.List, error) {
	cfg, _, found, err := repoconfig.Load(root)
	if err != nil {
		return nil, err
	}
	var out diagnostic.List
	if !found {
		return out, nil
	}
	for _, binding := range cfg.Repo.Bindings {
		if binding.Display == nil || len(binding.Display.Keys) == 0 {
			continue
		}
		profile := displaypkg.Profile{
			ID:   firstNonEmpty(binding.Display.Profile, "repo-binding"),
			Keys: binding.Display.Keys,
		}
		if err := profile.Validate(); err != nil {
			out.Add(diagnostic.Diagnostic{
				Severity:  diagnostic.SeverityError,
				Code:      "FGE8003",
				Message:   err.Error(),
				Partition: binding.Partition,
				Path:      ".guardian/config.yaml#repo.bindings[].display",
			})
		}
	}
	return out, nil
}

func validate(binding *registry.RepoBinding) error {
	keys := binding.Display.Keys
	profile := displaypkg.Profile{ID: firstNonEmpty(binding.Display.Profile, "repo-binding"), Keys: keys}
	return profile.Validate()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
