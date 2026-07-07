package awssetup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

type StackStatus int

const (
	StackNotFound StackStatus = iota
	StackReady
	StackFailed
)

func checkCDKBootstrapStackStatus(ctx context.Context, cfg Config) (StackStatus, error) {
	if cfg.DryRun {
		return StackNotFound, nil
	}

	awsCfg, err := loadAWSConfig(ctx, cfg)
	if err != nil {
		return StackNotFound, err
	}

	client := cloudformation.NewFromConfig(awsCfg)
	out, err := client.DescribeStacks(ctx, &cloudformation.DescribeStacksInput{
		StackName: awsroot.String(cfg.BootstrapStackName),
	})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return StackNotFound, nil
		}
		return StackNotFound, fmt.Errorf("describe bootstrap stack %s: %w", cfg.BootstrapStackName, err)
	}

	if len(out.Stacks) == 0 {
		return StackNotFound, nil
	}

	status := out.Stacks[0].StackStatus
	switch status {
	case cftypes.StackStatusCreateComplete,
		cftypes.StackStatusUpdateComplete,
		cftypes.StackStatusUpdateRollbackComplete:
		return StackReady, nil
	case cftypes.StackStatusRollbackComplete,
		cftypes.StackStatusRollbackFailed,
		cftypes.StackStatusDeleteFailed,
		cftypes.StackStatusCreateFailed:
		return StackFailed, nil
	case cftypes.StackStatusReviewInProgress:
		return StackFailed, nil
	default:
		return StackReady, nil
	}
}

func CheckCDKBootstrapStack(ctx context.Context, cfg Config, accountID string) (bool, error) {
	status, err := checkCDKBootstrapStackStatus(ctx, cfg)
	if err != nil {
		return false, err
	}
	return status == StackReady, nil
}

func RunCDKBootstrap(ctx context.Context, cfg Config, accountID string) error {
	status, err := checkCDKBootstrapStackStatus(ctx, cfg)
	if err != nil {
		return err
	}

	if status == StackReady {
		return nil
	}

	if status == StackFailed {
		fmt.Printf("=== deleting failed %s stack ===\n", cfg.BootstrapStackName)

		if cfg.DryRun {
			fmt.Printf("  would delete stack: %s\n", cfg.BootstrapStackName)
		} else {
			awsCfg, err := loadAWSConfig(ctx, cfg)
			if err != nil {
				return err
			}
			client := cloudformation.NewFromConfig(awsCfg)
			_, err = client.DeleteStack(ctx, &cloudformation.DeleteStackInput{
				StackName: awsroot.String(cfg.BootstrapStackName),
			})
			if err != nil {
				return fmt.Errorf("delete failed stack %s: %w", cfg.BootstrapStackName, err)
			}

			waiter := cloudformation.NewStackDeleteCompleteWaiter(client)
			if wErr := waiter.Wait(ctx, &cloudformation.DescribeStacksInput{
				StackName: awsroot.String(cfg.BootstrapStackName),
			}, 10*time.Minute); wErr != nil {
				if !strings.Contains(wErr.Error(), "does not exist") {
					return fmt.Errorf("waiting for stack deletion: %w", wErr)
				}
			}
			fmt.Printf("  deleted: %s\n", cfg.BootstrapStackName)
		}
	}

	if cfg.DryRun {
		fmt.Printf("  would run: cdk bootstrap aws://%s/%s\n", accountID, cfg.Region)
		return nil
	}

	cdk, err := exec.LookPath("cdk")
	if err != nil {
		npx, npxErr := exec.LookPath("npx")
		if npxErr != nil {
			return fmt.Errorf("cdk not found in PATH and npx cdk not available: install aws-cdk (npm install -g aws-cdk)")
		}
		cdk = npx
	}

	fmt.Printf("=== running cdk bootstrap aws://%s/%s ===\n", accountID, cfg.Region)

	bootstrapArgs := []string{
		"bootstrap",
		fmt.Sprintf("aws://%s/%s", accountID, cfg.Region),
		"--force",
	}
	if cdk == "npx" || strings.HasSuffix(cdk, "npx") {
		bootstrapArgs = append([]string{"cdk"}, bootstrapArgs...)
	}

	cmd := exec.CommandContext(ctx, cdk, bootstrapArgs...)
	cmd.Env = bootstrapEnv(cfg)

	start := time.Now()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cdk bootstrap failed: %w\n%s", err, string(out))
	}

	fmt.Printf("  completed in %s\n", time.Since(start).Round(time.Second))

	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
		s, checkErr := checkCDKBootstrapStackStatus(ctx, cfg)
		if checkErr == nil && s == StackReady {
			return nil
		}
	}

	return fmt.Errorf("bootstrap stack %s not found after deploy", cfg.BootstrapStackName)
}

func bootstrapEnv(cfg Config) []string {
	env := os.Environ()
	env = append(env, "CDK_NEW_BOOTSTRAP=1")
	if cfg.Profile != "" {
		env = append(env, "AWS_PROFILE="+cfg.Profile)
	}
	if cfg.Region != "" {
		env = append(env, "AWS_REGION="+cfg.Region)
		env = append(env, "AWS_DEFAULT_REGION="+cfg.Region)
		env = append(env, "CDK_DEFAULT_REGION="+cfg.Region)
	}
	return env
}
