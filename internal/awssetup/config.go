package awssetup

import (
	"fmt"
	"os"
)

type Config struct {
	Profile            string
	Region             string
	RoleName           string
	BootstrapStackName string
	PolicyName         string
	DryRun             bool
}

func DefaultConfig() Config {
	return Config{
		Profile:            os.Getenv("AWS_PROFILE"),
		Region:             "us-east-1",
		RoleName:           "GuardianCdkDeployRole",
		BootstrapStackName: "CDKToolkit",
		PolicyName:         "AdministratorAccess",
		DryRun:             false,
	}
}

func (c Config) Validate() error {
	if c.Region == "" {
		return fmt.Errorf("region is required")
	}
	if c.RoleName == "" {
		return fmt.Errorf("role name is required")
	}
	if c.BootstrapStackName == "" {
		return fmt.Errorf("bootstrap stack name is required")
	}
	return nil
}

func (c Config) WithDefaults() Config {
	if c.Profile == "" {
		c.Profile = os.Getenv("AWS_PROFILE")
	}
	if c.Region == "" {
		if r := os.Getenv("AWS_REGION"); r != "" {
			c.Region = r
		} else if r := os.Getenv("AWS_DEFAULT_REGION"); r != "" {
			c.Region = r
		} else {
			c.Region = "us-east-1"
		}
	}
	if c.RoleName == "" {
		c.RoleName = "GuardianCdkDeployRole"
	}
	if c.BootstrapStackName == "" {
		c.BootstrapStackName = "CDKToolkit"
	}
	if c.PolicyName == "" {
		c.PolicyName = "AdministratorAccess"
	}
	return c
}
