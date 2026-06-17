package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RenderTemplate renders a single template file with envsubst.
// extra is merged into the base env for this render (e.g., SUFFIX, NODE_NAME).
func RenderTemplate(templatePath string, baseEnv Env, extra Env) (string, error) {
	tmpl, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("reading template %s: %w", templatePath, err)
	}

	// Build the environment for envsubst
	cmdEnv := os.Environ()
	for k, v := range baseEnv {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}
	for k, v := range extra {
		cmdEnv = append(cmdEnv, fmt.Sprintf("%s=%s", k, v))
	}

	cmd := exec.Command("envsubst")
	cmd.Env = cmdEnv
	cmd.Stdin = bytes.NewReader(tmpl)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("envsubst %s: %w\n%s", templatePath, err, stderr.String())
	}

	return stdout.String(), nil
}

// ApplyTemplate renders and applies a template via kubectl apply.
// Set dryRun to true for --dry-run=client mode.
func ApplyTemplate(ctx context.Context, templatePath string, baseEnv Env, extra Env, dryRun bool) error {
	rendered, err := RenderTemplate(templatePath, baseEnv, extra)
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "+ kubectl apply -f - (<<< %d bytes from %s)\n", len(rendered), filepath.Base(templatePath))
	if dryRun {
		// Print first 200 chars for preview
		preview := rendered
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		fmt.Println(preview)
		return nil
	}

	cmd := exec.CommandContext(ctx, "kubectl", "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(rendered)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ApplyTemplatesForEach applies a template once for each suffix value.
// E.g., deploy-node.yaml rendered for node-a, node-b, node-c, node-d, node-e.
func ApplyTemplatesForEach(ctx context.Context, templatePath string, baseEnv Env, extraFn func(suffix string) Env, suffixes []string, dryRun bool) error {
	for _, suffix := range suffixes {
		extra := extraFn(suffix)
		if err := ApplyTemplate(ctx, templatePath, baseEnv, extra, dryRun); err != nil {
			return fmt.Errorf("rendering %s for %s: %w", templatePath, suffix, err)
		}
	}
	return nil
}

// BuildMonoFSImages builds all MonoFS Docker images from the monofs sibling repo.
func BuildMonoFSImages(ctx context.Context, cfg *Config, dryRun bool) error {
	img := cfg.Storage.Images
	targets := []struct {
		target string
		tag    string
	}{
		{"server", img.Server},
		{"router", img.Router},
		{"fetcher", img.Fetcher},
		{"search", img.Search},
		{"registry", img.Registry},
	}

	monofsDir := os.Getenv("MONOFS_REPO_DIR")
	if monofsDir == "" {
		monofsDir = filepath.Join(findRoot(), "..", "monofs")
	}

	for _, t := range targets {
		fmt.Fprintf(os.Stderr, "=== building %s (%s) ===\n", t.target, t.tag)
		if err := Run(ctx, dryRun, "docker", "build", "-t", t.tag, "--target", t.target, monofsDir); err != nil {
			return fmt.Errorf("building monofs %s: %w", t.target, err)
		}
	}

	// Build LB image
	lbDir := os.Getenv("LB_REPO_DIR")
	if lbDir == "" {
		lbDir = filepath.Join(findRoot(), "..", "lb")
	}
	fmt.Fprintf(os.Stderr, "=== building lb (%s) ===\n", img.LB)
	dockerfile := filepath.Join(lbDir, "Dockerfile")
	if err := Run(ctx, dryRun, "docker", "build", "-t", img.LB, "-f", dockerfile, lbDir); err != nil {
		return fmt.Errorf("building lb: %w", err)
	}

	return nil
}

// BuildGuardianImages builds guardian Docker images.
func BuildGuardianImages(ctx context.Context, cfg *Config, dryRun bool) error {
	guardianDir := os.Getenv("GUARDIAN_REPO_DIR")
	if guardianDir == "" {
		guardianDir = filepath.Join(findRoot(), "..", "guardian")
	}
	monofsDir := os.Getenv("MONOFS_REPO_DIR")
	if monofsDir == "" {
		monofsDir = filepath.Join(findRoot(), "..", "monofs")
	}
	kvsDir := os.Getenv("KVS_REPO_DIR")
	if kvsDir == "" {
		kvsDir = filepath.Join(findRoot(), "..", "kvs")
	}
	img := cfg.Guardian.Images

	// Build guardiand
	fmt.Fprintf(os.Stderr, "=== building guardiand (%s) ===\n", img.Guardiand)
	if err := Run(ctx, dryRun, "docker", "build",
		"-t", img.Guardiand,
		"--build-context", "monofs="+monofsDir,
		"--build-context", "kvs="+kvsDir,
		guardianDir,
	); err != nil {
		return fmt.Errorf("building guardiand: %w", err)
	}

	// Build K8s pusher
	fmt.Fprintf(os.Stderr, "=== building guardian-pusher-k8s (%s) ===\n", img.PusherK8s)
	k8sDockerfile := filepath.Join(guardianDir, "Dockerfile.pusher-k8s")
	if err := Run(ctx, dryRun, "docker", "build",
		"-t", img.PusherK8s,
		"-f", k8sDockerfile,
		"--build-context", "monofs="+monofsDir,
		"--build-context", "kvs="+kvsDir,
		guardianDir,
	); err != nil {
		return fmt.Errorf("building k8s pusher: %w", err)
	}

	// Build AWS pusher (if enabled)
	if cfg.Guardian.Pushers.AWS.Enabled {
		fmt.Fprintf(os.Stderr, "=== building guardian-pusher-aws (%s) ===\n", img.PusherAws)
		awsDockerfile := filepath.Join(guardianDir, "Dockerfile.pusher-aws")
		if err := Run(ctx, dryRun, "docker", "build",
			"-t", img.PusherAws,
			"-f", awsDockerfile,
			"--build-context", "monofs="+monofsDir,
			"--build-context", "kvs="+kvsDir,
			guardianDir,
		); err != nil {
			return fmt.Errorf("building aws pusher: %w", err)
		}
	}

	// Build Docker pusher
	fmt.Fprintf(os.Stderr, "=== building guardian-pusher-docker (%s) ===\n", img.PusherDocker)
	dockerPusherDockerfile := filepath.Join(guardianDir, "Dockerfile.pusher-docker")
	if err := Run(ctx, dryRun, "docker", "build",
		"-t", img.PusherDocker,
		"-f", dockerPusherDockerfile,
		"--build-context", "monofs="+monofsDir,
		"--build-context", "kvs="+kvsDir,
		guardianDir,
	); err != nil {
		return fmt.Errorf("building docker pusher: %w", err)
	}

	return nil
}

// LoadImagesIntoKind loads images into a kind cluster.
func LoadImagesIntoKind(ctx context.Context, images []string, dryRun bool) error {
	if dryRun {
		for _, img := range images {
			fmt.Fprintf(os.Stderr, "+ kind load docker-image %s\n", img)
		}
		return nil
	}

	// Check if kind cluster exists
	ctxName := kubectlQuery("config", "current-context")
	if !strings.HasPrefix(ctxName, "kind-") {
		return fmt.Errorf("not a kind cluster context: %s", ctxName)
	}

	// Get cluster name from context
	clusterName := strings.TrimPrefix(ctxName, "kind-")

	for _, img := range images {
		fmt.Fprintf(os.Stderr, "+ kind load docker-image %s --name %s\n", img, clusterName)
		cmd := exec.CommandContext(ctx, "kind", "load", "docker-image", img, "--name", clusterName)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("loading image %s into kind: %w", img, err)
		}
	}
	return nil
}

// WaitForDeployment waits for a deployment rollout to complete.
func WaitForDeployment(ctx context.Context, namespace, name string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "+ kubectl -n %s rollout status deployment/%s\n", namespace, name)
		return nil
	}
	return Run(ctx, false, "kubectl", "-n", namespace, "rollout", "status", "deployment/"+name, "--timeout=120s")
}

// ScaleDeployments scales all deployments in a namespace to the given replicas.
func ScaleDeployments(ctx context.Context, namespace string, replicas int, dryRun bool) error {
	return Run(ctx, dryRun, "kubectl", "-n", namespace, "scale", "deployment", "--all", fmt.Sprintf("--replicas=%d", replicas))
}

// DeleteNamespace deletes a namespace (non-blocking).
func DeleteNamespace(ctx context.Context, namespace string, dryRun bool) error {
	return Run(ctx, dryRun, "kubectl", "delete", "namespace", namespace, "--ignore-not-found", "--wait=false")
}

// DeleteClusterRoleBinding deletes a cluster role binding.
func DeleteClusterRoleBinding(ctx context.Context, name string, dryRun bool) error {
	return Run(ctx, dryRun, "kubectl", "delete", "clusterrolebinding", name, "--ignore-not-found")
}

// ConfigureKindRegistryForContainerd configures containerd on each kind node
// to pull from the monofs-registry service. Writes hosts.toml for both
// registry.strata.local:5000 (direct strata images) and docker.io (proxied via
// monofs-registry's pull-through to Docker Hub).
func ConfigureKindRegistryForContainerd(ctx context.Context, kindClusterName, registryNamespace, registryServiceName string, dryRun bool) error {
	ctxName := kubectlQuery("config", "current-context")
	if !strings.HasPrefix(ctxName, "kind-") {
		return fmt.Errorf("not a kind cluster context: %s", ctxName)
	}

	if kindClusterName == "" {
		kindClusterName = strings.TrimPrefix(ctxName, "kind-")
	}

	clusterIP := KubectlGetServiceIP(registryNamespace, registryServiceName)
	if clusterIP == "" {
		return fmt.Errorf("service %s/%s not found or has no ClusterIP", registryNamespace, registryServiceName)
	}

	nodes, err := RunCapture(ctx, "kind", "get", "nodes", "--name", kindClusterName)
	if err != nil {
		return fmt.Errorf("listing kind nodes: %w", err)
	}

	hostsToml := fmt.Sprintf(`server = "http://%s:5000"

[host."http://%s:5000"]
  capabilities = ["pull", "resolve"]
  skip_verify = true
`, clusterIP, clusterIP)

	registries := []string{"registry.strata.local:5000"}

	for _, node := range strings.Split(nodes, "\n") {
		node = strings.TrimSpace(node)
		if node == "" {
			continue
		}

		for _, reg := range registries {
			regDir := "/etc/containerd/certs.d/" + reg

			fmt.Fprintf(os.Stderr, "  configuring containerd on kind node %s for %s -> %s:5000\n", node, reg, clusterIP)

			if dryRun {
				continue
			}

			if err := Run(ctx, false, "docker", "exec", node, "mkdir", "-p", regDir); err != nil {
				return fmt.Errorf("creating certs.d dir on node %s: %w", node, err)
			}

			cmd := exec.CommandContext(ctx, "docker", "exec", "-i", node, "tee", regDir+"/hosts.toml")
			cmd.Stdin = strings.NewReader(hostsToml)
			cmd.Stdout = os.Stderr
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("writing hosts.toml on node %s: %w", node, err)
			}
		}
	}

	return nil
}

// PatchServiceFinalizer clears finalizers on all services in a namespace.
func PatchServiceFinalizers(ctx context.Context, namespace string, dryRun bool) error {
	if dryRun {
		fmt.Fprintf(os.Stderr, "+ kubectl -n %s patch services --type=merge -p '{\"metadata\":{\"finalizers\":[]}}'\n", namespace)
		return nil
	}

	// Get service names
	out, err := RunCapture(ctx, "kubectl", "-n", namespace, "get", "service", "-o", "name")
	if err != nil {
		return nil // No services, nothing to patch
	}

	for _, svc := range strings.Split(out, "\n") {
		svc = strings.TrimSpace(svc)
		if svc == "" {
			continue
		}
		_ = Run(ctx, false, "kubectl", "-n", namespace, "patch", svc, "--type=merge", "-p", `{"metadata":{"finalizers":[]}}`)
	}
	return nil
}
