package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/cli/command"
	"github.com/rydzu/ainfra/guardian/internal/cli/output"
)

type repeatedFlag []string

func (r *repeatedFlag) String() string { return strings.Join(*r, ",") }

func (r *repeatedFlag) Set(value string) error {
	*r = append(*r, value)
	return nil
}

// awsPusherDeployCommand deploys the AwsPusherStack CDK stack into an AWS
// account and prints the registration steps.
func awsPusherDeployCommand(printer *output.Printer) *command.Command {
	flags := flag.NewFlagSet("aws pusher-deploy", flag.ContinueOnError)
	dir := flags.String("dir", ".", "path to the aws CDK project")
	profile := flags.String("profile", "", "AWS profile for the CDK CLI")
	account := flags.String("account", "", "target AWS account id (sets CDK_DEFAULT_ACCOUNT)")
	region := flags.String("region", "", "target AWS region (sets CDK_DEFAULT_REGION)")
	pusherName := flags.String("pusher-name", "", "Guardian pusher name (default aws-<account>)")
	noObserve := flags.Bool("no-observe", false, "deploy without the EventBridge/SQS observation pipeline")
	requireApproval := flags.String("require-approval", "never", "CDK require-approval mode")
	var contexts repeatedFlag
	flags.Var(&contexts, "context", "additional CDK context key=value (repeatable)")
	return &command.Command{Description: "Deploy the AWS pusher stack (AwsPusherStack)", Flags: flags, Run: func(ctx context.Context, args []string) error {
		cdkDir, err := filepath.Abs(filepath.Clean(*dir))
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(cdkDir, "cdk.json")); err != nil {
			return fmt.Errorf("%s does not look like a CDK project (missing cdk.json)", cdkDir)
		}
		cdkArgs := []string{"cdk", "deploy", "AwsPusherStack", "--require-approval", *requireApproval}
		if strings.TrimSpace(*profile) != "" {
			cdkArgs = append(cdkArgs, "--profile", strings.TrimSpace(*profile))
		}
		if strings.TrimSpace(*pusherName) != "" {
			cdkArgs = append(cdkArgs, "-c", "pusherName="+strings.TrimSpace(*pusherName))
		}
		if *noObserve {
			cdkArgs = append(cdkArgs, "-c", "deployAwsObserve=false")
		}
		for _, item := range contexts {
			cdkArgs = append(cdkArgs, "-c", item)
		}
		cmd := exec.CommandContext(ctx, "npx", cdkArgs...)
		cmd.Dir = cdkDir
		cmd.Env = os.Environ()
		if strings.TrimSpace(*account) != "" {
			cmd.Env = append(cmd.Env, "CDK_DEFAULT_ACCOUNT="+strings.TrimSpace(*account))
		}
		if strings.TrimSpace(*region) != "" {
			cmd.Env = append(cmd.Env, "CDK_DEFAULT_REGION="+strings.TrimSpace(*region))
		}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cdk deploy AwsPusherStack: %w", err)
		}
		printer.PrintText("\npusher deployed. Register it with Guardian:\n")
		printer.PrintText("  guardianctl pusher add --account <account> --region <region> \\\n")
		printer.PrintText("    --url <AwsReadApiUrl or http://guardian-awsread:19090> \\\n")
		printer.PrintText("    --token-env GUARDIAN_AWSREAD_TOKEN\n")
		printer.PrintText("  guardianctl repo add --name <repo> --pusher <pusher> --partition <partition> --source <git-url>\n")
		printer.PrintText("  guardianctl repo sync --name <repo> --synth\n")
		return nil
	}}
}
