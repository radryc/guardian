package assets

import (
	"fmt"
	"regexp"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

var buildTagPattern = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type ImageBuildSpec struct {
	Repository      string `json:"repository" yaml:"repository"`
	Registry        string `json:"registry,omitempty" yaml:"registry,omitempty"`
	ObserveExisting bool   `json:"observeExisting,omitempty" yaml:"observeExisting,omitempty"`
	BuildContext    string `json:"buildContext,omitempty" yaml:"buildContext,omitempty"`
	SourceImage     string `json:"sourceImage,omitempty" yaml:"sourceImage,omitempty"`
	// BuildSource is a MonoFS logical path for the build context, for example
	// /.repos/github.com/acme/app. The k8s pusher builds the image from that
	// source revision and pushes it before dependent Computes are applied.
	BuildSource string `json:"buildSource,omitempty" yaml:"buildSource,omitempty"`
	// BuildTag overrides the image tag derived from the source commit.
	BuildTag   string            `json:"buildTag,omitempty" yaml:"buildTag,omitempty"`
	Dockerfile string            `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	Target     string            `json:"target,omitempty" yaml:"target,omitempty"`
	Platform   string            `json:"platform,omitempty" yaml:"platform,omitempty"`
	BuildArgs  map[string]string `json:"buildArgs,omitempty" yaml:"buildArgs,omitempty"`
	Insecure   *bool             `json:"insecure,omitempty" yaml:"insecure,omitempty"`
	// Instrumentation records whether the image is built with zero-code
	// instrumentation. The Dockerfile performs the actual otelc wrapping.
	Instrumentation string `json:"instrumentation,omitempty" yaml:"instrumentation,omitempty"`
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
	hasBuildSource := strings.TrimSpace(typed.BuildSource) != ""

	if hasBuildSource {
		if hasBuildContext || hasSourceImage {
			return fmt.Errorf("buildSource is mutually exclusive with buildContext and sourceImage")
		}
		source := strings.TrimSpace(typed.BuildSource)
		if !strings.HasPrefix(source, "/.repos/") && !strings.HasPrefix(source, ".repos/") {
			return fmt.Errorf("property buildSource must be a MonoFS /.repos/ path")
		}
		if strings.Contains(source, "..") {
			return fmt.Errorf("property buildSource must not contain ..")
		}
		if strings.TrimSpace(typed.Dockerfile) == "" {
			return fmt.Errorf("property dockerfile is required when buildSource is set")
		}
		if tag := strings.TrimSpace(typed.BuildTag); tag != "" {
			if !buildTagPattern.MatchString(tag) {
				return fmt.Errorf("property buildTag %q is not a valid image tag", tag)
			}
		}
	} else {
		if !hasBuildContext && !hasSourceImage {
			return fmt.Errorf("either buildContext or sourceImage must be specified")
		}
		if hasBuildContext && strings.TrimSpace(typed.Dockerfile) == "" {
			return fmt.Errorf("property dockerfile is required when buildContext is set")
		}
		if strings.TrimSpace(typed.BuildTag) != "" {
			return fmt.Errorf("property buildTag requires buildSource")
		}
	}
	switch typed.Instrumentation {
	case "", "none", "otelc":
	default:
		return fmt.Errorf("property instrumentation must be one of none, otelc")
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
