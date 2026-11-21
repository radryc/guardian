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
	monofsstore "github.com/radryc/guardian/internal/store/monofs"
	"github.com/radryc/guardian/internal/versioning/revisions"
	"github.com/radryc/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

type pipelineDocument struct {
	Name string         `yaml:"name"`
	Jobs map[string]any `yaml:"jobs"`
}

func pipelineInstallCommand() *command.Command {
	flags := flag.NewFlagSet("pipeline install", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	file := flags.String("file", ".monofs/pipelines/partition-pipeline.yml", "pipeline config file")
	name := flags.String("name", "", "pipeline name (default from the file)")
	apply := flags.Bool("apply", false, "install the pipeline into MonoFS /.pipelines/")
	routerAddr := flags.String("monofs-router", firstEnv("MONOFS_ROUTER", "127.0.0.1:9090"), "MonoFS router gRPC address")
	token := flags.String("monofs-token", os.Getenv("MONOFS_TOKEN"), "MonoFS token")
	principal := flags.String("monofs-principal-id", "forge", "MonoFS principal id")
	return &command.Command{Description: "Validate and install the MonoFS partition pipeline", Flags: flags, Run: func(ctx context.Context, _ []string) error {
		data, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		var doc pipelineDocument
		if err := yaml.Unmarshal(data, &doc); err != nil {
			return fmt.Errorf("parse pipeline: %w", err)
		}
		if strings.TrimSpace(doc.Name) == "" {
			return fmt.Errorf("pipeline name is required")
		}
		if len(doc.Jobs) == 0 {
			return fmt.Errorf("pipeline must define at least one job")
		}
		pipelineName := strings.TrimSpace(*name)
		if pipelineName == "" {
			pipelineName = doc.Name
		}
		target := "/.pipelines/" + pipelineName + ".yml"
		if !*apply {
			fmt.Printf("pipeline %s is valid\n", pipelineName)
			fmt.Printf("target: %s\n", target)
			fmt.Println("this is a platform-config change; re-run with --apply to install it into MonoFS")
			return nil
		}
		if strings.TrimSpace(*token) == "" {
			return fmt.Errorf("--apply requires a MonoFS token: pass --monofs-token or set MONOFS_TOKEN")
		}
		store, _, err := monofsstore.Open(ctx, monofsstore.OpenConfig{
			RouterAddr: *routerAddr, Token: *token, PrincipalID: *principal,
			Role: "cli", ClientIDPrefix: "forge-pipeline", Version: "forge",
			Writable: true, RPCTimeout: 30 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("connect to MonoFS: %w", err)
		}
		correlationID := revisions.NewCorrelationID()
		batch, err := store.UpsertFiles(ctx, guardianapi.MutationBatch{
			Writes: []guardianapi.PathWrite{{LogicalPath: target, Content: data}},
			Context: guardianapi.MutationContext{
				PrincipalID:   *principal,
				Reason:        "install forge partition pipeline",
				CorrelationID: correlationID,
			},
		})
		if err != nil {
			return err
		}
		fmt.Printf("installed %s revision=%s correlation=%s\n", target, batch.BatchRevisionID, correlationID)
		return nil
	}}
}
