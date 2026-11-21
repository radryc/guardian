package assets

import "testing"

func TestComputeValidateSecretMounts(t *testing.T) {
	definition := computeDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{
		"tls-cert": "Secret",
		"tls-key":  "Secret",
	}}
	spec := &ComputeSpec{
		Image: "demo:v1",
		SecretMounts: []SecretMountSpec{
			{Secret: "tls-cert", Path: "/var/run/secrets/strata-tls", ReadOnly: true},
			{Secret: "tls-key", Path: "/etc/tls/tls.key", File: "tls.key", ReadOnly: true},
		},
	}
	if err := definition.Validate(spec, ctx); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestComputeValidateRejectsInvalidSecretMounts(t *testing.T) {
	definition := computeDefinition{}
	ctx := ValidationContext{AssetTypes: map[string]string{"tls-cert": "Secret"}}
	cases := []struct {
		name  string
		mount SecretMountSpec
	}{
		{name: "missing asset", mount: SecretMountSpec{Secret: "missing", Path: "/etc/tls"}},
		{name: "relative path", mount: SecretMountSpec{Secret: "tls-cert", Path: "etc/tls"}},
		{name: "file with slash", mount: SecretMountSpec{Secret: "tls-cert", Path: "/etc/tls", File: "dir/tls.key"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := &ComputeSpec{Image: "demo:v1", SecretMounts: []SecretMountSpec{tc.mount}}
			if err := definition.Validate(spec, ctx); err == nil {
				t.Fatalf("Validate() accepted invalid mount %+v", tc.mount)
			}
		})
	}
}
