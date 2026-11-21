package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/radryc/guardian/internal/cli/command"
	cliformat "github.com/radryc/guardian/internal/cli/format"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/forge/workspace"
	"github.com/radryc/guardian/internal/repoconfig"
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
)

type submitOptions struct {
	Root           string
	Partition      string
	Message        string
	Title          string
	Body           string
	BodyFile       string
	Base           string
	BaseCommit     string
	BaseBranch     string
	WorkspaceID    string
	StorageID      string
	DisplayPath    string
	RepoURL        string
	AuthorName     string
	AuthorEmail    string
	All            bool
	IncludeSecrets bool
	DryRun         bool
	Format         string
	RouterAddr     string
	Token          string
	Principal      string
}

func submitCommand() *command.Command {
	flags := flag.NewFlagSet("submit", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "partition name (required when the root has several)")
	message := flags.String("message", "", "commit and pull request title")
	title := flags.String("title", "", "pull request title override")
	body := flags.String("body", "", "pull request body")
	bodyFile := flags.String("body-file", "", "read the pull request body from a file (- for stdin)")
	base := flags.String("base", "", "git ref to diff against (default origin/main, main, master, or HEAD)")
	baseCommit := flags.String("base-commit", "", "expected remote head; guards non-fast-forward")
	baseBranch := flags.String("base-branch", "", "target branch (default from .guardian/config.yaml or main)")
	workspaceID := flags.String("workspace", "forge", "MonoFS workspace id used in the review branch name")
	storageID := flags.String("storage-id", "", "repository storage id (default derived from the url)")
	displayPath := flags.String("display-path", "", "repository display path (default derived from the url)")
	repoURL := flags.String("repo-url", "", "repository clone url (default from .guardian/config.yaml)")
	authorName := flags.String("author-name", "", "commit author name (default git config)")
	authorEmail := flags.String("author-email", "", "commit author email (default git config)")
	all := flags.Bool("all", false, "submit every file in the partition instead of only changed files")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/")
	dryRun := flags.Bool("dry-run", false, "print operations without publishing")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")

	return &command.Command{
		Description: "Validate and submit a partition change as a reviewed PR/MR through MonoFS",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			outFormat, err := parseFormat(*format)
			if err != nil {
				return err
			}
			result, runErr := executeSubmit(ctx, submitOptions{
				Root: *root, Partition: *partition, Message: *message, Title: *title,
				Body: *body, BodyFile: *bodyFile, Base: *base, BaseCommit: *baseCommit,
				BaseBranch: *baseBranch, WorkspaceID: *workspaceID, StorageID: *storageID,
				DisplayPath: *displayPath, RepoURL: *repoURL, AuthorName: *authorName,
				AuthorEmail: *authorEmail, All: *all, IncludeSecrets: *includeSecrets,
				DryRun: *dryRun, Format: *format, RouterAddr: *routerAddr,
				Token: *token, Principal: *principal,
			})
			printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
			if outFormat == cliformat.FormatJSON {
				printer.PrintJSON(result)
			} else {
				printSubmitResult(printer, result)
			}
			if result.Warning != "" {
				fmt.Fprintln(os.Stderr, "warning: "+result.Warning)
			}
			return runErr
		},
	}
}

func executeSubmit(ctx context.Context, opts submitOptions) (publish.Result, error) {
	bodyValue, err := resolveBody(opts.Body, opts.BodyFile)
	if err != nil {
		return publish.Result{}, err
	}
	partitionDir, partitionName, repoConfig, err := resolvePartition(ctx, opts.Root, opts.Partition)
	if err != nil {
		return publish.Result{}, err
	}
	gitRoot := opts.Root
	if detected, err := publish.GitRoot(ctx, opts.Root); err == nil {
		gitRoot = detected
	}

	repoURL := strings.TrimSpace(opts.RepoURL)
	if repoURL == "" && repoConfig != nil {
		repoURL = strings.TrimSpace(repoConfig.Repo.Source)
	}
	if repoURL == "" {
		return publish.Result{}, fmt.Errorf("repository url is required: pass --repo-url or set repo.source in .guardian/config.yaml")
	}
	branch := strings.TrimSpace(opts.BaseBranch)
	if branch == "" && repoConfig != nil {
		branch = strings.TrimSpace(repoConfig.Repo.Branch)
	}
	if branch == "" {
		branch = "main"
	}
	display := strings.TrimSpace(opts.DisplayPath)
	if display == "" {
		display = publish.DisplayPathFromURL(repoURL)
	}
	storage := strings.TrimSpace(opts.StorageID)
	if storage == "" {
		storage = publish.StorageIDForURL(repoURL)
	}
	name := strings.TrimSpace(opts.AuthorName)
	email := strings.TrimSpace(opts.AuthorEmail)
	if name == "" || email == "" {
		gitName, gitEmail := publish.GitAuthor(ctx, gitRoot)
		if name == "" {
			name = gitName
		}
		if email == "" {
			email = gitEmail
		}
	}
	if name == "" {
		name = "forge"
	}
	if email == "" {
		email = "forge@local"
	}

	req := publish.Request{
		Root:             opts.Root,
		PartitionDir:     partitionDir,
		Partition:        partitionName,
		IncludeSecrets:   opts.IncludeSecrets,
		Changed:          !opts.All,
		BaseRef:          opts.Base,
		WorkspaceID:      opts.WorkspaceID,
		StorageID:        storage,
		DisplayPath:      display,
		RepoURL:          repoURL,
		BaseBranch:       branch,
		BaseCommit:       opts.BaseCommit,
		CommitMessage:    opts.Message,
		PullRequestTitle: opts.Title,
		PullRequestBody:  bodyValue,
		AuthorName:       name,
		AuthorEmail:      email,
		DryRun:           opts.DryRun,
	}

	var publisher publish.Publisher
	if !opts.DryRun {
		publisher, err = openPublisher(ctx, opts.RouterAddr, opts.Token, opts.Principal)
		if err != nil {
			return publish.Result{}, err
		}
	}
	return publish.Submit(ctx, req, publisher)
}

func resolvePartition(ctx context.Context, root, wanted string) (string, string, *repoconfig.Config, error) {
	refs, err := workspace.Discover(root)
	if err != nil {
		return "", "", nil, err
	}
	var selected workspace.PartitionRef
	if strings.TrimSpace(wanted) == "" {
		if len(refs) != 1 {
			return "", "", nil, fmt.Errorf("%d partitions found; pass --partition", len(refs))
		}
		selected = refs[0]
	} else {
		found := false
		for _, ref := range refs {
			if ref.Name == wanted {
				selected = ref
				found = true
				break
			}
		}
		if !found {
			return "", "", nil, fmt.Errorf("partition %q not found under %s", wanted, root)
		}
	}

	configRoot := root
	if gitRoot, err := publish.GitRoot(ctx, root); err == nil {
		configRoot = gitRoot
	}
	cfg, _, _, cfgErr := repoconfig.Load(configRoot)
	if cfgErr != nil {
		return "", "", nil, cfgErr
	}
	return selected.Dir, selected.Name, cfg, nil
}

func openPublisher(ctx context.Context, routerAddr, token, principal string) (publish.Publisher, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("MonoFS token is required: pass --monofs-token or set MONOFS_TOKEN")
	}
	_, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
		RouterAddr:     routerAddr,
		Token:          token,
		PrincipalID:    principal,
		Role:           "cli",
		ClientIDPrefix: "forge",
		Version:        "forge",
		Writable:       true,
		RPCTimeout:     2 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("connect to MonoFS: %w", err)
	}
	return client.WorkspacePublisher(), nil
}

func resolveBody(body, bodyFile string) (string, error) {
	bodyFile = strings.TrimSpace(bodyFile)
	if bodyFile == "" {
		return body, nil
	}
	if strings.TrimSpace(body) != "" {
		return "", fmt.Errorf("pass either --body or --body-file, not both")
	}
	if bodyFile == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("read --body-file: %w", err)
	}
	return string(data), nil
}

func printSubmitResult(printer *output.Printer, result publish.Result) {
	if result.DryRun {
		printer.PrintText("dry run: %d operation(s) for partition %s\n", len(result.Operations), result.Partition)
		for _, op := range result.Operations {
			printer.PrintText("  %-6s %s\n", op.Kind, op.Path)
		}
		return
	}
	printer.PrintText("submitted %s (%d operation(s))\n", result.Partition, len(result.Operations))
	printer.PrintText("  repository: %s\n", result.DisplayPath)
	if result.TargetBranch != "" {
		printer.PrintText("  branch:     %s\n", result.TargetBranch)
	}
	if result.PushedCommit != "" {
		printer.PrintText("  commit:     %s\n", result.PushedCommit)
	}
	if result.JobID != "" {
		printer.PrintText("  job:        %s\n", result.JobID)
	}
	if result.Status != "" {
		printer.PrintText("  status:     %s\n", result.Status)
	}
	if result.PullRequestURL != "" {
		action := "open"
		if result.PullRequestCreated {
			action = "created"
		}
		printer.PrintText("  pull request %s: %s\n", action, result.PullRequestURL)
	}
	printer.PrintText("  merging deploys this change after the required checks pass.\n")
}

func firstEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}
