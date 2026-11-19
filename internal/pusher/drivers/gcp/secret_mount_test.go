package gcpdriver

import (
	"testing"

	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/registry"
)

func TestComputeRequestIncludesSecretMounts(t *testing.T) {
	driver := &ComputeDriver{}
	in := registry.AssetInput{
		PartitionName: "demo",
		IntentName:    "stack",
		Asset:         taskdomain.AbstractAsset{Name: "app"},
		Assets: map[string]taskdomain.AbstractAsset{
			"tls-cert": {Type: "Secret", Name: "tls-cert", Properties: map[string]any{"value": "cert", "key": "tls.crt"}},
			"tls-ca":   {Type: "Secret", Name: "tls-ca", Properties: map[string]any{"value": "ca", "key": "ca.crt"}},
		},
		Outputs: map[string]string{"tls-cert.secret_id": "projects/demo/secrets/tls-cert"},
	}
	spec := &assetdefs.ComputeSpec{
		Image: "demo:v1",
		SecretMounts: []assetdefs.SecretMountSpec{
			{Secret: "tls-cert", Path: "/var/run/secrets/strata-tls/tls.crt", File: "tls.crt"},
			{Secret: "tls-ca", Path: "/var/run/secrets/strata-tls"},
		},
	}
	req := driver.request(in, spec)
	mounts, ok := req.Variables["secret_mounts"].([]map[string]any)
	if !ok || len(mounts) != 2 {
		t.Fatalf("secret_mounts = %#v", req.Variables["secret_mounts"])
	}
	if mounts[0]["secret"] != "projects/demo/secrets/tls-cert" ||
		mounts[0]["mount_path"] != "/var/run/secrets/strata-tls" ||
		mounts[0]["item_path"] != "tls.crt" {
		t.Fatalf("first mount = %#v", mounts[0])
	}
	if mounts[1]["secret"] != secretName("demo", "stack", "tls-ca") ||
		mounts[1]["mount_path"] != "/var/run/secrets/strata-tls" ||
		mounts[1]["item_path"] != "ca.crt" {
		t.Fatalf("second mount = %#v", mounts[1])
	}
}
