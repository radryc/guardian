package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/gcpsetup"
)

func gcpCommands() []bootstrapReg {
	return []bootstrapReg{
		{Group: "gcp", Name: "setup", Cmd: gcpSetupCommand()},
		{Group: "gcp", Name: "init", Cmd: gcpInitCommand()},
	}
}

func gcpConfigFlags(name string) (*flag.FlagSet, *gcpsetup.Config) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(ioDiscard{})
	cfg := &gcpsetup.Config{}
	flags.StringVar(&cfg.Project, "project", envOr("GCP_PROJECT", ""), "GCP project (default: $GCP_PROJECT)")
	flags.StringVar(&cfg.Region, "region", envOr("GCP_REGION", "europe-west1"), "GCP region")
	flags.StringVar(&cfg.ServiceAccount, "service-account", envOr("GCP_DEPLOYER_SA", ""), "deploy service account email (default: guardian-deployer@<project>)")
	flags.StringVar(&cfg.StateBucket, "state-bucket", envOr("TF_STATE_BUCKET", ""), "GCS bucket for OpenTofu state (default: <project>-tofu-state)")
	flags.StringVar(&cfg.KMSKey, "kms-key", envOr("GCP_KMS_KEY", ""), "KMS key resource name for state encryption")
	return flags, cfg
}

func gcpSetupCommand() *command.Command {
	flags, cfg := gcpConfigFlags("gcp setup")
	jsonOut := flags.Bool("json", false, "print the report as JSON")
	strict := flags.Bool("strict", false, "treat warnings as errors")
	return &command.Command{
		Description: "Verify that a GCP project is ready for the Guardian GCP pusher (APIs, service account, state bucket, KMS key)",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			*cfg = cfg.WithDefaults()
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w (set --project)", err)
			}
			report, err := gcpsetup.Run(ctx, *cfg)
			if err != nil {
				return err
			}
			if *jsonOut {
				encoded, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(encoded))
			} else {
				fmt.Printf("=== gcp setup ===\nproject: %s\nregion:  %s\n\n", report.Project, report.Region)
				printGCPReport(report)
			}
			if !report.OK() {
				return fmt.Errorf("GCP project %s is not ready; apply the remediation above", report.Project)
			}
			if *strict {
				for _, check := range report.Checks {
					if check.Severity == gcpsetup.SeverityWarning {
						return fmt.Errorf("strict mode: %s: %s", check.Name, check.Detail)
					}
				}
			}
			fmt.Printf("\n=== setup check complete ===\n")
			return nil
		},
	}
}

func gcpInitCommand() *command.Command {
	flags, cfg := gcpConfigFlags("gcp init")
	pusherName := flags.String("pusher-name", envOr("GUARDIAN_PUSHER_NAME", ""), "pusher name (default: gcp-<project>)")
	readAddr := flags.String("read-addr", "0.0.0.0:19091", "read API address the pusher will listen on")
	namespace := flags.String("namespace", "guardian", "Kubernetes namespace the pusher runs in")
	jsonOut := flags.Bool("json", false, "print the deployment environment as JSON")
	return &command.Command{
		Description: "Verify GCP prerequisites and print the environment for deploy-pusher-gcp.yaml",
		Flags:       flags,
		Run: func(ctx context.Context, args []string) error {
			*cfg = cfg.WithDefaults()
			if err := cfg.Validate(); err != nil {
				return fmt.Errorf("invalid config: %w (set --project)", err)
			}
			if strings.TrimSpace(*pusherName) == "" {
				*pusherName = "gcp-" + sanitizeGCPLabel(cfg.Project)
			}
			report, err := gcpsetup.Run(ctx, *cfg)
			if err != nil {
				return err
			}
			printGCPReport(report)
			if !report.OK() {
				return fmt.Errorf("GCP project %s is not ready; apply the remediation above", report.Project)
			}

			values := map[string]string{
				"GUARDIAN_PUSHER_NAME":      *pusherName,
				"GUARDIAN_GCP_PROJECT":      cfg.Project,
				"GUARDIAN_GCP_REGION":       cfg.Region,
				"GUARDIAN_GCP_STATE_BUCKET": cfg.StateBucket,
				"GUARDIAN_GCP_KMS_KEY":      cfg.KMSKey,
				"GUARDIAN_GCPREAD_ADDR":     *readAddr,
				"GUARDIAN_OBSERVED_API_URL": fmt.Sprintf("http://%s.%s.svc:19091", *pusherName, *namespace),
			}
			if *jsonOut {
				encoded, err := json.MarshalIndent(values, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(encoded))
				return nil
			}
			fmt.Printf("\n=== pusher environment ===\n")
			for _, key := range []string{
				"GUARDIAN_PUSHER_NAME",
				"GUARDIAN_GCP_PROJECT",
				"GUARDIAN_GCP_REGION",
				"GUARDIAN_GCP_STATE_BUCKET",
				"GUARDIAN_GCP_KMS_KEY",
				"GUARDIAN_GCPREAD_ADDR",
				"GUARDIAN_OBSERVED_API_URL",
			} {
				fmt.Printf("%s=%s\n", key, values[key])
			}
			fmt.Printf("\nnext: deploy stratatools/deploy/bootstrap/guardian/deploy-pusher-gcp.yaml\n")
			return nil
		},
	}
}

func printGCPReport(report gcpsetup.Report) {
	for _, check := range report.Checks {
		symbol := "✓"
		switch check.Severity {
		case gcpsetup.SeverityWarning:
			symbol = "!"
		case gcpsetup.SeverityCritical:
			symbol = "✗"
		}
		fmt.Printf("  %s %-16s %s\n", symbol, check.Name, check.Detail)
		if check.Remediation != "" {
			fmt.Printf("      → %s\n", check.Remediation)
		}
	}
}

func sanitizeGCPLabel(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.NewReplacer("_", "-", ".", "-").Replace(value)
	out := make([]rune, 0, len(value))
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			out = append(out, r)
		}
	}
	result := strings.Trim(string(out), "-")
	if result == "" {
		return "default"
	}
	return result
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
