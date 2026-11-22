package quota

import (
	"os"
	"path/filepath"
	"testing"

	policy "github.com/radryc/monofs/pkg/authz/policy"
)

func TestCatalogIncludesBuiltinsInSizeOrder(t *testing.T) {
	profiles := Catalog(t.TempDir())
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
		if !profile.Builtin {
			t.Fatalf("profile %q should be builtin", profile.Name)
		}
	}
	want := policy.QuotaProfileNames()
	if len(names) != len(want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}

func TestCatalogReadsCustomProfiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "access"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := `version: 1
quota_profiles:
  team-small:
    query:
      requestsPerSecond: 12
`
	if err := os.WriteFile(filepath.Join(root, "access", "base.yaml"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	profile, ok := Find(root, "team-small")
	if !ok {
		t.Fatal("custom profile not found")
	}
	if profile.Builtin {
		t.Fatal("custom profile marked builtin")
	}
	if profile.Quota.Query == nil || profile.Quota.Query.RequestsPerSecond != 12 {
		t.Fatalf("quota = %#v", profile.Quota)
	}
}

func TestValidateProfileSelection(t *testing.T) {
	root := t.TempDir()
	if err := Validate(root, &policy.Quota{Profile: "medium"}); err != nil {
		t.Fatalf("builtin profile rejected: %v", err)
	}
	if err := Validate(root, &policy.Quota{Profile: "nope"}); err == nil {
		t.Fatal("unknown profile accepted")
	}
	if err := Validate(root, &policy.Quota{Profile: "medium", Query: &policy.QueryQuota{RequestsPerSecond: 1}}); err == nil {
		t.Fatal("inline limits accepted")
	}
	if err := Validate(root, &policy.Quota{Enforcement: "observe"}); err == nil {
		t.Fatal("missing profile accepted")
	}
}
