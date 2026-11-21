package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/forge/api"
	"github.com/radryc/guardian/internal/forge/publish"
	"github.com/radryc/guardian/internal/repoconfig"
)

func uiCommand() *command.Command {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	listen := flags.String("listen", "127.0.0.1:0", "listen address (loopback only by default)")
	noOpen := flags.Bool("no-open", false, "do not open a browser")
	token := flags.String("token", "", "fixed session token (default random)")
	repoURL := flags.String("repo-url", "", "repository clone URL (default from .guardian/config.yaml)")
	baseBranch := flags.String("base-branch", "", "target branch")
	workspaceID := flags.String("workspace", "forge", "MonoFS workspace id")
	storageID := flags.String("storage-id", "", "repository storage id")
	displayPath := flags.String("display-path", "", "repository display path")
	includeSecrets := flags.Bool("include-secrets", false, "include files under secrets/ in submissions")
	authorName := flags.String("author-name", "", "commit author name")
	authorEmail := flags.String("author-email", "", "commit author email")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	monofsToken := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")

	return &command.Command{
		Description: "Open the local Forge workbench in a browser",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			sessionToken := strings.TrimSpace(*token)
			if sessionToken == "" {
				sessionToken = randomToken()
			}

			gitRoot := *root
			if detected, err := publish.GitRoot(ctx, *root); err == nil {
				gitRoot = detected
			}
			cfg, _, _, cfgErr := repoconfig.Load(gitRoot)
			if cfgErr != nil {
				return cfgErr
			}
			displayKeys := map[string]string{}
			if cfg != nil {
				for _, binding := range cfg.Repo.Bindings {
					if binding.Display == nil {
						continue
					}
					for canonical, display := range binding.Display.Keys {
						displayKeys[canonical] = display
					}
				}
			}

			resolvedRepoURL := strings.TrimSpace(*repoURL)
			if resolvedRepoURL == "" && cfg != nil {
				resolvedRepoURL = strings.TrimSpace(cfg.Repo.Source)
			}
			resolvedBranch := strings.TrimSpace(*baseBranch)
			if resolvedBranch == "" && cfg != nil {
				resolvedBranch = strings.TrimSpace(cfg.Repo.Branch)
			}
			if resolvedBranch == "" {
				resolvedBranch = "main"
			}
			resolvedDisplay := strings.TrimSpace(*displayPath)
			if resolvedDisplay == "" && resolvedRepoURL != "" {
				resolvedDisplay = publish.DisplayPathFromURL(resolvedRepoURL)
			}
			resolvedStorage := strings.TrimSpace(*storageID)
			if resolvedStorage == "" && resolvedRepoURL != "" {
				resolvedStorage = publish.StorageIDForURL(resolvedRepoURL)
			}
			name := strings.TrimSpace(*authorName)
			email := strings.TrimSpace(*authorEmail)
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

			submit := func(submitCtx context.Context, req publish.Request) (publish.Result, error) {
				req.WorkspaceID = *workspaceID
				req.StorageID = resolvedStorage
				req.DisplayPath = resolvedDisplay
				req.RepoURL = resolvedRepoURL
				req.BaseBranch = resolvedBranch
				req.AuthorName = name
				req.AuthorEmail = email
				req.IncludeSecrets = *includeSecrets
				var publisher publish.Publisher
				if !req.DryRun {
					if resolvedRepoURL == "" {
						return publish.Result{}, fmt.Errorf("repository URL is required: pass --repo-url or set repo.source in .guardian/config.yaml")
					}
					var err error
					publisher, err = openPublisher(submitCtx, *routerAddr, *monofsToken, *principal)
					if err != nil {
						return publish.Result{}, err
					}
				}
				return publish.Submit(submitCtx, req, publisher)
			}

			server, err := api.New(api.Options{
				Root: *root, Token: sessionToken, Submit: submit, DisplayKeys: displayKeys,
			})
			if err != nil {
				return err
			}
			addr, err := server.Serve(ctx, *listen)
			if err != nil {
				return err
			}
			url := fmt.Sprintf("http://%s/?token=%s", addr, sessionToken)
			fmt.Printf("Forge UI listening on %s\n", url)
			if !strings.HasPrefix(addr, "127.0.0.1") && !strings.HasPrefix(addr, "[::1]") {
				fmt.Fprintln(os.Stderr, "warning: the UI is not bound to loopback; the session token is the only access control")
			}
			if !*noOpen {
				openBrowser(url)
			}
			<-ctx.Done()
			return nil
		},
	}
}

func randomToken() string {
	var buf [24]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("forge: generate session token: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
