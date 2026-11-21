package bootstrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/radryc/tlsx/pki"
)

// MTLSService is the local service identity list used by the dev bootstrap.
// DNS names cover every address the local stack dials (deployment names,
// services, LB alias and 127.0.0.1 for port-forwards).
func MTLSServices() []pki.Service {
	sharedIPs := []string{"127.0.0.1", "::1"}
	routers := []string{
		"router-a", "router-b",
		"router-a.monofs.svc", "router-b.monofs.svc",
		"router-a.monofs.svc.cluster.local", "router-b.monofs.svc.cluster.local",
		"monofs-router", "monofs-router.monofs.svc", "monofs-router.monofs.svc.cluster.local",
		"monofs-external", "monofs-external.lb-edge.svc", "monofs-external.lb-edge.svc.cluster.local",
	}
	nodes := []string{
		"node-a", "node-b", "node-c", "node-d", "node-e",
		"node-a.monofs.svc", "node-b.monofs.svc", "node-c.monofs.svc", "node-d.monofs.svc", "node-e.monofs.svc",
		"node-a.monofs.svc.cluster.local", "node-b.monofs.svc.cluster.local", "node-c.monofs.svc.cluster.local",
		"node-d.monofs.svc.cluster.local", "node-e.monofs.svc.cluster.local",
	}
	return []pki.Service{
		{Name: "monofs-router", Namespace: "monofs", DNSNames: routers, IPs: sharedIPs},
		{Name: "monofs-server", Namespace: "monofs", DNSNames: nodes, IPs: sharedIPs},
		{Name: "monofs-search", Namespace: "monofs", DNSNames: []string{"search-index", "search-index.monofs.svc", "search-index.monofs.svc.cluster.local"}, IPs: sharedIPs},
		{Name: "monofs-fetcher", Namespace: "monofs", DNSNames: []string{"fetcher-a", "fetcher-b", "fetcher-a.monofs.svc", "fetcher-b.monofs.svc"}, IPs: sharedIPs},
		{Name: "monofs-registry", Namespace: "monofs", IPs: sharedIPs},
		{Name: "kvs", Namespace: "monofs", DNSNames: nodes, IPs: sharedIPs},
		{Name: "guardiand", Namespace: "guardian", DNSNames: []string{"guardian-ui", "guardian-ui.guardian.svc", "guardian-ui.guardian.svc.cluster.local"}, IPs: sharedIPs},
		{Name: "guardian-pusher-k8s", Namespace: "guardian", IPs: sharedIPs},
		{Name: "guardian-pusher-aws", Namespace: "guardian", IPs: sharedIPs},
		{Name: "guardian-pusher-gcp", Namespace: "guardian", IPs: sharedIPs},
		{Name: "guardian-pusher-docker", Namespace: "guardian", IPs: sharedIPs},
		{Name: "doctor-query", Namespace: "doctor", IPs: sharedIPs},
		{Name: "doctor-ingest", Namespace: "doctor", IPs: sharedIPs},
		{Name: "doctor-rules", Namespace: "doctor", IPs: sharedIPs},
		{Name: "doctor-sentinel", Namespace: "doctor", IPs: sharedIPs},
		{Name: "lb-registry", Namespace: "lb-edge", IPs: sharedIPs},
		{Name: "lb-edge", Namespace: "lb-edge", IPs: sharedIPs},
		{Name: "agent", Namespace: "agent", IPs: sharedIPs},
	}
}

// MTLSDir returns the host certificate directory.
func MTLSDir() string {
	if value := strings.TrimSpace(os.Getenv("STRATA_MTLS_DIR")); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "strata-mtls")
	}
	return filepath.Join(home, ".strata", "mtls")
}

// EnsureMTLS generates the local CA and leaf certificates, installs one
// Kubernetes Secret per service, and writes an env file for host-side tools
// (guardianctl/guardianctl).
//
// rotateLeaves reissues leaf certificates under the existing CA. newCA creates
// a fresh CA (rolling all leaves).
func EnsureMTLS(ctx context.Context, dir string, rotateLeaves, newCA, dryRun bool) error {
	if strings.TrimSpace(dir) == "" {
		dir = MTLSDir()
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mtls: create %s: %w", dir, err)
	}
	reuseCA := !newCA
	if reuseCA {
		if _, err := os.Stat(filepath.Join(dir, "ca.crt")); err != nil {
			reuseCA = false
		}
	}
	bundle, err := pki.Generate(pki.Options{
		Dir:         dir,
		TrustDomain: "strata.local",
		Validity:    30 * 24 * time.Hour,
		CAValidity:  365 * 24 * time.Hour,
		Services:    MTLSServices(),
		ReuseCA:     reuseCA,
		Force:       rotateLeaves || newCA,
	})
	if err != nil {
		return fmt.Errorf("mtls: generate certificates: %w", err)
	}
	// The host-side Docker pusher runs as the image's non-root guardian user
	// with the host group added. Make just its cert directory traversable and
	// the private key group-readable so it can present the mTLS client cert.
	if svc, ok := bundle.Services["guardian-pusher-docker"]; ok {
		_ = os.Chmod(filepath.Join(bundle.Dir, "guardian-pusher-docker"), 0o750)
		_ = os.Chmod(svc.KeyFile, 0o640)
	}

	namespaces := map[string]struct{}{}
	for _, svc := range bundle.Services {
		namespaces[svc.Namespace] = struct{}{}
	}
	for ns := range namespaces {
		if err := ensureNamespace(ctx, ns, dryRun); err != nil {
			return err
		}
	}
	names := make([]string, 0, len(bundle.Services))
	for name := range bundle.Services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		svc := bundle.Services[name]
		if err := applyTLSSecret(ctx, svc, dryRun); err != nil {
			return err
		}
	}
	if err := writeGuardianctlEnv(bundle); err != nil {
		return err
	}
	fmt.Printf("mTLS: %d service certificates in %s (CA %s)\n", len(bundle.Services), bundle.Dir, bundle.CAFile)
	return nil
}

func ensureNamespace(ctx context.Context, namespace string, dryRun bool) error {
	if strings.TrimSpace(namespace) == "" {
		return nil
	}
	if !dryRun {
		cmd := exec.CommandContext(ctx, "kubectl", "get", "namespace", namespace)
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	manifest := fmt.Sprintf(`{"apiVersion":"v1","kind":"Namespace","metadata":{"name":%q}}`, namespace)
	return kubectlApplyJSON(ctx, manifest, dryRun, "namespace "+namespace)
}

func applyTLSSecret(ctx context.Context, svc pki.ServiceBundle, dryRun bool) error {
	cert, err := os.ReadFile(svc.CertFile)
	if err != nil {
		return fmt.Errorf("mtls: read %s: %w", svc.CertFile, err)
	}
	key, err := os.ReadFile(svc.KeyFile)
	if err != nil {
		return fmt.Errorf("mtls: read %s: %w", svc.KeyFile, err)
	}
	ca, err := os.ReadFile(svc.CAFile)
	if err != nil {
		return fmt.Errorf("mtls: read %s: %w", svc.CAFile, err)
	}
	manifest, err := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata": map[string]any{
			"name":      "strata-tls-" + svc.Name,
			"namespace": svc.Namespace,
			"labels": map[string]string{
				"app.kubernetes.io/managed-by": "stratatools",
				"strata.dev/mtls":              "true",
			},
		},
		"type": "Opaque",
		"data": map[string]string{
			"tls.crt": base64.StdEncoding.EncodeToString(cert),
			"tls.key": base64.StdEncoding.EncodeToString(key),
			"ca.crt":  base64.StdEncoding.EncodeToString(ca),
		},
	})
	if err != nil {
		return fmt.Errorf("mtls: encode secret %s: %w", svc.Name, err)
	}
	return kubectlApplyJSON(ctx, string(manifest), dryRun, "secret "+svc.Namespace+"/strata-tls-"+svc.Name)
}

func kubectlApplyJSON(ctx context.Context, manifest string, dryRun bool, what string) error {
	if dryRun {
		fmt.Printf("  (dry-run) would apply %s\n", what)
		return nil
	}
	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(manifest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mtls: apply %s: %w: %s", what, err, strings.TrimSpace(stderr.String()))
	}
	fmt.Printf("  applied %s\n", what)
	return nil
}

func writeGuardianctlEnv(bundle *pki.Bundle) error {
	svc, ok := bundle.Services["guardiand"]
	if !ok {
		return nil
	}
	abs := func(value string) string {
		if out, err := filepath.Abs(value); err == nil {
			return out
		}
		return value
	}
	content := fmt.Sprintf(`# Generated by guardianctl dev init / dev mtls.
# Source this file before running guardianctl against the local mTLS stack.
export STRATA_TLS_MODE=optional
export STRATA_TLS_CA_FILE=%s
export STRATA_TLS_CERT_FILE=%s
export STRATA_TLS_KEY_FILE=%s
`, abs(svc.CAFile), abs(svc.CertFile), abs(svc.KeyFile))
	path := filepath.Join(bundle.Dir, "guardianctl.env")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("mtls: write %s: %w", path, err)
	}
	return nil
}
