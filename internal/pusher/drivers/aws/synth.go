package awsdriver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// SynthTemplateFromFiles stages an in-memory CDK source tree, runs cdk synth and
// returns the synthesized CloudFormation template JSON for stackID. It is used
// by the observed API source-upload diff fallback when Guardian only has the
// CDK source (no prebuilt assembly).
func SynthTemplateFromFiles(ctx context.Context, stateDir string, files map[string]string, appCommand, packageManager, stackID string, contextValues map[string]string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("sourceFiles is required")
	}
	if strings.TrimSpace(stateDir) == "" {
		stateDir = os.TempDir()
	}
	workspace, err := os.MkdirTemp(stateDir, "guardian-aws-synth-*")
	if err != nil {
		return "", fmt.Errorf("create synth workspace: %w", err)
	}
	defer os.RemoveAll(workspace)

	relativePaths := make([]string, 0, len(files))
	for rel := range files {
		relativePaths = append(relativePaths, rel)
	}
	sort.Strings(relativePaths)
	for _, rel := range relativePaths {
		clean := path.Clean(strings.TrimSpace(rel))
		if clean == "" || clean == "." || strings.HasPrefix(clean, "..") || path.IsAbs(clean) {
			return "", fmt.Errorf("invalid source file path %q", rel)
		}
		dest := filepath.Join(workspace, filepath.FromSlash(clean))
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(dest, []byte(files[rel]), 0o644); err != nil {
			return "", err
		}
	}

	if strings.TrimSpace(appCommand) == "" {
		appCommand = "npx ts-node --prefer-ts-exts bin/app.ts"
	}
	if manager, args, err := packageManagerCommand(workspace, packageManager); err == nil && manager != "" {
		if err := runInDir(ctx, workspace, manager, args...); err != nil {
			return "", fmt.Errorf("%s %s: %w", manager, strings.Join(args, " "), err)
		}
	}

	outputDir := filepath.Join(workspace, "cdk.out")
	cdkArgs := []string{"cdk", "synth", "--app", appCommand, "--output", outputDir}
	if trimmed := strings.TrimSpace(stackID); trimmed != "" {
		cdkArgs = append(cdkArgs, trimmed)
	}
	contextKeys := make([]string, 0, len(contextValues))
	for key := range contextValues {
		contextKeys = append(contextKeys, key)
	}
	sort.Strings(contextKeys)
	for _, key := range contextKeys {
		cdkArgs = append(cdkArgs, "-c", key+"="+contextValues[key])
	}
	if err := runInDir(ctx, workspace, "npx", cdkArgs...); err != nil {
		return "", fmt.Errorf("cdk synth: %w", err)
	}

	templatePath := filepath.Join(outputDir, stackID+".template.json")
	if _, err := os.Stat(templatePath); err != nil {
		if resolved := templateFromManifest(outputDir, stackID); resolved != "" {
			templatePath = resolved
		}
	}
	content, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("read synthesized template for %q: %w", stackID, err)
	}
	return string(content), nil
}

func runInDir(ctx context.Context, dir, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir = dir
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func templateFromManifest(assemblyDir, stackName string) string {
	content, err := os.ReadFile(filepath.Join(assemblyDir, "manifest.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		Artifacts map[string]struct {
			Type       string `json:"type"`
			Properties struct {
				TemplateFile string `json:"templateFile"`
				StackName    string `json:"stackName"`
			} `json:"properties"`
		} `json:"artifacts"`
	}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return ""
	}
	for id, artifact := range manifest.Artifacts {
		if artifact.Type != "aws:cloudformation:stack" {
			continue
		}
		if artifact.Properties.StackName == stackName || (artifact.Properties.StackName == "" && id == stackName) {
			if artifact.Properties.TemplateFile != "" {
				return filepath.Join(assemblyDir, artifact.Properties.TemplateFile)
			}
		}
	}
	return ""
}
