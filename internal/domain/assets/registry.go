package assets

import (
	"encoding/json"
	"fmt"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

type ValidationContext struct {
	AssetTypes map[string]string
	// RequireSecretRefs rejects inline Secret values. Repo-sourced bundles
	// already enforce this; the flag extends the guardrail to all sources.
	RequireSecretRefs bool
}

type Definition interface {
	Type() string
	NewSpec() any
	Validate(spec any, ctx ValidationContext) error
}

// Definitions is an isolated set of asset definitions. Production uses Default();
// tests that register extra definitions should build their own set instead of
// mutating process-wide state.
type Definitions struct {
	definitions map[string]Definition
}

func NewDefinitions(defs ...Definition) *Definitions {
	catalog := &Definitions{definitions: map[string]Definition{}}
	for _, def := range defs {
		catalog.Register(def)
	}
	return catalog
}

func (c *Definitions) Register(def Definition) {
	if def == nil {
		panic("assets.Register: nil Definition")
	}
	if c.definitions == nil {
		c.definitions = map[string]Definition{}
	}
	c.definitions[def.Type()] = def
}

func (c *Definitions) DefinitionFor(assetType string) (Definition, bool) {
	def, ok := c.definitions[assetType]
	return def, ok
}

func (c *Definitions) Decode(spec assetdomain.Spec) (any, Definition, error) {
	def, ok := c.DefinitionFor(spec.Type)
	if !ok {
		return nil, nil, fmt.Errorf("unsupported asset type %q", spec.Type)
	}
	target := def.NewSpec()
	props := spec.Properties
	if props == nil {
		props = map[string]any{}
	}
	content, err := json.Marshal(props)
	if err != nil {
		return nil, nil, fmt.Errorf("encode asset properties: %w", err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		return nil, nil, fmt.Errorf("decode %s properties: %w", spec.Type, err)
	}
	return target, def, nil
}

func (c *Definitions) Validate(spec assetdomain.Spec, ctx ValidationContext) error {
	typed, def, err := c.Decode(spec)
	if err != nil {
		return err
	}
	return def.Validate(typed, ctx)
}

var defaultCatalog = NewDefinitions()

// Default returns the process-wide definitions populated by the asset definition
// packages' init functions.
func Default() *Definitions { return defaultCatalog }

func Register(def Definition) { defaultCatalog.Register(def) }

func DefinitionFor(assetType string) (Definition, bool) {
	return defaultCatalog.DefinitionFor(assetType)
}

func Decode(spec assetdomain.Spec) (any, Definition, error) {
	return defaultCatalog.Decode(spec)
}

func Validate(spec assetdomain.Spec, ctx ValidationContext) error {
	return defaultCatalog.Validate(spec, ctx)
}
