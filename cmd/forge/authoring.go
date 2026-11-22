package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	"github.com/radryc/guardian/internal/forge/author"
	"github.com/radryc/guardian/internal/forge/repoinit"
	"github.com/radryc/guardian/internal/forge/workspace"
	"gopkg.in/yaml.v3"
)

func partitionNewCommand() *command.Command {
	flags := flag.NewFlagSet("partition new", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	name := flags.String("name", "", "partition name")
	partitionsDir := flags.String("partitions-dir", "", "directory for partition folders (default <root>/partitions)")
	policy := flags.String("deletion-policy", "orphan", "orphan|destroy")
	interval := flags.String("interval", "30s", "reconciliation interval")
	repoName := flags.String("repo", "", "repository name for .guardian/config.yaml (default: directory name)")
	source := flags.String("source", "", "repository clone URL for .guardian/config.yaml")
	branch := flags.String("branch", "", "repository default branch for .guardian/config.yaml")
	bind := flags.Bool("bind", true, "register the partition in .guardian/config.yaml")
	force := flags.Bool("force", false, "overwrite an existing config.yaml")
	return &command.Command{Description: "Create a partition config", Flags: flags, Run: func(_ context.Context, _ []string) error {
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		path, err := author.CreatePartition(author.PartitionOptions{
			Root: *root, Name: *name, PartitionsDir: *partitionsDir,
			DeletionPolicy: *policy, Interval: *interval, Force: *force,
		})
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", path)
		if *bind {
			configPath, err := repoinit.EnsureBinding(repoinit.BindingOptions{
				Root: *root, RepoName: *repoName, Source: *source, Branch: *branch,
				Partition: *name, PartitionPath: filepath.ToSlash(filepath.Join("partitions", *name)),
			})
			if err != nil {
				return err
			}
			fmt.Printf("registered %s\n", configPath)
		}
		return nil
	}}
}

func intentNewCommand() *command.Command {
	flags := flag.NewFlagSet("intent new", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	name := flags.String("name", "", "intent name")
	pusher := flags.String("pusher", "", "target pusher, for example k8s-main")
	cluster := flags.String("cluster", "", "target cluster")
	namespace := flags.String("namespace", "", "target namespace")
	force := flags.Bool("force", false, "overwrite an existing intent")
	return &command.Command{Description: "Create an intent", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		path, err := author.CreateIntent(author.IntentOptions{
			PartitionDir: dir, Name: *name, Pusher: *pusher,
			Cluster: *cluster, Namespace: *namespace, Force: *force,
		})
		if err != nil {
			return err
		}
		fmt.Printf("created %s\n", path)
		return nil
	}}
}

func assetAddCommand() *command.Command {
	flags := flag.NewFlagSet("asset add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	assetType := flags.String("type", "Compute", "asset type")
	name := flags.String("name", "", "asset name")
	propertiesJSON := flags.String("properties", "", "asset properties as a JSON or YAML object")
	propertiesFile := flags.String("properties-file", "", "path to a JSON or YAML file with asset properties")
	image := flags.String("image", "", "container image (Compute)")
	replicas := flags.Int("replicas", 0, "replica count (Compute)")
	port := flags.Int("port", 0, "container port (Compute)")
	content := flags.String("content", "", "inline content (Config)")
	secretRef := flags.String("ref", "", "secret reference (Secret)")
	return &command.Command{Description: "Append an asset to an intent", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if strings.TrimSpace(*name) == "" {
			return fmt.Errorf("--name is required")
		}
		properties := map[string]any{}
		if *propertiesFile != "" && *propertiesJSON != "" {
			return fmt.Errorf("pass either --properties or --properties-file, not both")
		}
		if *propertiesFile != "" {
			data, err := os.ReadFile(*propertiesFile)
			if err != nil {
				return err
			}
			if err := yaml.Unmarshal(data, &properties); err != nil {
				return fmt.Errorf("parse %s: %w", *propertiesFile, err)
			}
		}
		if *propertiesJSON != "" {
			if err := yaml.Unmarshal([]byte(*propertiesJSON), &properties); err != nil {
				return fmt.Errorf("parse --properties: %w", err)
			}
		}
		switch *assetType {
		case "Compute":
			if *image != "" {
				properties["image"] = *image
			}
			if imageValue, _ := properties["image"].(string); strings.TrimSpace(imageValue) == "" {
				return fmt.Errorf("--image (or properties.image) is required for a Compute asset")
			}
			if *replicas > 0 {
				properties["replicas"] = *replicas
			}
			if *port > 0 {
				if _, ok := properties["ports"]; !ok {
					properties["ports"] = []map[string]any{{
						"name": "http", "containerPort": *port, "protocol": "TCP",
					}}
				}
			}
		case "Config":
			if *content != "" {
				properties["content"] = *content
			}
		case "Secret":
			if *secretRef != "" {
				properties["secretRef"] = *secretRef
			}
		}
		path, err := author.AddAsset(dir, *intent, assetdomain.Spec{
			Type: *assetType, Name: *name, Properties: properties,
		})
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}

func assetConnectCommand() *command.Command {
	flags := flag.NewFlagSet("asset connect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	from := flags.String("from", "", "dependent asset")
	to := flags.String("to", "", "dependency asset")
	return &command.Command{Description: "Add a dependsOn edge between assets", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if *from == "" || *to == "" {
			return fmt.Errorf("--from and --to are required")
		}
		path, err := author.ConnectAssets(dir, *intent, *from, *to)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}

func probeAddCommand() *command.Command {
	flags := flag.NewFlagSet("probe add", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	name := flags.String("name", "", "probe asset name (default <target>-probe)")
	target := flags.String("asset", "", "target asset to probe")
	url := flags.String("url", "", "explicit URL to probe")
	address := flags.String("address", "", "explicit host:port or hostname")
	probeType := flags.String("type", "http", "probe type: http|https|tcp|grpc|dns|tls")
	path := flags.String("path", "", "HTTP path")
	method := flags.String("method", "", "HTTP method")
	port := flags.Int("port", 0, "port")
	interval := flags.String("interval", "30s", "probe interval")
	timeout := flags.String("timeout", "", "per-step timeout, for example 3s")
	retries := flags.Int("retries", -1, "retries before reporting down")
	headers := flags.String("headers", "", "request headers as a JSON object")
	expectStatus := flags.String("expect-status", "", "comma-separated accepted HTTP status codes")
	steps := flags.String("steps", "", "ordered synthetic journey steps as JSON")
	stepsFile := flags.String("steps-file", "", "path to a JSON or YAML journey steps list")
	propertiesJSON := flags.String("properties", "", "additional probe properties as a JSON or YAML object")
	insecure := flags.Bool("insecure-skip-verify", false, "disable TLS certificate verification")
	return &command.Command{Description: "Add a Probe asset, including a multi-step journey", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		if *target == "" && *url == "" {
			return fmt.Errorf("pass --asset or --url")
		}
		probeName := *name
		if probeName == "" {
			base := *target
			if base == "" {
				base = "http"
			}
			probeName = base + "-probe"
		}
		properties := map[string]any{}
		if *propertiesJSON != "" {
			if err := yaml.Unmarshal([]byte(*propertiesJSON), &properties); err != nil {
				return fmt.Errorf("parse --properties: %w", err)
			}
		}
		properties["type"] = *probeType
		if *url != "" {
			properties["url"] = *url
		} else {
			properties["target"] = *target
		}
		if *address != "" {
			properties["address"] = *address
		}
		if *path != "" {
			properties["path"] = *path
		}
		if *method != "" {
			properties["method"] = *method
		}
		if *port > 0 {
			properties["port"] = *port
		}
		if *interval != "" {
			properties["interval"] = *interval
		}
		if *timeout != "" {
			properties["timeout"] = *timeout
		}
		if *retries >= 0 {
			properties["retries"] = *retries
		}
		if *insecure {
			properties["insecureSkipVerify"] = true
		}
		if *headers != "" {
			var parsed map[string]any
			if err := yaml.Unmarshal([]byte(*headers), &parsed); err != nil {
				return fmt.Errorf("parse --headers: %w", err)
			}
			properties["headers"] = parsed
		}
		if *expectStatus != "" {
			statuses := []int{}
			for _, item := range strings.Split(*expectStatus, ",") {
				item = strings.TrimSpace(item)
				if item == "" {
					continue
				}
				status, err := strconv.Atoi(item)
				if err != nil {
					return fmt.Errorf("invalid --expect-status %q", item)
				}
				statuses = append(statuses, status)
			}
			if len(statuses) > 0 {
				properties["expectStatus"] = statuses
			}
		}
		if *steps != "" && *stepsFile != "" {
			return fmt.Errorf("pass either --steps or --steps-file, not both")
		}
		var journeySteps any
		if *stepsFile != "" {
			data, err := os.ReadFile(*stepsFile)
			if err != nil {
				return err
			}
			if err := yaml.Unmarshal(data, &journeySteps); err != nil {
				return fmt.Errorf("parse %s: %w", *stepsFile, err)
			}
		} else if *steps != "" {
			if err := yaml.Unmarshal([]byte(*steps), &journeySteps); err != nil {
				return fmt.Errorf("parse --steps: %w", err)
			}
		}
		if journeySteps != nil {
			properties["steps"] = journeySteps
		}
		assetPath, err := author.AddProbe(dir, *intent, assetdomain.Spec{
			Type: "Probe", Name: probeName, Properties: properties,
		})
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", assetPath)
		return nil
	}}
}

func secretRefCommand() *command.Command {
	flags := flag.NewFlagSet("secret ref", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	intent := flags.String("intent", "", "intent name")
	asset := flags.String("asset", "", "Compute asset name")
	env := flags.String("env", "", "environment variable")
	ref := flags.String("ref", "", "secret reference, for example monofs-secret://checkout/db")
	return &command.Command{Description: "Point an env var at a secret reference", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		path, err := author.AddSecretRef(dir, *intent, *asset, *env, *ref)
		if err != nil {
			return err
		}
		fmt.Printf("updated %s\n", path)
		return nil
	}}
}

func alertSuggestCommand() *command.Command {
	flags := flag.NewFlagSet("alert suggest", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name")
	write := flags.Bool("write", false, "write alerts/suggested-health.yaml")
	force := flags.Bool("force", false, "overwrite an existing suggested file")
	return &command.Command{Description: "Generate a starter alert group from the partition", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		dir, err := resolvePartitionDir(ctx, *root, *partition)
		if err != nil {
			return err
		}
		data, err := author.SuggestAlerts(dir)
		if err != nil {
			return err
		}
		if !*write {
			_, err := os.Stdout.Write(data)
			return err
		}
		path := filepath.Join(dir, "alerts", "suggested-health.yaml")
		if _, err := os.Stat(path); err == nil && !*force {
			return fmt.Errorf("%s already exists (use --force)", path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
		return nil
	}}
}

func resolvePartitionDir(ctx context.Context, root, partition string) (string, error) {
	if strings.TrimSpace(partition) == "" {
		return "", fmt.Errorf("--partition is required")
	}
	refs, err := workspace.Discover(root)
	if err != nil {
		return "", err
	}
	for _, ref := range refs {
		if ref.Name == partition {
			return ref.Dir, nil
		}
	}
	return "", fmt.Errorf("partition %q not found under %s", partition, root)
}
