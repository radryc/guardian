package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/policy"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/forge/validate"
	"github.com/radryc/guardian/internal/forge/workspace"
	"github.com/radryc/guardian/internal/repoconfig"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
}

func doctorCommand() *command.Command {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	repoURL := flags.String("repo-url", "", "repository URL (default from .guardian/config.yaml)")
	baseBranch := flags.String("base-branch", "", "protected branch (default from config or main)")
	githubToken := flags.String("github-token", firstEnv("MONOFS_GITHUB_TOKEN", "GITHUB_TOKEN"), "GitHub token for branch-protection checks")
	strict := flags.Bool("strict", false, "treat warnings as failures")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	return &command.Command{Description: "Check that the repository and pipeline gates are configured", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		outFormat, err := parseFormat(*format)
		if err != nil {
			return err
		}
		checks := runDoctor(ctx, *root, *repoURL, *baseBranch, *githubToken)
		failures, warnings := 0, 0
		for _, check := range checks {
			switch check.Status {
			case "fail":
				failures++
			case "warn":
				warnings++
			}
		}
		printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
		if outFormat == cliformat.FormatJSON {
			printer.PrintJSON(map[string]any{
				"success": failures == 0 && (!*strict || warnings == 0),
				"checks":  checks,
			})
		} else {
			rows := make([][]string, 0, len(checks))
			for _, check := range checks {
				rows = append(rows, []string{check.Status, check.Name, check.Detail})
			}
			printer.PrintTable([]string{"STATUS", "CHECK", "DETAIL"}, rows)
			if failures > 0 || (*strict && warnings > 0) {
				printer.PrintText("failed: %d failure(s), %d warning(s)\n", failures, warnings)
			} else {
				printer.PrintText("ok: %d check(s) passed (%d warning(s))\n", len(checks)-warnings, warnings)
			}
		}
		if failures > 0 || (*strict && warnings > 0) {
			return fmt.Errorf("%d failure(s), %d warning(s)", failures, warnings)
		}
		return nil
	}}
}

func runDoctor(ctx context.Context, root, repoURL, baseBranch, githubToken string) []doctorCheck {
	var checks []doctorCheck
	add := func(name, status, detail string) {
		checks = append(checks, doctorCheck{Name: name, Status: status, Detail: detail})
	}

	if _, err := exec.LookPath("git"); err != nil {
		add("git", "fail", "git is not installed")
	} else {
		add("git", "ok", "found")
	}

	gitRoot, err := publish.GitRoot(ctx, root)
	if err != nil {
		add("repository", "fail", err.Error())
		gitRoot = root
	} else {
		add("repository", "ok", gitRoot)
	}

	cfg, _, found, cfgErr := repoconfig.Load(gitRoot)
	if cfgErr != nil {
		add("repo config", "fail", cfgErr.Error())
	} else if !found {
		add("repo config", "warn", "no .guardian/config.yaml (run forge repo init)")
	} else {
		add("repo config", "ok", ".guardian/config.yaml")
		if repoURL == "" {
			repoURL = cfg.Repo.Source
		}
		if baseBranch == "" {
			baseBranch = cfg.Repo.Branch
		}
	}
	if baseBranch == "" {
		baseBranch = "main"
	}

	refs, err := workspace.Discover(root)
	if err != nil {
		add("partitions", "fail", err.Error())
	} else {
		add("partitions", "ok", fmt.Sprintf("%d partition(s)", len(refs)))
	}

	result, err := validate.Run(ctx, validate.Options{Root: root})
	if err != nil {
		add("validation", "fail", fmt.Sprintf("%d error(s), %d warning(s)", result.Errors, result.Warnings))
	} else if result.Warnings > 0 {
		add("validation", "warn", fmt.Sprintf("%d warning(s)", result.Warnings))
	} else {
		add("validation", "ok", "clean")
	}

	pipelinePath := filepath.Join(gitRoot, ".monofs", "pipelines", "partition-pipeline.yml")
	if _, err := os.Stat(pipelinePath); err != nil {
		add("pipeline config", "warn", "missing .monofs/pipelines/partition-pipeline.yml")
	} else {
		add("pipeline config", "ok", pipelinePath)
	}

	if _, found, err := policy.Load(policy.PathFor(gitRoot)); err != nil {
		add("guardrail policy", "fail", err.Error())
	} else if !found {
		add("guardrail policy", "warn", "no .forge/policy.yaml")
	} else {
		add("guardrail policy", "ok", ".forge/policy.yaml")
	}

	for _, tool := range []string{"docker", "kubectl"} {
		if _, err := exec.LookPath(tool); err != nil {
			add(tool, "warn", "not installed")
		} else {
			add(tool, "ok", "found")
		}
	}

	if repoURL == "" {
		add("branch protection", "warn", "no repository URL configured")
		return checks
	}
	if githubToken == "" {
		add("branch protection", "warn", "not checked (no GitHub token)")
		return checks
	}
	status, detail := checkBranchProtection(ctx, githubToken, repoURL, baseBranch)
	add("branch protection", status, detail)
	return checks
}

func checkBranchProtection(ctx context.Context, token, repoURL, branch string) (string, string) {
	display := publish.DisplayPathFromURL(repoURL)
	parts := strings.Split(display, "/")
	if len(parts) < 3 || parts[0] != "github.com" {
		return "warn", "branch-protection check supports GitHub URLs only"
	}
	owner, repo := parts[1], parts[2]
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/branches/%s/protection", owner, repo, branch)
	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "fail", err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return "fail", err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "fail", fmt.Sprintf("branch protection is not configured on %s", branch)
	}
	if resp.StatusCode >= 300 {
		return "fail", fmt.Sprintf("GitHub API returned %d", resp.StatusCode)
	}
	var body struct {
		RequiredStatusChecks *struct {
			Contexts []string `json:"contexts"`
		} `json:"required_status_checks"`
		RequiredPullRequestReviews *struct{} `json:"required_pull_request_reviews"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "fail", err.Error()
	}
	if body.RequiredStatusChecks == nil {
		return "fail", "required status checks are not configured"
	}
	found := false
	for _, context := range body.RequiredStatusChecks.Contexts {
		if context == "monofs/partition-pipeline" {
			found = true
		}
	}
	if !found {
		return "fail", "required checks must include monofs/partition-pipeline"
	}
	if body.RequiredPullRequestReviews == nil {
		return "warn", "required check is configured; no required review"
	}
	return "ok", "monofs/partition-pipeline is required"
}
