package assets

import (
	"fmt"
	"sort"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ImageBuildSpec struct {
	Repository  string            `json:"repository" yaml:"repository"`
	Registry    string            `json:"registry,omitempty" yaml:"registry,omitempty"`
	SourceDir   string            `json:"sourceDir,omitempty" yaml:"sourceDir,omitempty"`
	ImageTar    string            `json:"imageTar,omitempty" yaml:"imageTar,omitempty"`
	SourceImage string            `json:"sourceImage,omitempty" yaml:"sourceImage,omitempty"`
	Dockerfile  string            `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Target      string            `json:"target,omitempty" yaml:"target,omitempty"`
	Platform    string            `json:"platform,omitempty" yaml:"platform,omitempty"`
	BuildArgs   map[string]string `json:"buildArgs,omitempty" yaml:"buildArgs,omitempty"`
	Insecure    *bool             `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	StampOnly   bool              `json:"stampOnly,omitempty" yaml:"stampOnly,omitempty"`
}

type imageBuildDefinition struct{}

func init() {
	Register(imageBuildDefinition{})
}

func (imageBuildDefinition) Type() string { return assetdomain.TypeImageBuild }

func (imageBuildDefinition) NewSpec() any { return &ImageBuildSpec{} }

func (imageBuildDefinition) Validate(spec any, _ ValidationContext) error {
	typed, ok := spec.(*ImageBuildSpec)
	if !ok {
		return fmt.Errorf("internal image build spec type mismatch")
	}
	if err := requireString(typed.Repository, "repository"); err != nil {
		return err
	}
	hasSourceDir := strings.TrimSpace(typed.SourceDir) != ""
	hasImageTar := strings.TrimSpace(typed.ImageTar) != ""
	if !hasSourceDir && !hasImageTar {
		return fmt.Errorf("either sourceDir or imageTar must be specified")
	}
	if hasSourceDir && hasImageTar {
		return fmt.Errorf("sourceDir and imageTar are mutually exclusive")
	}
	if hasImageTar {
		if !strings.HasPrefix(strings.TrimSpace(typed.ImageTar), "/") {
			return fmt.Errorf("property imageTar must be an absolute logical path")
		}
		if strings.TrimSpace(typed.SourceImage) == "" {
			return fmt.Errorf("property sourceImage is required when imageTar is set")
		}
		return nil
	}
	if !strings.HasPrefix(strings.TrimSpace(typed.SourceDir), "/") {
		return fmt.Errorf("property sourceDir must be an absolute logical path")
	}
	if strings.TrimSpace(typed.Dockerfile) != "" {
		dockerfile := strings.TrimSpace(typed.Dockerfile)
		if strings.HasPrefix(dockerfile, "/") {
			return fmt.Errorf("property dockerfile must be relative to sourceDir")
		}
	}
	for key, value := range typed.BuildArgs {
		if strings.TrimSpace(key) == "" {
			return fmt.Errorf("property buildArgs must not contain empty keys")
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("property buildArgs[%q] must not be empty", key)
		}
	}
	return nil
}

func NormalizeBuildArgs(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	keys := make([]string, 0, len(in))
	for key := range in {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]string, len(keys))
	for _, key := range keys {
		trimmedKey := strings.TrimSpace(key)
		if trimmedKey == "" {
			continue
		}
		value := strings.TrimSpace(in[key])
		if value == "" {
			continue
		}
		out[trimmedKey] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
