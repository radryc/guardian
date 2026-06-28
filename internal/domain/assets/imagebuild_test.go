package assets

import "testing"

func TestImageBuildValidate(t *testing.T) {
	validSource := &ImageBuildSpec{
		Repository: "demo-api",
		Registry:   "registry.strata.local:5000",
		SourceDir:  "/partitions/demo/payloads/sources/api",
		Dockerfile: "Dockerfile",
		BuildArgs: map[string]string{
			"APP_ENV": "dev",
		},
	}
	if err := (imageBuildDefinition{}).Validate(validSource, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validSource) error = %v", err)
	}

	validTar := &ImageBuildSpec{
		Repository:  "demo-api",
		Registry:    "registry.strata.local:5000",
		ImageTar:    "/partitions/demo/payloads/images/demo-api.tar",
		SourceImage: "demo-api:latest",
	}
	if err := (imageBuildDefinition{}).Validate(validTar, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validTar) error = %v", err)
	}

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

	validStampOnly := &ImageBuildSpec{
		Repository:  "demo-api",
		Registry:    "registry.strata.local:5000",
		StampOnly:   true,
		SourceImage: "registry.strata.local:5000/demo-api:sha256-abc12345",
	}
	if err := (imageBuildDefinition{}).Validate(validStampOnly, ValidationContext{}); err != nil {
		t.Fatalf("Validate(validStampOnly) error = %v", err)
	}

	buildContextMissingDockerfile := &ImageBuildSpec{
		Repository:   "demo-api",
		BuildContext: "/home/user/src/demo",
	}
	if err := (imageBuildDefinition{}).Validate(buildContextMissingDockerfile, ValidationContext{}); err == nil {
		t.Fatal("Validate(buildContextMissingDockerfile) expected error (dockerfile required)")
	}

	stampOnlyNoSourceImage := &ImageBuildSpec{
		Repository: "demo-api",
		StampOnly:  true,
	}
	if err := (imageBuildDefinition{}).Validate(stampOnlyNoSourceImage, ValidationContext{}); err == nil {
		t.Fatal("Validate(stampOnlyNoSourceImage) expected error (must specify a mode)")
	}

	invalidSource := &ImageBuildSpec{
		Repository: "demo-api",
		SourceDir:  "relative/path",
	}
	if err := (imageBuildDefinition{}).Validate(invalidSource, ValidationContext{}); err == nil {
		t.Fatal("Validate(invalidSource) expected error")
	}

	missingTarImage := &ImageBuildSpec{
		Repository: "demo-api",
		ImageTar:   "/partitions/demo/payloads/images/demo-api.tar",
	}
	if err := (imageBuildDefinition{}).Validate(missingTarImage, ValidationContext{}); err == nil {
		t.Fatal("Validate(missingTarImage) expected error (sourceImage required)")
	}

	sourceDirAndBuildContext := &ImageBuildSpec{
		Repository:   "demo-api",
		SourceDir:    "/partitions/demo/payloads/sources/api",
		BuildContext: "/home/user/src/demo",
		Dockerfile:   "Dockerfile",
	}
	if err := (imageBuildDefinition{}).Validate(sourceDirAndBuildContext, ValidationContext{}); err == nil {
		t.Fatal("Validate(sourceDirAndBuildContext) expected error (mutually exclusive)")
	}

	tarAndBuildContext := &ImageBuildSpec{
		Repository:   "demo-api",
		ImageTar:     "/partitions/demo/payloads/images/demo-api.tar",
		BuildContext: "/home/user/src/demo",
		SourceImage:  "demo-api:latest",
		Dockerfile:   "Dockerfile",
	}
	if err := (imageBuildDefinition{}).Validate(tarAndBuildContext, ValidationContext{}); err == nil {
		t.Fatal("Validate(tarAndBuildContext) expected error (mutually exclusive)")
	}

	neitherSet := &ImageBuildSpec{
		Repository: "demo-api",
	}
	if err := (imageBuildDefinition{}).Validate(neitherSet, ValidationContext{}); err == nil {
		t.Fatal("Validate(neitherSet) expected error (must specify one)")
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
