package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/rydzu/ainfra/guardian/internal/bootstrap"
	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
	"gopkg.in/yaml.v3"
)

func releaseCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("release", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partition := flags.String("partition", "", "partition name to release")
	allFlag := flags.Bool("all", false, "release all partitions")
	dirFlag := flags.String("dir", "", "path to partition directory")
	bump := flags.Bool("bump", false, "bump asset versions before releasing")
	waitFlag := flags.Bool("wait", false, "wait for partition convergence")
	reconcile := flags.Bool("reconcile", false, "run reconcile after push")
	skipBuild := flags.Bool("skip-build", false, "skip image build")
	skipPush := flags.Bool("skip-push", false, "skip image push")
	skipStamp := flags.Bool("skip-stamp", false, "skip image stamp")
	skipGuardian := flags.Bool("skip-guardian", false, "skip guardianctl push")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")
	monofsRouter := flags.String("monofs-router", os.Getenv("GUARDIAN_MONOFS_ROUTER"), "MonoFS router address for partition push")
	monofsToken := flags.String("monofs-token", os.Getenv("GUARDIAN_MONOFS_TOKEN"), "MonoFS token for partition push")

	return &command.Command{
		Description: "Release a partition (build images -> stamp -> push -> reconcile)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			storeArgs := storeFlags(*monofsRouter, *monofsToken)
			if *allFlag {
				return releaseAll(ctx, printer, *bump, *waitFlag, *reconcile, *skipBuild, *skipPush, *skipStamp, *skipGuardian, *dryRun, storeArgs)
			}

			if *partition == "" && *dirFlag == "" {
				return fmt.Errorf("--partition or --dir is required (or use --all)")
			}

			partDir := *dirFlag
			partName := *partition
			if partDir == "" {
				partDir = findPartitionDir(partName)
			}
			if partName == "" {
				partName = partitionNameFromDir(partDir)
			}
			if partDir == "" {
				return fmt.Errorf("partition directory not found (set ST_ROOT or use --dir)")
			}

			return releaseOne(ctx, printer, partName, partDir, *bump, *waitFlag, *reconcile, *skipBuild, *skipPush, *skipStamp, *skipGuardian, *dryRun, storeArgs)
		},
	}
}

func imageCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("image", flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	partitionFlag := flags.String("partition", "", "partition name")
	dirFlag := flags.String("dir", "", "path to partition directory")
	dryRun := flags.Bool("dry-run", false, "print commands without executing")

	return &command.Command{
		Description: "Build, push, and stamp container images for a partition",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			partDir := *dirFlag
			partName := *partitionFlag

			// Extract flags that may appear after positional args (e.g. "build --dir PATH")
			remaining := make([]string, 0, len(args))
			for i := 0; i < len(args); i++ {
				switch args[i] {
				case "--dir":
					if i+1 < len(args) {
						partDir = args[i+1]
						i++
					}
				case "--partition":
					if i+1 < len(args) {
						partName = args[i+1]
						i++
					}
				case "--dry-run":
					*dryRun = true
				default:
					remaining = append(remaining, args[i])
				}
			}
			args = remaining

			if partDir == "" && partName != "" {
				partDir = findPartitionDir(partName)
			}
			if partDir == "" {
				return fmt.Errorf("--dir or --partition is required")
			}

			for _, step := range args {
				switch step {
				case "build":
					return imageBuild(ctx, partDir, *dryRun)
				case "push":
					return imagePush(ctx, partDir, *dryRun)
				case "stamp":
					return imageStamp(ctx, partDir, *dryRun)
				default:
					return fmt.Errorf("unknown image step: %s (expected: build, push, stamp)", step)
				}
			}

			// Default: build + push + stamp
			if err := imageBuild(ctx, partDir, *dryRun); err != nil {
				return err
			}
			if err := imagePush(ctx, partDir, *dryRun); err != nil {
				return err
			}
			return imageStamp(ctx, partDir, *dryRun)
		},
	}
}

func releaseOne(ctx context.Context, printer *output.Printer, name, dir string, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun bool, storeArgs []string) error {
	fmt.Fprintf(os.Stderr, "=== releasing partition %s ===\n", name)

	// Bump versions
	if bump {
		fmt.Fprintf(os.Stderr, "=== bumping versions ===\n")
		if err := bootstrap.Run(ctx, dryRun, "guardianctl", "partition", "tag", "--dir", dir); err != nil {
			return fmt.Errorf("bump: %w", err)
		}
	}

	// Apply dev tag to sourceImage references so builds use the right version
	tag := readDevTag()
	if tag == "" {
		tag = resolveDevTag(ctx)
	}
	_ = patchImageSources(dir, tag, dryRun)

	// Build and stamp images
	if !skipBuild {
		fmt.Fprintf(os.Stderr, "=== building images with tag %s ===\n", tag)
		if err := imageBuild(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image build: %w", err)
		}
	}
	if !skipPush {
		if err := imagePush(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image push: %w", err)
		}
	}
	if !skipStamp {
		if err := imageStamp(ctx, dir, dryRun); err != nil {
			return fmt.Errorf("image stamp: %w", err)
		}
	}

	// Push to Guardian
	if !skipGuardian {
		fmt.Fprintf(os.Stderr, "=== pushing partition to Guardian ===\n")
		pushArgs := append([]string{"guardianctl"}, storeArgs...)
		pushArgs = append(pushArgs, "partition", "push", "--dir", dir)
		if err := bootstrap.Run(ctx, dryRun, pushArgs[0], pushArgs[1:]...); err != nil {
			return fmt.Errorf("partition push: %w", err)
		}

		// Reconcile
		if reconcile {
			fmt.Fprintf(os.Stderr, "=== reconciling partition %s ===\n", name)
			recArgs := append([]string{"guardianctl"}, storeArgs...)
			recArgs = append(recArgs, "partition", "reconcile", "--partition", name)
			if err := bootstrap.Run(ctx, dryRun, recArgs[0], recArgs[1:]...); err != nil {
				return fmt.Errorf("partition reconcile: %w", err)
			}
		}

		// Wait
		if wait {
			fmt.Fprintf(os.Stderr, "=== waiting for partition %s ===\n", name)
			waitArgs := append([]string{"guardianctl"}, storeArgs...)
			waitArgs = append(waitArgs, "partition", "wait", "--partition", name)
			if err := bootstrap.Run(ctx, dryRun, waitArgs[0], waitArgs[1:]...); err != nil {
				return fmt.Errorf("partition wait: %w", err)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "=== partition %s released ===\n", name)
	return nil
}

func releaseAll(ctx context.Context, printer *output.Printer, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun bool, storeArgs []string) error {
	partitions := []string{
		"guardian-configs", "opentelemetry", "k8s-top",
		"doctor", "monitoring", "dev-workspace", "agent", "lolipop",
	}

	for _, name := range partitions {
		dir := findPartitionDir(name)
		if dir == "" {
			fmt.Fprintf(os.Stderr, "skipping %s: partition dir not found\n", name)
			continue
		}
		if err := releaseOne(ctx, printer, name, dir, bump, wait, reconcile, skipBuild, skipPush, skipStamp, skipGuardian, dryRun, storeArgs); err != nil {
			return fmt.Errorf("partition %s: %w", name, err)
		}
		fmt.Fprintln(os.Stderr)
	}
	return nil
}

func imageBuild(ctx context.Context, partitionDir string, dryRun bool) error {
	root := findRootDir()
	if root == "." {
		return fmt.Errorf("cannot find stratatools root; set ST_ROOT")
	}
	imagesPath := filepath.Join(partitionDir, "intents", "images.yaml")
	data, err := os.ReadFile(imagesPath)
	if err != nil {
		return nil // no images intent, nothing to build
	}
	var intent struct {
		Spec struct {
			Assets []struct {
				Name       string `yaml:"name"`
				Properties struct {
					SourceImage string `yaml:"sourceImage"`
				} `yaml:"properties"`
			} `yaml:"assets"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return fmt.Errorf("parse images intent: %w", err)
	}

	// Build image name → spec map from recipes
	specs := releaseImageSpecs(partitionDir)
	for _, asset := range intent.Spec.Assets {
		if asset.Properties.SourceImage == "" {
			continue
		}
		imageName := strings.SplitN(asset.Properties.SourceImage, ":", 2)[0]
		spec, ok := specs[imageName]
		if !ok {
			continue
		}
		fmt.Fprintf(os.Stderr, "=== building %s ===\n", asset.Properties.SourceImage)
		args := []string{"docker", "build"}
		if spec.noCache {
			args = append(args, "--no-cache")
		}
		args = append(args, "-t", asset.Properties.SourceImage)
		if spec.dockerfile != "" {
			args = append(args, "-f", filepath.Join(spec.context, spec.dockerfile))
		}
		for _, ba := range spec.buildArgs {
			args = append(args, "--build-arg", ba)
		}
		for name, ctxPath := range spec.buildContexts {
			args = append(args, "--build-context", name+"="+ctxPath)
		}
		args = append(args, spec.context)
		if err := bootstrap.Run(ctx, dryRun, args[0], args[1:]...); err != nil {
			return fmt.Errorf("build %s: %w", asset.Properties.SourceImage, err)
		}
	}
	return nil
}

func imagePush(ctx context.Context, partitionDir string, dryRun bool) error {
	imagesPath := filepath.Join(partitionDir, "intents", "images.yaml")
	data, err := os.ReadFile(imagesPath)
	if err != nil {
		return nil
	}
	var intent struct {
		Spec struct {
			Assets []struct {
				Properties struct {
					SourceImage string `yaml:"sourceImage"`
					Registry    string `yaml:"registry"`
					Repository  string `yaml:"repository"`
				} `yaml:"properties"`
			} `yaml:"assets"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &intent); err != nil {
		return nil
	}
	ctxName, _ := bootstrap.RunCapture(ctx, "kubectl", "config", "current-context")
	isKind := strings.HasPrefix(ctxName, "kind-")
	for _, asset := range intent.Spec.Assets {
		tag := asset.Properties.SourceImage
		if tag == "" {
			continue
		}
		if isKind {
			if reg := asset.Properties.Registry; reg != "" {
				repo := asset.Properties.Repository
				if repo == "" {
					repo = strings.SplitN(tag, ":", 2)[0]
				}
				regTag := reg + "/" + repo + ":" + strings.SplitN(tag, ":", 2)[1]
				fmt.Fprintf(os.Stderr, "  pushing %s -> %s\n", tag, regTag)
				_ = bootstrap.Run(ctx, dryRun, "docker", "tag", tag, regTag)
				if err := bootstrap.Run(ctx, dryRun, "docker", "push", regTag); err != nil {
					return fmt.Errorf("push %s to registry: %w", regTag, err)
				}
			}
		} else {
			fmt.Fprintf(os.Stderr, "  pushing %s\n", tag)
			if err := bootstrap.Run(ctx, dryRun, "docker", "push", tag); err != nil {
				return fmt.Errorf("push %s: %w", tag, err)
			}
		}
	}
	return nil
}

func imageStamp(ctx context.Context, partitionDir string, dryRun bool) error {
	imagesPath := filepath.Join(partitionDir, "intents", "images.yaml")
	data, err := os.ReadFile(imagesPath)
	if err != nil {
		return nil
	}
	var imgIntent struct {
		Spec struct {
			Assets []struct {
				Name       string `yaml:"name"`
				Properties struct {
					SourceImage string `yaml:"sourceImage"`
					Registry    string `yaml:"registry"`
					Repository  string `yaml:"repository"`
				} `yaml:"properties"`
			} `yaml:"assets"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &imgIntent); err != nil {
		return nil
	}
	// Build asset-name → registry-prefixed image ref mapping
	refs := make(map[string]string)
	for _, a := range imgIntent.Spec.Assets {
		if a.Properties.SourceImage == "" {
			continue
		}
		if a.Properties.Registry != "" {
			repo := a.Properties.Repository
			if repo == "" {
				repo = strings.SplitN(a.Properties.SourceImage, ":", 2)[0]
			}
			parts := strings.SplitN(a.Properties.SourceImage, ":", 2)
			if len(parts) == 2 {
				refs[a.Name] = a.Properties.Registry + "/" + repo + ":" + parts[1]
			}
		} else {
			refs[a.Name] = a.Properties.SourceImage
		}
	}
	if len(refs) == 0 {
		return nil
	}

	// Resolve ${intent.images.outputs.<name>.imageRef} in all intent YAML files
	re := regexp.MustCompile(`\$\{intent\.images\.outputs\.([^.}]+)\.imageRef\}`)

	// Also build a bare-sourceImage → registry-ref map for repairing
	// already-hardcoded bare refs from previous releases (e.g. "doctor:abc123" → "registry.strata.local:5000/doctor:abc123")
	bareRefs := make(map[string]string) // "doctor:0608574" → "registry.strata.local:5000/doctor:0608574"
	for _, a := range imgIntent.Spec.Assets {
		if a.Properties.SourceImage != "" && a.Properties.Registry != "" {
			repo := a.Properties.Repository
			if repo == "" {
				repo = strings.SplitN(a.Properties.SourceImage, ":", 2)[0]
			}
			parts := strings.SplitN(a.Properties.SourceImage, ":", 2)
			if len(parts) == 2 {
				bareRefs[a.Properties.SourceImage] = a.Properties.Registry + "/" + repo + ":" + parts[1]
			}
		}
	}

	intentsDir := filepath.Join(partitionDir, "intents")
	entries, _ := os.ReadDir(intentsDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") || e.Name() == "images.yaml" {
			continue
		}
		filePath := filepath.Join(intentsDir, e.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}
		orig := string(content)
		changed := false
		result := re.ReplaceAllStringFunc(orig, func(match string) string {
			parts := re.FindStringSubmatch(match)
			if len(parts) < 2 {
				return match
			}
			assetName := parts[1]
			if ref, ok := refs[assetName]; ok {
				changed = true
				return ref
			}
			// Try matching without "-build" suffix
			for name, ref := range refs {
				if strings.TrimSuffix(assetName, "-build") == strings.TrimSuffix(name, "-build") {
					changed = true
					return ref
				}
			}
			return match
		})

		// Second pass: repair already-hardcoded bare refs (from previous buggy releases).
		// Only replace when the bare ref appears as an image value, not as a
		// substring inside an already-prefixed ref (e.g. skip "registry/foo:tag"
		// when we're repairing "foo:tag" → "registry/foo:tag").
		for bare, full := range bareRefs {
			if !strings.Contains(result, bare) {
				continue
			}
			// Match "image: <bare>" only — not substrings inside longer paths
			reBare := regexp.MustCompile(`(image:\s*)` + regexp.QuoteMeta(bare) + `\b`)
			newResult := reBare.ReplaceAllString(result, "${1}"+full)
			if newResult != result {
				result = newResult
				changed = true
			}
		}

		if changed {
			fmt.Fprintf(os.Stderr, "  stamp %s\n", e.Name())
			if !dryRun {
				os.WriteFile(filePath, []byte(result), 0644)
			}
		}
	}
	return nil
}

// releaseImageSpec holds the build parameters for a single image.
type releaseImageSpec struct {
	dockerfile    string
	context       string
	buildArgs     []string
	buildContexts map[string]string
	noCache       bool
}

// releaseImageSpecs returns the build specs for a partition's images.
func releaseImageSpecs(partitionDir string) map[string]releaseImageSpec {
	root := findRootDir()
	if root == "." {
		return nil
	}
	strataRoot := filepath.Dir(root) // ~/strata

	monofsArgs := func() []string {
		sha, _ := bootstrap.RunCapture(context.Background(), "git", "-C", filepath.Join(strataRoot, "monofs"), "rev-parse", "--short", "HEAD")
		return []string{"VERSION=dev", "COMMIT=" + sha, "BUILD_TIME=" + timeNowUTC()}
	}

	mkBldCtx := func(names ...string) map[string]string {
		m := make(map[string]string)
		for _, n := range names {
			m[n] = filepath.Join(strataRoot, n)
		}
		return m
	}

	partName := filepath.Base(partitionDir)
	switch partName {
	case "guardian-configs":
		return map[string]releaseImageSpec{
			"guardian":                 {"Dockerfile", filepath.Join(strataRoot, "guardian"), monofsArgs(), mkBldCtx("monofs", "kvs"), false},
			"guardian-pusher-k8s":      {"Dockerfile.pusher-k8s", filepath.Join(strataRoot, "guardian"), nil, mkBldCtx("monofs", "kvs"), false},
			"guardian-pusher-aws":      {"Dockerfile.pusher-aws", filepath.Join(strataRoot, "guardian"), nil, mkBldCtx("monofs", "kvs"), false},
			"guardian-pusher-docker":   {"Dockerfile.pusher-docker", filepath.Join(strataRoot, "guardian"), nil, mkBldCtx("monofs", "kvs"), false},
			"lb":                       {"Dockerfile", filepath.Join(strataRoot, "lb"), nil, nil, false},
		}
	case "k8s-top":
		return map[string]releaseImageSpec{
			"k8s-top": {"k8s-top/Dockerfile", strataRoot, nil, nil, false},
		}
	case "agent":
		return map[string]releaseImageSpec{
			"lagent-llm":      {"Dockerfile", filepath.Join(strataRoot, "agent", "llm"), nil, nil, false},
			"lagent-backend":  {"Dockerfile", filepath.Join(strataRoot, "agent", "backend"), nil, nil, false},
			"lagent-frontend": {"Dockerfile", filepath.Join(strataRoot, "agent", "frontend"), nil, nil, false},
		}
	case "doctor":
		return map[string]releaseImageSpec{
			"doctor-ingest": {"Dockerfile", filepath.Join(strataRoot, "doctor"), []string{"DOCTOR_SERVICE=doctor-ingest"}, mkBldCtx("monofs"), true},
			"doctor-query":  {"Dockerfile", filepath.Join(strataRoot, "doctor"), []string{"DOCTOR_SERVICE=doctor-query"}, mkBldCtx("monofs"), true},
			"lb":            {"Dockerfile", filepath.Join(strataRoot, "lb"), nil, nil, false},
		}
	case "dev-workspace", "monitoring":
		return map[string]releaseImageSpec{
			"lb": {"Dockerfile", filepath.Join(strataRoot, "lb"), nil, nil, false},
		}
	case "opentelemetry", "lb-agent":
		// These are handled by IMAGEBUILD_PREPARE_RECIPES (stage sources) — skip direct build
		return nil
	}
	return nil
}

func timeNowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

func kindClusterName() string {
	n := os.Getenv("KIND_CLUSTER_NAME")
	if n == "" {
		n = "strata"
	}
	return n
}

func findPartitionDir(name string) string {
	root := findRootDir()
	if root != "." {
		dir := filepath.Join(root, "partitions", name)
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
	}
	// Fallback: walk up from CWD looking for partitions/<name>
	cwd, _ := os.Getwd()
	for dir := cwd; ; {
		for _, pattern := range []string{
			filepath.Join(dir, "partitions", name),
			filepath.Join(dir, "stratatools", "partitions", name),
			filepath.Join(dir, "strata", "stratatools", "partitions", name),
			filepath.Join(dir, "projects", "stratatools", "partitions", name),
			filepath.Join(dir, "src", "stratatools", "partitions", name),
		} {
			if _, err := os.Stat(pattern); err == nil {
				return pattern
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func partitionNameFromDir(dir string) string {
	if dir == "" {
		return ""
	}
	return filepath.Base(dir)
}

// storeFlags returns guardianctl flags for MonoFS connectivity.
func storeFlags(router, token string) []string {
	var args []string
	if router != "" {
		args = append(args, "--monofs-router", router)
	}
	if token != "" {
		args = append(args, "--monofs-token", token)
	}
	return args
}

// patchImageSources replaces :latest with :<tag> in sourceImage fields of the
// partition's images intent, so st-image builds with versioned tags.
func patchImageSources(partitionDir, tag string, dryRun bool) error {
	imagesPath := filepath.Join(partitionDir, "intents", "images.yaml")
	data, err := os.ReadFile(imagesPath)
	if err != nil {
		return nil // no images.yaml, nothing to patch
	}
	lines := strings.Split(string(data), "\n")
	changed := false
	for i, line := range lines {
		const key = "sourceImage: "
		idx := strings.Index(line, key)
		if idx < 0 {
			continue
		}
		image := strings.TrimSpace(line[idx+len(key):])
		// Only patch :latest references
		if !strings.HasSuffix(image, ":latest") {
			continue
		}
		newImage := strings.TrimSuffix(image, ":latest") + ":" + tag
		lines[i] = line[:idx] + key + newImage
		changed = true
	}
	if !changed {
		return nil
	}
	if dryRun {
		fmt.Fprintf(os.Stderr, "  would patch sourceImage tags in %s\n", imagesPath)
		return nil
	}
	return os.WriteFile(imagesPath, []byte(strings.Join(lines, "\n")), 0644)
}

// Ensure release/image commands are registered.
var _ = func() bool {
	// Registration happens in releaseCommands()
	return true
}()
