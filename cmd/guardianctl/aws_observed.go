package main

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"github.com/radryc/guardian/internal/cli/command"
	"github.com/radryc/guardian/internal/cli/output"
	"github.com/radryc/guardian/internal/observedgen"
)

// awsObservedGenerateCommand generates a local observed partition bundle from a
// synthesized CDK cloud assembly. The bundle can be pushed with
// `guardianctl partition push --dir`.
func awsObservedGenerateCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("aws observed-generate", flag.ContinueOnError)
	assembly := flags.String("assembly", "", "local CDK cloud assembly directory (cdk.out)")
	partition := flags.String("partition", "", "target partition name")
	account := flags.String("account", "", "AWS account id")
	region := flags.String("region", "", "AWS region")
	pusher := flags.String("pusher", "", "target pusher name (default aws-<account>)")
	out := flags.String("out", "", "output partition bundle directory")
	payloadDir := flags.String("payload-dir", "", "logical MonoFS payload dir (default /partitions/<partition>/payloads/aws)")
	mode := flags.String("mode", string(observedgen.ModeObserved), "intent mode: observed (report-only) or managed (standard, Guardian applies)")
	selector := flags.String("selector", "", "comma-separated CloudFormation stack name globs")
	return &command.Command{Description: "Generate a CloudFormation partition bundle from a CDK cloud assembly", Flags: flags, Run: func(ctx context.Context, args []string) error {
		if strings.TrimSpace(*assembly) == "" || strings.TrimSpace(*partition) == "" ||
			strings.TrimSpace(*account) == "" || strings.TrimSpace(*region) == "" || strings.TrimSpace(*out) == "" {
			return fmt.Errorf("--assembly, --partition, --account, --region and --out are required")
		}
		created, err := observedgen.Generate(observedgen.Options{
			Mode:        observedgen.Mode(*mode),
			AssemblyDir: *assembly,
			OutDir:      *out,
			Partition:   *partition,
			Account:     *account,
			Region:      *region,
			Pusher:      *pusher,
			PayloadDir:  *payloadDir,
			Selector:    *selector,
		})
		if err != nil {
			return err
		}
		if printer.Format == "json" {
			printer.PrintJSON(map[string]any{"partition": *partition, "out": *out, "intents": created})
			return nil
		}
		for _, intent := range created {
			fmt.Fprintf(printer.Writer, "created intent %s\n", intent)
		}
		fmt.Fprintf(printer.Writer, "wrote observed partition bundle to %s\n", *out)
		return nil
	}}
}
