package assets

import "testing"

func TestImageBuildValidate(t *testing.T) {
	validBuildContext := &ImageBuildSpec{
		Repository:   "demo-api",
		Registry:     "registry.strata.local:5000",
		BuildContext: "/home/user/src/demo",
		Dockerfile:   "Dockerfile",
		BuildArgs:    map[string]string{"GO_VERSION": "1.22"},
	}
	if err := (imageBuildDefinition{}).Validate(validBuildContext, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validBuildContext) error = %v", err)
	}

	validSourceImage := &ImageBuildSpec{
		Repository:  "demo-api",
		Registry:    "registry.strata.local:5000",
		SourceImage: "registry.strata.local:5000/demo-api:sha256-abc12345",
	}
	if err := (imageBuildDefinition{}).Validate(validSourceImage, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validSourceImage) error = %v", err)
	}

	buildContextMissingDockerfile := &ImageBuildSpec{
		Repository:   "demo-api",
		BuildContext: "/home/user/src/demo",
	}
	if err := (imageBuildDefinition{}).Validate(buildContextMissingDockerfile, ValidationContext{}); err == nil {
		t.Fatal("Validate(buildContextMissingDockerfile) expected error (dockerfile required)")
	}

	neitherSet := &ImageBuildSpec{
		Repository: "demo-api",
	}
	if err := (imageBuildDefinition{}).Validate(neitherSet, ValidationContext{}); err == nil {
		t.Fatal("Validate(neitherSet) expected error (must specify buildContext or sourceImage)")
	}

	buildArgsEmptyKey := &ImageBuildSpec{
		Repository:   "demo-api",
		BuildContext: "/home/user/src/demo",
		Dockerfile:   "Dockerfile",
		BuildArgs:    map[string]string{"": "value"},
	}
	if err := (imageBuildDefinition{}).Validate(buildArgsEmptyKey, ValidationContext{}); err == nil {
		t.Fatal("Validate(buildArgsEmptyKey) expected error (empty key)")
	}

	buildArgsEmptyValue := &ImageBuildSpec{
		Repository:   "demo-api",
		BuildContext: "/home/user/src/demo",
		Dockerfile:   "Dockerfile",
		BuildArgs:    map[string]string{"KEY": ""},
	}
	if err := (imageBuildDefinition{}).Validate(buildArgsEmptyValue, ValidationContext{}); err == nil {
		t.Fatal("Validate(buildArgsEmptyValue) expected error (empty value)")
	}
}
