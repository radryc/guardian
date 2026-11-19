package bootstrap

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
)

type mtlsTarget struct {
	Namespace  string
	Deployment string
	Container  string
	Secret     string
}

// InjectMTLS patches the bootstrap deployments so they mount their generated
// certificate Secret and run with STRATA_TLS_MODE=optional. Optional mode uses
// mTLS for gRPC and keeps HTTP plaintext, so local UIs and port-forwards keep
// working. The Secret volume is optional so a missing certificate never blocks
// a pod from starting (it then serves plaintext).
func InjectMTLS(ctx context.Context, cfg *Config, dryRun bool) error {
	if cfg == nil {
		return nil
	}
	targets := []mtlsTarget{
		{cfg.Storage.Namespace, "router-a", "router", "strata-tls-monofs-router"},
		{cfg.Storage.Namespace, "router-b", "router", "strata-tls-monofs-router"},
		{cfg.Storage.Namespace, "search-index", "search", "strata-tls-monofs-search"},
		{cfg.Storage.Namespace, "fetcher-a", "fetcher", "strata-tls-monofs-fetcher"},
		{cfg.Storage.Namespace, "fetcher-b", "fetcher", "strata-tls-monofs-fetcher"},
		{cfg.Storage.Namespace, "monofs-registry", "registry", "strata-tls-monofs-registry"},
		{cfg.Guardian.Namespace, "guardiand", "guardiand", "strata-tls-guardiand"},
		{cfg.Guardian.Namespace, "guardian-pusher-k8s", "pusher", "strata-tls-guardian-pusher-k8s"},
		{cfg.Guardian.Namespace, "guardian-pusher-aws", "pusher", "strata-tls-guardian-pusher-aws"},
		{cfg.Guardian.Namespace, "guardian-pusher-gcp", "pusher", "strata-tls-guardian-pusher-gcp"},
	}
	for _, node := range []string{"node-a", "node-b", "node-c", "node-d", "node-e"} {
		targets = append(targets, mtlsTarget{cfg.Storage.Namespace, node, "node", "strata-tls-monofs-server"})
	}
	for _, target := range targets {
		if err := injectMTLSTarget(ctx, target, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func injectMTLSTarget(ctx context.Context, target mtlsTarget, dryRun bool) error {
	if target.Namespace == "" || target.Deployment == "" || target.Container == "" {
		return nil
	}
	if !dryRun {
		check := exec.CommandContext(ctx, "kubectl", "-n", target.Namespace, "get", "deployment", target.Deployment)
		check.Stdout = io.Discard
		check.Stderr = io.Discard
		if err := check.Run(); err != nil {
			// Not deployed (for example a disabled pusher) — nothing to patch.
			return nil
		}
	}
	patch := map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"volumes": []map[string]any{{
						"name": "strata-tls",
						"secret": map[string]any{
							"secretName": target.Secret,
							"optional":   true,
						},
					}},
					"containers": []map[string]any{{
						"name": target.Container,
						"env": []map[string]any{{
							"name":  "STRATA_TLS_MODE",
							"value": "optional",
						}},
						"volumeMounts": []map[string]any{{
							"name":      "strata-tls",
							"mountPath": "/var/run/secrets/strata-tls",
							"readOnly":  true,
						}},
					}},
				},
			},
		},
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return fmt.Errorf("mtls: encode patch for %s: %w", target.Deployment, err)
	}
	if dryRun {
		fmt.Printf("  (dry-run) would patch deployment %s/%s for mTLS\n", target.Namespace, target.Deployment)
		return nil
	}
	cmd := exec.CommandContext(ctx, "kubectl", "-n", target.Namespace, "patch", "deployment", target.Deployment,
		"--type=strategic", "-p", string(raw))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mtls: patch %s/%s: %w: %s", target.Namespace, target.Deployment, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}
