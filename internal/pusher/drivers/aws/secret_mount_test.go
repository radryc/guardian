package awsdriver

import (
	"testing"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func TestResolveComputeSecretsIncludesSecretMounts(t *testing.T) {
	arn := "arn:aws:secretsmanager:eu-west-1:123456789012:secret:strata/tls-AbCdEf"
	in := registry.AssetInput{
		Assets: map[string]taskdomain.AbstractAsset{
			"tls-cert": {Type: "Secret", Name: "tls-cert", Properties: map[string]any{"value": "cert", "key": "tls.crt"}},
			"tls-key":  {Type: "Secret", Name: "tls-key", Properties: map[string]any{"value": "key", "key": "tls.key"}},
			"tls-ca":   {Type: "Secret", Name: "tls-ca", Properties: map[string]any{"value": "ca", "key": "ca.crt"}},
		},
		Outputs: map[string]string{"tls-cert.arn": arn},
	}
	spec := &assetdefs.ComputeSpec{
		SecretMounts: []assetdefs.SecretMountSpec{
			{Secret: "tls-cert", Path: "/var/run/secrets/strata-tls"},
			{Secret: "tls-key", Path: "/var/run/secrets/strata-tls/tls.key", File: "tls.key"},
			{Secret: "tls-ca", Path: "/var/run/secrets/strata-tls/ca.crt"},
		},
	}
	got := resolveComputeSecrets(in, spec)
	if got["TLS_CRT"] != arn {
		t.Fatalf("TLS_CRT = %q, want %q", got["TLS_CRT"], arn)
	}
	if got["TLS_KEY"] != awsSecretName(in, "tls-key") {
		t.Fatalf("TLS_KEY = %q", got["TLS_KEY"])
	}
	if got["CA_CRT"] != awsSecretName(in, "tls-ca") {
		t.Fatalf("CA_CRT = %q", got["CA_CRT"])
	}
}

func TestSecretMountEnvNameSanitizes(t *testing.T) {
	cases := map[string]string{
		"/etc/tls/tls.crt":      "TLS_CRT",
		"tls-key.pem":           "TLS_KEY_PEM",
		"weird key.one":         "WEIRD_KEY_ONE",
		"/var/run/no-extension": "NO_EXTENSION",
	}
	for input, want := range cases {
		got := secretMountEnvName(input)
		if got != want {
			t.Fatalf("secretMountEnvName(%q) = %q, want %q", input, got, want)
		}
	}
}
