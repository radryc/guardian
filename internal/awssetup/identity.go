package awssetup

import (
	"context"
	"fmt"

	awsroot "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

type CallerIdentity struct {
	Account string
	UserID  string
	ARN     string
}

func GetCallerIdentity(ctx context.Context, cfg Config) (*CallerIdentity, error) {
	loadOpts := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.Profile != "" {
		loadOpts = append(loadOpts, awsconfig.WithSharedConfigProfile(cfg.Profile))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := sts.NewFromConfig(awsCfg)
	out, err := client.GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return nil, fmt.Errorf("get caller identity: %w", err)
	}

	return &CallerIdentity{
		Account: awsroot.ToString(out.Account),
		UserID:  awsroot.ToString(out.UserId),
		ARN:     awsroot.ToString(out.Arn),
	}, nil
}
