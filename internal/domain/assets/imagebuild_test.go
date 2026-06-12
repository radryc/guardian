package assets

import "testing"

func TestImageBuildValidate(t *testing.T) {
	valid := &ImageBuildSpec{
		Repository: "demo-api",
		Registry:   "registry.strata.local:5000",
		SourceDir:  "/partitions/demo/payloads/sources/api",
		Dockerfile: "Dockerfile",
		BuildArgs: map[string]string{
			"APP_ENV": "dev",
		},
	}
	if err := (imageBuildDefinition{}).Validate(valid, ValidationContext{}); err != nil {
		t.Fatalf("Validate(valid) error = %v", err)
	}

	invalid := &ImageBuildSpec{
		Repository: "demo-api",
		SourceDir:  "relative/path",
	}
	if err := (imageBuildDefinition{}).Validate(invalid, ValidationContext{}); err == nil {
		t.Fatal("Validate(invalid) expected error")
	}
}
