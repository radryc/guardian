package awsdriver

import (
	"fmt"
	"strings"

	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

func awsResourceName(prefix string, target targetdomain.Placement, partition, intent, asset string) string {
	account := strings.TrimSpace(target.Account)
	region := strings.TrimSpace(target.Region)
	if account == "" {
		account = "default"
	}
	if region == "" {
		region = "default"
	}
	return driverutil.ResourceName(
		fmt.Sprintf("aws-%s-%s-%s", prefix, account, region),
		targetdomain.Placement{},
		partition, intent, asset,
	)
}

func awsTaskFamily(prefix string, in registry.AssetInput) string {
	return awsResourceName(prefix, in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func awsServiceName(in registry.AssetInput) string {
	return awsResourceName("svc", in.Target, in.PartitionName, in.IntentName, in.Asset.Name)
}

func awsEFSName(in registry.AssetInput, assetName string) string {
	return awsResourceName("efs", in.Target, in.PartitionName, in.IntentName, assetName)
}

func awsEFSAccessPointName(in registry.AssetInput, assetName string) string {
	return awsResourceName("ap", in.Target, in.PartitionName, in.IntentName, assetName)
}

func awsSSMParameterName(in registry.AssetInput, assetName string) string {
	name := awsResourceName("ssm", in.Target, in.PartitionName, in.IntentName, assetName)
	return "/guardian/" + strings.ReplaceAll(name, "-", "/")
}

func awsSecretName(in registry.AssetInput, assetName string) string {
	name := awsResourceName("secret", in.Target, in.PartitionName, in.IntentName, assetName)
	return "/guardian/" + name
}

func awsALBName(in registry.AssetInput) string {
	hash := driverutil.AssetHash(in)
	short := hash[:min(8, len(hash))]
	return fmt.Sprintf("g-%s-%s", short, sanitizeAlpha(in.Asset.Name[:min(20, len(in.Asset.Name))]))
}

func awsTargetGroupName(in registry.AssetInput, suffix string) string {
	hash := driverutil.AssetHash(in)
	short := hash[:min(8, len(hash))]
	name := fmt.Sprintf("g-%s-%s%s", short, sanitizeAlpha(in.Asset.Name[:min(15, len(in.Asset.Name))]), suffix)
	if len(name) > 32 {
		name = name[:32]
	}
	return strings.TrimRight(name, "-")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sanitizeAlpha(s string) string {
	result := make([]byte, 0, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' {
			result = append(result, c)
		}
	}
	return string(result)
}

func awsS3BucketName(in registry.AssetInput) string {
	account := strings.TrimSpace(in.Target.Account)
	if account == "" {
		account = "default"
	}
	name := driverutil.ResourceName(
		fmt.Sprintf("guardian-%s", account),
		targetdomain.Placement{Region: in.Target.Region},
		in.PartitionName, in.IntentName, in.Asset.Name,
	)
	return strings.ToLower(name)
}

func awsLogGroupName(in registry.AssetInput) string {
	account := strings.TrimSpace(in.Target.Account)
	if account == "" {
		account = "default"
	}
	return fmt.Sprintf("guardian/%s/%s/%s/%s", account, in.Target.Region, in.PartitionName, in.IntentName)
}

func awsTags(in registry.AssetInput, hash string) map[string]string {
	tags := map[string]string{
		"guardian-managed":   "true",
		"guardian-partition": in.PartitionName,
		"guardian-intent":    in.IntentName,
		"guardian-asset":     in.Asset.Name,
		"guardian-type":      in.Asset.Type,
		"guardian-account":   strings.TrimSpace(in.Target.Account),
		"guardian-hash":      hash,
	}
	return tags
}

func awsAccount(in registry.AssetInput) string {
	if strings.TrimSpace(in.Target.Account) == "" {
		return "default"
	}
	return strings.TrimSpace(in.Target.Account)
}

func awsRegion(in registry.AssetInput) string {
	if strings.TrimSpace(in.Target.Region) == "" {
		return "us-east-1"
	}
	return strings.TrimSpace(in.Target.Region)
}
