package cdkgen

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Catalog is the declarative template registry: which CDK TypeScript template
// implements which asset type, which variants exist, and which properties are
// required or unsupported. It replaces the per-type Go switches that used to
// live in generate.go.
type Catalog struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Version    string          `yaml:"version" json:"version"`
	Templates  []TemplateEntry `yaml:"templates" json:"templates"`
}

// TemplateEntry describes one template module in the catalog.
type TemplateEntry struct {
	ID         string         `yaml:"id" json:"id"`
	AssetTypes []string       `yaml:"assetTypes" json:"assetTypes"`
	Export     string         `yaml:"export" json:"export"`
	Module     string         `yaml:"module" json:"module"`
	Version    int            `yaml:"version" json:"version"`
	Implicit   bool           `yaml:"implicit" json:"implicit"`
	Fallback   bool           `yaml:"fallback" json:"fallback"`
	Priority   int            `yaml:"priority" json:"priority"`
	Requires   []string       `yaml:"requires" json:"requires"`
	Match      []MatchRule    `yaml:"match" json:"match"`
	Params     TemplateParams `yaml:"params" json:"params"`
	Outputs    []string       `yaml:"outputs" json:"outputs"`
	// OutputPatterns are output keys with a %d placeholder expanded once per
	// entry of the asset's `listeners` property (for example `tg_%d_arn`).
	OutputPatterns []string `yaml:"outputPatterns" json:"outputPatterns"`
}

// MatchRule is a declarative variant selection rule. Paths support nested
// fields and `[*]` array wildcards (for example `listeners[*].protocol`).
type MatchRule struct {
	Path  string `yaml:"path" json:"path"`
	Op    string `yaml:"op" json:"op"`
	Value string `yaml:"value" json:"value"`
}

// TemplateParams declares property requirements enforced by the composer.
type TemplateParams struct {
	Required    []string `yaml:"required" json:"required"`
	Unsupported []string `yaml:"unsupported" json:"unsupported"`
}

// Dir returns the template directory name derived from the module path.
func (e *TemplateEntry) Dir() string {
	if idx := strings.Index(e.Module, "/"); idx >= 0 {
		return e.Module[:idx]
	}
	return e.Module
}

// SupportsAssetType reports whether the catalog has a template for assetType.
func (c *Catalog) SupportsAssetType(assetType string) bool {
	for i := range c.Templates {
		if c.Templates[i].Implicit {
			continue
		}
		if c.Templates[i].supportsType(assetType) {
			return true
		}
	}
	return false
}

func (e *TemplateEntry) supportsType(assetType string) bool {
	for _, candidate := range e.AssetTypes {
		if candidate == assetType {
			return true
		}
	}
	return false
}

// Entry returns the template entry with the given id.
func (c *Catalog) Entry(id string) (*TemplateEntry, bool) {
	for i := range c.Templates {
		if c.Templates[i].ID == id {
			return &c.Templates[i], true
		}
	}
	return nil, false
}

// Select picks the template for an asset: highest priority first, first entry
// whose match rules all pass. Entries without match rules are defaults.
func (c *Catalog) Select(assetType string, properties map[string]any) (*TemplateEntry, error) {
	candidates := make([]*TemplateEntry, 0, len(c.Templates))
	for i := range c.Templates {
		entry := &c.Templates[i]
		if entry.Implicit || !entry.supportsType(assetType) {
			continue
		}
		candidates = append(candidates, entry)
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no template registered for asset type %s", assetType)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority > candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	for _, entry := range candidates {
		if entry.matches(properties) {
			return entry, nil
		}
	}
	return nil, fmt.Errorf("no template for asset type %s matched the asset properties", assetType)
}

func (e *TemplateEntry) matches(properties map[string]any) bool {
	for _, rule := range e.Match {
		if !matchRule(properties, rule) {
			return false
		}
	}
	return true
}

func matchRule(properties map[string]any, rule MatchRule) bool {
	values := resolvePath(properties, rule.Path)
	switch strings.ToLower(strings.TrimSpace(rule.Op)) {
	case "equals":
		want := strings.ToLower(strings.TrimSpace(rule.Value))
		for _, value := range values {
			if strings.ToLower(strings.TrimSpace(toString(value))) == want {
				return true
			}
		}
		return false
	case "in":
		for _, candidate := range strings.Split(rule.Value, ",") {
			want := strings.ToLower(strings.TrimSpace(candidate))
			for _, value := range values {
				if strings.ToLower(strings.TrimSpace(toString(value))) == want {
					return true
				}
			}
		}
		return false
	case "contains":
		want := strings.ToLower(strings.TrimSpace(rule.Value))
		for _, value := range values {
			if strings.Contains(strings.ToLower(toString(value)), want) {
				return true
			}
		}
		return false
	case "exists":
		for _, value := range values {
			if value != nil {
				return true
			}
		}
		return false
	case "notexists":
		for _, value := range values {
			if value != nil {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// resolvePath walks a dotted path with optional `[*]` array wildcards and
// returns every value it finds.
func resolvePath(properties map[string]any, path string) []any {
	segments := strings.Split(strings.TrimSpace(path), ".")
	current := []any{properties}
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		wildcard := strings.HasSuffix(segment, "[*]")
		if wildcard {
			segment = strings.TrimSuffix(segment, "[*]")
		}
		next := make([]any, 0, len(current))
		for _, value := range current {
			object, ok := value.(map[string]any)
			if !ok {
				continue
			}
			child, ok := object[segment]
			if !ok {
				continue
			}
			if !wildcard {
				next = append(next, child)
				continue
			}
			items, ok := child.([]any)
			if !ok {
				continue
			}
			next = append(next, items...)
		}
		current = next
		if len(current) == 0 {
			return nil
		}
	}
	return current
}

// OutputsFor returns the static outputs plus expanded output patterns.
func (e *TemplateEntry) OutputsFor(properties map[string]any) []string {
	outputs := append([]string(nil), e.Outputs...)
	for _, pattern := range e.OutputPatterns {
		count := 0
		if listeners, ok := properties["listeners"].([]any); ok {
			count = len(listeners)
		}
		for i := 0; i < count; i++ {
			outputs = append(outputs, strings.ReplaceAll(pattern, "%d", fmt.Sprint(i)))
		}
	}
	return outputs
}

// ValidateProps enforces the catalog's required/unsupported declarations.
func (e *TemplateEntry) ValidateProps(assetName string, properties map[string]any) error {
	for _, key := range e.Params.Required {
		if !isPresent(properties[key]) {
			return fmt.Errorf("asset %s: property %s is required", assetName, key)
		}
	}
	for _, key := range e.Params.Unsupported {
		if value, ok := properties[key]; ok && !isEmptyValue(value) {
			return fmt.Errorf("asset %s: property %s is not supported by generated CDK stacks", assetName, key)
		}
	}
	return nil
}

func isPresent(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	case bool:
		return typed
	}
	return true
}

func isEmptyValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return true
	case bool:
		return !typed
	case string:
		return strings.TrimSpace(typed) == ""
	case []any:
		return len(typed) == 0
	case map[string]any:
		return len(typed) == 0
	}
	return false
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

// Validate checks catalog integrity (unique ids, module/export present).
func (c *Catalog) Validate() error {
	seen := map[string]bool{}
	for i := range c.Templates {
		entry := &c.Templates[i]
		if entry.ID == "" {
			return fmt.Errorf("catalog template %d: id is required", i)
		}
		if seen[entry.ID] {
			return fmt.Errorf("catalog template %s: duplicate id", entry.ID)
		}
		seen[entry.ID] = true
		if entry.Module == "" || entry.Export == "" {
			return fmt.Errorf("catalog template %s: module and export are required", entry.ID)
		}
		if len(entry.AssetTypes) == 0 && !entry.Implicit {
			return fmt.Errorf("catalog template %s: assetTypes is required unless implicit", entry.ID)
		}
		for _, rule := range entry.Match {
			if rule.Path == "" {
				return fmt.Errorf("catalog template %s: match rule path is required", entry.ID)
			}
			switch strings.ToLower(rule.Op) {
			case "equals", "in", "contains", "exists", "notexists":
			default:
				return fmt.Errorf("catalog template %s: unsupported match op %q", entry.ID, rule.Op)
			}
		}
	}
	return nil
}

// ParseCatalog decodes a catalog manifest.
func ParseCatalog(raw []byte) (*Catalog, error) {
	var catalog Catalog
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return nil, fmt.Errorf("parse template catalog: %w", err)
	}
	if err := catalog.Validate(); err != nil {
		return nil, err
	}
	return &catalog, nil
}

// loadCatalogFromFS reads catalog.yaml from the root of fsys.
func loadCatalogFromFS(fsys fs.FS) (*Catalog, error) {
	raw, err := fs.ReadFile(fsys, "catalog.yaml")
	if err != nil {
		return nil, fmt.Errorf("read template catalog: %w", err)
	}
	return ParseCatalog(raw)
}

var defaultCatalog = mustEmbeddedCatalog()

func mustEmbeddedCatalog() *Catalog {
	catalog, err := loadCatalogFromFS(embeddedTemplates())
	if err != nil {
		panic(err)
	}
	return catalog
}
