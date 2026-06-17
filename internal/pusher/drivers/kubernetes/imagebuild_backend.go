package kubernetesdriver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ImageBuildBackendAPI interface {
	BuildAndPublish(ctx context.Context, req ImageBuildRequest) (ImageBuildResult, error)
	LoadAndPush(ctx context.Context, req ImageLoadRequest) (ImageBuildResult, error)
	StampImage(ctx context.Context, currentRef, newRef string) error
	ImageExists(ctx context.Context, imageRef string) (bool, error)
}

type ImageBuildRequest struct {
	WorkspaceDir string
	Dockerfile   string
	ImageRef     string
	Target       string
	Platform     string
	BuildArgs    map[string]string
	Insecure     bool
}

type ImageLoadRequest struct {
	TarPath     string
	ImageRef    string
	SourceImage string
}

type BuildLogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
}

type ImageBuildResult struct {
	ImageRef string
	Logs     []BuildLogEntry
}

type ImageBuildBackend struct {
	kubectl string
}

func NewImageBuildBackend() *ImageBuildBackend {
	kubectl, _ := exec.LookPath("kubectl")
	if kubectl == "" {
		kubectl = "kubectl"
	}
	return &ImageBuildBackend{kubectl: kubectl}
}

func (b *ImageBuildBackend) BuildAndPublish(ctx context.Context, req ImageBuildRequest) (ImageBuildResult, error) {
	archivePath, archiveCleanup, err := createBuildContextArchive(req.WorkspaceDir)
	if err != nil {
		return ImageBuildResult{}, err
	}
	defer archiveCleanup()

	kanikoImage := os.Getenv("GUARDIAN_KANIKO_IMAGE")
	if kanikoImage == "" {
		kanikoImage = "gcr.io/kaniko-project/executor:v1.24.0"
	}
	namespace := os.Getenv("GUARDIAN_NAMESPACE")
	if namespace == "" {
		namespace = "guardian"
	}
	registryHost := os.Getenv("GUARDIAN_IMAGE_BUILD_REGISTRY")

	jobName := fmt.Sprintf("guardian-imagebuild-%d", time.Now().UnixNano())

	registryClusterIP, err := b.resolveRegistryClusterIP(ctx, namespace, registryHost)
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("resolve registry cluster IP: %w", err)
	}

	jobManifest := b.buildJobManifest(jobName, namespace, kanikoImage, registryHost, registryClusterIP, req)
	if err := b.applyManifest(jobManifest); err != nil {
		return ImageBuildResult{}, fmt.Errorf("create kaniko job %s: %w", jobName, err)
	}
	defer b.deleteJob(namespace, jobName)

	if err := b.waitForPodInit(ctx, namespace, jobName); err != nil {
		rawLogs, _ := b.jobLogs(namespace, jobName)
		return ImageBuildResult{}, fmt.Errorf("kaniko job init failed %s: %w\n%s", req.ImageRef, err, rawLogs)
	}
	if err := b.copyContextToJob(ctx, namespace, jobName, archivePath); err != nil {
		return ImageBuildResult{}, fmt.Errorf("copy build context to job %s: %w", jobName, err)
	}

	// Stream build logs while waiting. Keeps pusher logs useful and collects
	// build output to return as Guardian task log entries.
	var buildLogs []BuildLogEntry
	logCh := make(chan BuildLogEntry, 256)
	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		for entry := range logCh {
			buildLogs = append(buildLogs, entry)
		}
	}()

	buildErr := b.waitForJobStreaming(ctx, namespace, jobName, req.ImageRef, logCh)
	close(logCh)
	<-logDone

	if buildErr != nil {
		// Collect final logs in case the job is still around.
		rawLogs, _ := b.jobLogs(namespace, jobName)
		// Surface them in the error so Guardian UI shows them.
		return ImageBuildResult{Logs: buildLogs}, fmt.Errorf("kaniko build %s failed: %w\n%s", req.ImageRef, buildErr, rawLogs)
	}
	return ImageBuildResult{ImageRef: req.ImageRef, Logs: buildLogs}, nil
}

func (b *ImageBuildBackend) resolveRegistryClusterIP(ctx context.Context, namespace, registryHost string) (string, error) {
	if registryHost == "" {
		return "", nil
	}
	host := strings.SplitN(registryHost, ":", 2)[0]
	out, err := exec.CommandContext(ctx, b.kubectl, "get", "svc", "monofs-registry", "-n", "monofs",
		"-o", "jsonpath={.spec.clusterIP}").CombinedOutput()
	if err != nil {
		log.Printf("[ImageBuild] registry-resolve host=%s kubectlError: %v", host, err)
		return "", nil
	}
	ip := strings.TrimSpace(string(out))
	if ip == "" || ip == "<none>" {
		log.Printf("[ImageBuild] registry-resolve host=%s notFound: monofs-registry has no ClusterIP", host)
		return "", nil
	}
	log.Printf("[ImageBuild] registry-resolve host=%s found service=monofs-registry clusterIP=%s", host, ip)
	return ip, nil
}

func (b *ImageBuildBackend) buildJobManifest(jobName, namespace, kanikoImage, registryHost, registryClusterIP string, req ImageBuildRequest) map[string]any {
	args := []string{
		"--context", "tar:///context/context.tar.gz",
		"--dockerfile", req.Dockerfile,
		"--destination", req.ImageRef,
		"--cache=false",
		"--force",
		"--insecure",
		"--skip-tls-verify",
		"--insecure-pull",
	}
	if req.Target != "" {
		args = append(args, "--target", req.Target)
	}
	if req.Platform != "" {
		args = append(args, "--custom-platform", req.Platform)
	}
	keys := make([]string, 0, len(req.BuildArgs))
	for k := range req.BuildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, req.BuildArgs[k]))
	}

	podSpec := map[string]any{
		"restartPolicy": "Never",
		"volumes": []map[string]any{
			{"name": "context", "emptyDir": map[string]any{}},
		},
		"initContainers": []map[string]any{
			{
				"name":    "loader",
				"image":   "busybox:1.36",
				"command": []string{"sh", "-c", "until [ -f /context/context.tar.gz ]; do sleep 1; done"},
				"volumeMounts": []map[string]any{
					{"name": "context", "mountPath": "/context"},
				},
			},
		},
		"containers": []map[string]any{
			{
				"name":  "kaniko",
				"image": kanikoImage,
				"args":  args,
				"volumeMounts": []map[string]any{
					{"name": "context", "mountPath": "/context"},
				},
			},
		},
	}

	// Add hostAliases so the Kaniko Job can resolve the local registry hostname.
	if registryClusterIP != "" && registryHost != "" {
		host := strings.SplitN(registryHost, ":", 2)[0]
		podSpec["hostAliases"] = []map[string]any{
			{
				"ip":        registryClusterIP,
				"hostnames": []string{host},
			},
		}
	}

	ttl := int32(600)
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": namespace,
			"labels":    map[string]string{"guardian.managed": "true", "guardian.role": "imagebuild"},
		},
		"spec": map[string]any{
			"ttlSecondsAfterFinished": ttl,
			"backoffLimit":            0,
			"template": map[string]any{
				"spec": podSpec,
			},
		},
	}
}

func (b *ImageBuildBackend) applyManifest(obj map[string]any) error {
	data, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	cmd := exec.Command(b.kubectl, "apply", "-f", "-")
	cmd.Stdin = strings.NewReader(string(data))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (b *ImageBuildBackend) waitForJob(ctx context.Context, namespace, jobName string) error {
	return b.waitForJobStreaming(ctx, namespace, jobName, jobName, nil)
}

func (b *ImageBuildBackend) waitForJobStreaming(ctx context.Context, namespace, jobName, imageRef string, logCh chan<- BuildLogEntry) error {
	deadline := time.Now().Add(30 * time.Minute)
	logTicker := time.NewTicker(30 * time.Second)
	defer logTicker.Stop()
	var lastLogLines int

	emit := func(level, msg string) {
		entry := BuildLogEntry{Timestamp: time.Now().UTC(), Level: level, Message: msg}
		if logCh != nil {
			select {
			case logCh <- entry:
			default:
			}
		}
		log.Printf("[ImageBuild] image=%s %s", imageRef, msg)
	}

	emit("info", fmt.Sprintf("kaniko job %s started", jobName))

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-logTicker.C:
			// Stream latest Kaniko logs as progress.
			if out, err := exec.CommandContext(ctx, b.kubectl, "-n", namespace, "logs",
				"-l", fmt.Sprintf("job-name=%s", jobName), "-c", "kaniko",
				fmt.Sprintf("--tail=%d", 20)).CombinedOutput(); err == nil {
				lines := strings.Split(strings.TrimSpace(string(out)), "\n")
				newLines := lines
				if len(lines) > lastLogLines && lastLogLines > 0 {
					newLines = lines[lastLogLines:]
				}
				lastLogLines = len(lines)
				for _, l := range newLines {
					if l = strings.TrimSpace(l); l != "" {
						emit("info", l)
					}
				}
			}
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for job %s", jobName)
		}
		out, err := exec.CommandContext(ctx, b.kubectl, "-n", namespace, "get", "job", jobName,
			"-o", "jsonpath={.status.succeeded},{.status.failed}").CombinedOutput()
		if err == nil {
			parts := strings.Split(strings.TrimSpace(string(out)), ",")
			if len(parts) == 2 {
				if parts[0] == "1" {
					emit("info", fmt.Sprintf("kaniko job %s succeeded", jobName))
					return nil
				}
				if parts[1] != "" && parts[1] != "0" {
					// Collect final failure logs before returning error.
					if out, err := exec.CommandContext(ctx, b.kubectl, "-n", namespace, "logs",
						"-l", fmt.Sprintf("job-name=%s", jobName), "--all-containers",
						"--tail=100").CombinedOutput(); err == nil {
						for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
							if l = strings.TrimSpace(l); l != "" {
								emit("error", l)
							}
						}
					}
					return fmt.Errorf("job %s failed", jobName)
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func (b *ImageBuildBackend) jobLogs(namespace, jobName string) (string, error) {
	out, err := exec.Command(b.kubectl, "-n", namespace, "logs",
		"-l", fmt.Sprintf("job-name=%s", jobName), "--tail=200").CombinedOutput()
	return string(out), err
}

func (b *ImageBuildBackend) deleteJob(namespace, jobName string) {
	_ = exec.Command(b.kubectl, "-n", namespace, "delete", "job", jobName,
		"--ignore-not-found", "--cascade=foreground").Run()
}

func (b *ImageBuildBackend) waitForPodInit(ctx context.Context, namespace, jobName string) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for init container in job %s", jobName)
		}
		out, _ := exec.CommandContext(ctx, b.kubectl, "-n", namespace, "get", "pod",
			"-l", fmt.Sprintf("job-name=%s", jobName),
			"-o", "jsonpath={.items[0].status.initContainerStatuses[0].state.running}").CombinedOutput()
		if strings.Contains(string(out), "startedAt") {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (b *ImageBuildBackend) copyContextToJob(ctx context.Context, namespace, jobName, archivePath string) error {
	// Get the pod name for the job
	out, err := exec.CommandContext(ctx, b.kubectl, "-n", namespace, "get", "pod",
		"-l", fmt.Sprintf("job-name=%s", jobName),
		"-o", "jsonpath={.items[0].metadata.name}").CombinedOutput()
	if err != nil {
		return fmt.Errorf("get job pod: %w: %s", err, strings.TrimSpace(string(out)))
	}
	podName := strings.TrimSpace(string(out))
	if podName == "" {
		return fmt.Errorf("no pod found for job %s", jobName)
	}
	dest := fmt.Sprintf("%s/%s:/context/context.tar.gz", namespace, podName)
	cpOut, err := exec.CommandContext(ctx, b.kubectl, "cp", archivePath, dest, "-c", "loader").CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl cp context: %w: %s", err, strings.TrimSpace(string(cpOut)))
	}
	return nil
}

func createBuildContextArchive(workspaceDir string) (string, func(), error) {
	tempFile, err := os.CreateTemp("", "guardian-imagebuild-*.tar.gz")
	if err != nil {
		return "", func() {}, fmt.Errorf("create image build archive: %w", err)
	}
	archivePath := tempFile.Name()
	cleanup := func() { _ = os.Remove(archivePath) }

	gzipWriter := gzip.NewWriter(tempFile)
	tarWriter := tar.NewWriter(gzipWriter)
	walkErr := filepath.Walk(workspaceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if path == workspaceDir {
			return nil
		}
		relPath, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return err
		}
		relPath = filepath.ToSlash(relPath)
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relPath
		if info.IsDir() && !strings.HasSuffix(header.Name, "/") {
			header.Name += "/"
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(tarWriter, file)
		return err
	})
	closeErr := tarWriter.Close()
	gzipErr := gzipWriter.Close()
	fileErr := tempFile.Close()
	if walkErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("archive image build context: %w", walkErr)
	}
	if closeErr != nil || gzipErr != nil || fileErr != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("finalize image build archive")
	}
	return archivePath, cleanup, nil
}

func (b *ImageBuildBackend) ImageExists(ctx context.Context, imageRef string) (bool, error) {
	host, repo, tag, ok := parseImageRef(imageRef)
	if !ok {
		log.Printf("[ImageBuild] registry-check image=%s parseFailed: could not parse imageRef", imageRef)
		return false, nil
	}
	url := fmt.Sprintf("http://%s/v2/%s/manifests/%s", host, repo, tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		log.Printf("[ImageBuild] registry-check image=%s requestError url=%s: %v", imageRef, url, err)
		return false, nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[ImageBuild] registry-check image=%s httpError url=%s: %v", imageRef, url, err)
		return false, nil
	}
	resp.Body.Close()
	exists := resp.StatusCode == http.StatusOK
	if !exists {
		log.Printf("[ImageBuild] registry-check image=%s notFound status=%d url=%s", imageRef, resp.StatusCode, url)
	}
	return exists, nil
}

func parseImageRef(imageRef string) (host, repo, tag string, ok bool) {
	ref := strings.TrimSpace(imageRef)
	colonIdx := strings.LastIndex(ref, ":")
	if colonIdx < 0 {
		return "", "", "", false
	}
	tag = ref[colonIdx+1:]
	rest := ref[:colonIdx]
	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return "", "", "", false
	}
	host = rest[:slashIdx]
	repo = rest[slashIdx+1:]
	return host, repo, tag, true
}

func (b *ImageBuildBackend) StampImage(ctx context.Context, currentRef, newRef string) error {
	if output, err := exec.CommandContext(ctx, "docker", "tag", currentRef, newRef).CombinedOutput(); err != nil {
		return fmt.Errorf("docker tag %s -> %s failed: %w\n%s", currentRef, newRef, err, string(output))
	}
	if output, err := exec.CommandContext(ctx, "docker", "push", newRef).CombinedOutput(); err != nil {
		return fmt.Errorf("docker push %s failed: %w\n%s", newRef, err, string(output))
	}
	return nil
}

func (b *ImageBuildBackend) LoadAndPush(ctx context.Context, req ImageLoadRequest) (ImageBuildResult, error) {
	archivePath, archiveCleanup, err := prepareTarContext(req.TarPath)
	if err != nil {
		return ImageBuildResult{}, err
	}
	defer archiveCleanup()

	skopeoImage := os.Getenv("GUARDIAN_SKOPEO_IMAGE")
	if skopeoImage == "" {
		skopeoImage = "quay.io/skopeo/stable:latest"
	}
	namespace := os.Getenv("GUARDIAN_NAMESPACE")
	if namespace == "" {
		namespace = "guardian"
	}
	registryHost := os.Getenv("GUARDIAN_IMAGE_BUILD_REGISTRY")

	jobName := fmt.Sprintf("guardian-imageload-%d", time.Now().UnixNano())

	registryClusterIP, err := b.resolveRegistryClusterIP(ctx, namespace, registryHost)
	if err != nil {
		return ImageBuildResult{}, fmt.Errorf("resolve registry cluster IP: %w", err)
	}

	jobManifest := b.buildPushJobManifest(jobName, namespace, skopeoImage, registryHost, registryClusterIP, req)
	if err := b.applyManifest(jobManifest); err != nil {
		return ImageBuildResult{}, fmt.Errorf("create skopeo job %s: %w", jobName, err)
	}
	defer b.deleteJob(namespace, jobName)

	if err := b.waitForPodInit(ctx, namespace, jobName); err != nil {
		rawLogs, _ := b.jobLogs(namespace, jobName)
		return ImageBuildResult{}, fmt.Errorf("skopeo job init failed %s: %w\n%s", req.ImageRef, err, rawLogs)
	}
	if err := b.copyContextToJob(ctx, namespace, jobName, archivePath); err != nil {
		return ImageBuildResult{}, fmt.Errorf("copy tar to job %s: %w", jobName, err)
	}

	var buildLogs []BuildLogEntry
	logCh := make(chan BuildLogEntry, 256)
	logDone := make(chan struct{})
	go func() {
		defer close(logDone)
		for entry := range logCh {
			buildLogs = append(buildLogs, entry)
		}
	}()

	buildErr := b.waitForJobStreaming(ctx, namespace, jobName, req.ImageRef, logCh)
	close(logCh)
	<-logDone

	if buildErr != nil {
		rawLogs, _ := b.jobLogs(namespace, jobName)
		return ImageBuildResult{Logs: buildLogs}, fmt.Errorf("skopeo push %s failed: %w\n%s", req.ImageRef, buildErr, rawLogs)
	}
	return ImageBuildResult{ImageRef: req.ImageRef, Logs: buildLogs}, nil
}

func prepareTarContext(tarPath string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "guardian-imageload-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("create temp dir for tar context: %w", err)
	}
	destPath := filepath.Join(tmpDir, "image.tar")
	src, err := os.Open(tarPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("open source tar: %w", err)
	}
	defer src.Close()
	dst, err := os.Create(destPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("create dest tar copy: %w", err)
	}
	defer dst.Close()
	if _, err := io.Copy(dst, src); err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, fmt.Errorf("copy tar: %w", err)
	}
	dst.Close()
	src.Close()

	archivePath, archiveCleanup, err := createBuildContextArchive(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		return "", func() {}, err
	}
	os.RemoveAll(tmpDir)
	return archivePath, archiveCleanup, nil
}

func (b *ImageBuildBackend) buildPushJobManifest(jobName, namespace, skopeoImage, registryHost, registryClusterIP string, req ImageLoadRequest) map[string]any {
	podSpec := map[string]any{
		"restartPolicy": "Never",
		"volumes": []map[string]any{
			{"name": "context", "emptyDir": map[string]any{}},
		},
		"initContainers": []map[string]any{
			{
				"name":    "loader",
				"image":   "busybox:1.36",
				"command": []string{"sh", "-c", "until [ -f /context/context.tar.gz ]; do sleep 1; done; tar -xzf /context/context.tar.gz -C /context"},
				"volumeMounts": []map[string]any{
					{"name": "context", "mountPath": "/context"},
				},
			},
		},
		"containers": []map[string]any{
			{
				"name":  "skopeo",
				"image": skopeoImage,
				"command": []string{
					"skopeo", "copy",
					"--src-tls-verify=false", "--dest-tls-verify=false",
					"docker-archive:///context/image.tar",
					"docker://" + req.ImageRef,
				},
				"volumeMounts": []map[string]any{
					{"name": "context", "mountPath": "/context"},
				},
			},
		},
	}

	if registryClusterIP != "" && registryHost != "" {
		host := strings.SplitN(registryHost, ":", 2)[0]
		podSpec["hostAliases"] = []map[string]any{
			{
				"ip":        registryClusterIP,
				"hostnames": []string{host},
			},
		}
	}

	ttl := int32(600)
	return map[string]any{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": map[string]any{
			"name":      jobName,
			"namespace": namespace,
			"labels":    map[string]string{"guardian.managed": "true", "guardian.role": "imageload"},
		},
		"spec": map[string]any{
			"ttlSecondsAfterFinished": ttl,
			"backoffLimit":            0,
			"template": map[string]any{
				"spec": podSpec,
			},
		},
	}
}

