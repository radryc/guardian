package main

import (
	"context"
	"errors"
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
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
	pb "github.com/radryc/monofs/api/proto"
)

func statusCommand() *command.Command {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", ".", "repository root")
	partition := flags.String("partition", "", "filter by partition")
	format := flags.String("format", string(cliformat.FormatText), "output format: text|json")
	live := flags.Bool("live", false, "refresh job state from MonoFS")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")

	return &command.Command{
		Description: "Show submitted proposals and their delivery chain",
		Flags:       flags,
		Run: func(ctx context.Context, _ []string) error {
			outFormat, err := parseFormat(*format)
			if err != nil {
				return err
			}
			base := *root
			if gitRoot, err := publish.GitRoot(ctx, *root); err == nil {
				base = gitRoot
			}
			proposals, err := publish.LoadProposals(base)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
					if outFormat == cliformat.FormatJSON {
						printer.PrintJSON(map[string]any{"proposals": []publish.Proposal{}})
					} else {
						printer.PrintText("no submissions recorded under %s\n", base)
					}
					return nil
				}
				return err
			}

			if wanted := strings.TrimSpace(*partition); wanted != "" {
				filtered := make([]publish.Proposal, 0, len(proposals))
				for _, proposal := range proposals {
					if proposal.Partition == wanted {
						filtered = append(filtered, proposal)
					}
				}
				proposals = filtered
			}

			if *live {
				if err := refreshLive(ctx, *routerAddr, *token, *principal, proposals); err != nil {
					return err
				}
			}

			printer := &output.Printer{Format: outFormat, Writer: os.Stdout}
			if outFormat == cliformat.FormatJSON {
				printer.PrintJSON(map[string]any{"proposals": proposals})
				return nil
			}
			if len(proposals) == 0 {
				printer.PrintText("no submissions recorded\n")
				return nil
			}
			rows := make([][]string, 0, len(proposals))
			for _, proposal := range proposals {
				state := proposal.JobState
				if state == "" {
					state = proposal.Status
				}
				if state == "" {
					state = "submitted"
				}
				rows = append(rows, []string{
					proposal.Partition,
					shortID(proposal.JobID),
					state,
					proposal.TargetBranch,
					proposal.PullRequestURL,
					proposal.SubmittedAt.Local().Format(time.RFC3339),
				})
			}
			printer.PrintTable([]string{"PARTITION", "JOB", "STATE", "BRANCH", "PULL REQUEST", "SUBMITTED"}, rows)
			return nil
		},
	}
}

func refreshLive(ctx context.Context, routerAddr, token, principal string, proposals []publish.Proposal) error {
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("--live requires a MonoFS token: pass --monofs-token or set MONOFS_TOKEN")
	}
	_, client, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
		RouterAddr:     routerAddr,
		Token:          token,
		PrincipalID:    principal,
		Role:           "cli",
		ClientIDPrefix: "forge-status",
		Version:        "forge",
		RPCTimeout:     15 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("connect to MonoFS: %w", err)
	}
	for i := range proposals {
		jobID := strings.TrimSpace(proposals[i].JobID)
		if jobID == "" {
			continue
		}
		job, err := client.GetWorkspaceSyncJob(ctx, &pb.GetWorkspaceSyncJobRequest{JobId: jobID})
		if err != nil {
			continue
		}
		proposals[i].JobState = job.GetState().String()
		if errMessage := strings.TrimSpace(job.GetErrorMessage()); errMessage != "" {
			proposals[i].Message = errMessage
		}
	}
	return nil
}

func shortID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 12 {
		return value
	}
	return value[:12] + "…"
}
