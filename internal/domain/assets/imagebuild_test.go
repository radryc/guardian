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

	bothSet := &ImageBuildSpec{
		Repository:  "demo-api",
		SourceDir:   "/partitions/demo/payloads/sources/api",
		ImageTar:    "/partitions/demo/payloads/images/demo-api.tar",
		SourceImage: "demo-api:latest",
	}
	if err := (imageBuildDefinition{}).Validate(bothSet, ValidationContext{}); err == nil {
		t.Fatal("Validate(bothSet) expected error (mutually exclusive)")
	}

	neitherSet := &ImageBuildSpec{
		Repository: "demo-api",
	}
	if err := (imageBuildDefinition{}).Validate(neitherSet, ValidationContext{}); err == nil {
		t.Fatal("Validate(neitherSet) expected error (must specify one)")
	}
}
