package assets

import (
	"fmt"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
)

type ImageBuildSpec struct {
	Repository      string            `json:"repository" yaml:"repository"`
	Registry        string            `json:"registry,omitempty" yaml:"registry,omitempty"`
	ObserveExisting bool              `json:"observeExisting,omitempty" yaml:"observeExisting,omitempty"`
	BuildContext    string            `json:"buildContext,omitempty" yaml:"buildContext,omitempty"`
	SourceImage     string            `json:"sourceImage,omitempty" yaml:"sourceImage,omitempty"`
	Dockerfile      string            `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Target          string            `json:"target,omitempty" yaml:"target,omitempty"`
	Platform        string            `json:"platform,omitempty" yaml:"platform,omitempty"`
	BuildArgs       map[string]string `json:"buildArgs,omitempty" yaml:"buildArgs,omitempty"`
	Insecure        *bool             `json:"insecure,omitempty" yaml:"insecure,omitempty"`
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
	hasBuildContext := strings.TrimSpace(typed.BuildContext) != ""
	hasSourceImage := strings.TrimSpace(typed.SourceImage) != ""

	if !hasBuildContext && !hasSourceImage {
		return fmt.Errorf("either buildContext or sourceImage must be specified")
	}

	if hasBuildContext {
		if strings.TrimSpace(typed.Dockerfile) == "" {
			return fmt.Errorf("property dockerfile is required when buildContext is set")
		}
	}
	return validateBuildArgs(typed)
}

func validateBuildArgs(typed *ImageBuildSpec) error {
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
