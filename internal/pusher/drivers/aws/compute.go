package awsdriver

import (
	"context"
	"fmt"
	"strings"

	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

type ComputeDriver struct{ baseDriver }

func (d *ComputeDriver) Type() string                { return "Compute" }
func (d *ComputeDriver) Validate(p map[string]any) error { return nil }

func (d *ComputeDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (d *ComputeDriver) checkDep(ctx context.Context, in registry.AssetInput, dep string) error {
	asset, ok := in.Assets[dep]
	if !ok {
		return nil
	}
	switch asset.Type {
	case "Config":
		_, _, err := d.backend.GetParameter(ctx, awsSSMParameterName(in, dep))
		return err
	case "Volume":
		_, _, err := d.backend.GetFileSystem(ctx, awsEFSName(in, dep))
		return err
	case "Secret":
		_, _, err := d.backend.GetSecret(ctx, awsSecretName(in, dep))
		return err
	}
	return nil
}

func (d *ComputeDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}

	hash := driverutil.CompositeHash(in)
	svcName := awsServiceName(in)
	cluster := awsAccount(in)

	svc, ok, err := d.backend.GetService(ctx, cluster, svcName)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok || svc.Hash != hash {
		return changedDrift(in.Asset.Name, "ECS service differs"), nil
	}
	_ = spec
	return inSyncDrift(in.Asset.Name, "ECS service is in sync"), nil
}

func (d *ComputeDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeCompute(in)
	if err != nil {
		return registry.AssetResult{}, err
	}

	hash := driverutil.CompositeHash(in)
	launchType := "FARGATE"
	if driverutil.BoolValue(spec.Privileged) || hasCapability(spec.Capabilities, "SYS_ADMIN") {
		launchType = "EC2"
	}

	env, err := driverutil.ResolveEnv(ctx, d.resolver, spec.Env)
	if err != nil {
		return registry.AssetResult{}, err
	}

	secrets := resolveComputeSecrets(in, spec)

	ports := make([]PortDef, 0, len(spec.Ports))
	for _, p := range spec.Ports {
		containerPort := 0
		if p.ContainerPort != nil {
			containerPort = *p.ContainerPort
		}
		if containerPort == 0 && p.Port != nil {
			containerPort = *p.Port
		}
		hostPort := 0
		if p.HostPort != nil {
			hostPort = *p.HostPort
		}
		ports = append(ports, PortDef{
			Name:          p.Name,
			ContainerPort: containerPort,
			HostPort:      hostPort,
			Protocol:      "tcp",
		})
	}

	volumes := make([]VolumeDef, 0, len(spec.VolumeMounts))
	for _, mount := range spec.VolumeMounts {
		volumes = append(volumes, VolumeDef{
			Name:            mount.Volume,
			EFSFileSystemID: awsEFSName(in, mount.Volume),
			RootPath:        mount.Path,
		})
	}

	replicas := 1
	if spec.Replicas != nil {
		replicas = *spec.Replicas
	}

	taskFamily := awsTaskFamily("task", in)
	logGroup := awsLogGroupName(in)
	tags := awsTags(in, hash)

	_ = d.backend.UpsertLogGroup(ctx, LogGroup{
		Name: logGroup,
		Hash: hash,
		Tags: tags,
	})

	cpu, mem := resolveResources(spec)

	svc := ECSService{
		Name:           awsServiceName(in),
		Cluster:        awsAccount(in),
		Hash:           hash,
		Tags:           tags,
		DesiredCount:   replicas,
		LaunchType:     launchType,
		TaskFamily:     taskFamily,
		AssignPublicIP: launchType == "FARGATE",
		LogGroup:       logGroup,
		Container: ContainerDef{
			Name:   in.Asset.Name,
			Image:  spec.Image,
			Command: []string(spec.Command),
			Args:   []string(spec.Args),
			Env:     env,
			Secrets: secrets,
			Ports:   ports,
			CPU:     cpu,
			Memory:  mem,
			Volumes: volumes,
			Privileged: driverutil.BoolValue(spec.Privileged),
		},
	}

	if err := d.backend.UpsertService(ctx, svc); err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert ECS service: %w", err)
	}

	outputs := map[string]string{"service": awsServiceName(in), "launchType": launchType}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *ComputeDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return d.backend.DeleteService(ctx, awsAccount(in), awsServiceName(in))
}

func decodeCompute(in registry.AssetInput) (*assetdefs.ComputeSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.ComputeSpec)
	if !ok {
		return nil, fmt.Errorf("expected ComputeSpec, got %T", typed)
	}
	return spec, nil
}

func resolveComputeSecrets(in registry.AssetInput, spec *assetdefs.ComputeSpec) map[string]string {
	secrets := make(map[string]string)
	for _, mount := range spec.ConfigMounts {
		if _, ok := in.Assets[mount.Config]; ok {
			secrets[mount.Config] = awsSSMParameterName(in, mount.Config)
		}
	}
	return secrets
}

func hasCapability(caps []string, target string) bool {
	for _, c := range caps {
		if c == target {
			return true
		}
	}
	return false
}

func resolveResources(spec *assetdefs.ComputeSpec) (int, int) {
	cpu := 256
	mem := 512
	if spec.Resources != nil {
		if spec.Resources.Limits.CPU != "" {
			cpu = parseCPU(spec.Resources.Limits.CPU)
		} else if spec.Resources.Requests.CPU != "" {
			cpu = parseCPU(spec.Resources.Requests.CPU)
		}
		if spec.Resources.Limits.Memory != "" {
			mem = parseMemory(spec.Resources.Limits.Memory)
		} else if spec.Resources.Requests.Memory != "" {
			mem = parseMemory(spec.Resources.Requests.Memory)
		}
	}
	return cpu, mem
}

func parseCPU(cpu string) int {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return 256
	}
	isMillicpu := strings.HasSuffix(cpu, "m")
	if isMillicpu {
		cpu = cpu[:len(cpu)-1]
	}
	val := 0
	fracDigits := 0
	for i := 0; i < len(cpu); i++ {
		c := cpu[i]
		if c >= '0' && c <= '9' {
			if fracDigits > 0 {
				fracDigits++
			}
			val = val*10 + int(c-'0')
		} else if c == '.' {
			fracDigits = 1
		}
	}
	if isMillicpu {
		// millicpu to vCPU: 1000m = 1 vCPU
		// ECS CPU units: 1 vCPU = 1024 units
		val = val * 1024 / 1000
	} else if fracDigits > 0 {
		// fractional vCPU (e.g. "0.5") — scale back the extra digits
		for i := 1; i < fracDigits; i++ {
			val /= 10
		}
		val = val * 1024
	}
	if val == 0 {
		return 256
	}
	if val < 256 {
		return 256
	}
	return val
}

func parseMemory(mem string) int {
	val := 0
	for i := 0; i < len(mem); i++ {
		c := mem[i]
		if c >= '0' && c <= '9' {
			val = val*10 + int(c-'0')
		} else if c == 'G' || c == 'g' {
			val *= 1024
			break
		} else if c == 'M' || c == 'm' {
			break
		}
	}
	if val == 0 {
		return 512
	}
	return val
}
