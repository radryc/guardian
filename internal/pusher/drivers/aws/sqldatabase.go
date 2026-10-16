package awsdriver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	taskdomain "github.com/rydzu/ainfra/guardian/internal/domain/task"
	"github.com/rydzu/ainfra/guardian/internal/pusher/driverutil"
	"github.com/rydzu/ainfra/guardian/internal/pusher/registry"
)

// SQLDatabaseDriver maps a SQLDatabase/Database asset to an RDS Aurora
// cluster. The master password is managed by RDS in Secrets Manager.
type SQLDatabaseDriver struct {
	baseDriver
	typeName string
}

func (d *SQLDatabaseDriver) Type() string { return d.typeName }

func (d *SQLDatabaseDriver) Validate(props map[string]any) error {
	return assetdefs.Validate(assetdomain.Spec{Type: d.typeName, Properties: props}, assetdefs.ValidationContext{})
}

func (d *SQLDatabaseDriver) Check(ctx context.Context, in registry.AssetInput) error {
	return ctx.Err()
}

func (d *SQLDatabaseDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeAuroraSpec(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if _, err := auroraEngine(spec.Engine); err != nil {
		return taskdomain.DriftReport{}, err
	}
	cluster, ok, err := d.backend.GetDBCluster(ctx, awsDBClusterName(in))
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !ok {
		return changedDrift(in.Asset.Name, "aurora cluster is missing"), nil
	}
	if cluster.Hash != driverutil.CompositeHash(in) {
		return changedDrift(in.Asset.Name, "aurora cluster differs"), nil
	}
	if cluster.Status != "" && !strings.EqualFold(cluster.Status, "available") {
		return changedDrift(in.Asset.Name, "aurora cluster status is "+cluster.Status), nil
	}
	return inSyncDrift(in.Asset.Name, "aurora cluster is in sync"), nil
}

func (d *SQLDatabaseDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeAuroraSpec(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	engine, err := auroraEngine(spec.Engine)
	if err != nil {
		return registry.AssetResult{}, err
	}
	hash := driverutil.CompositeHash(in)
	desired := DBCluster{
		Name:           awsDBClusterName(in),
		Hash:           hash,
		Tags:           awsTags(in, hash),
		Engine:         engine,
		EngineVersion:  strings.TrimSpace(spec.Version),
		DatabaseName:   auroraDatabaseName(spec, in.Asset.Name),
		MasterUsername: auroraUsername(spec),
		Port:           auroraPort(engine, spec.Port),
		SubnetGroup:    awsDBSubnetGroupName(in),
	}
	cluster, err := d.backend.UpsertDBCluster(ctx, desired)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("upsert aurora cluster: %w", err)
	}
	outputs := map[string]string{
		"id":       cluster.Name,
		"engine":   engine,
		"database": cluster.DatabaseName,
		"user":     cluster.MasterUsername,
	}
	if cluster.Port > 0 {
		outputs["port"] = strconv.Itoa(cluster.Port)
	}
	if cluster.ARN != "" {
		outputs["arn"] = cluster.ARN
	}
	if cluster.Endpoint != "" {
		outputs["endpoint"] = cluster.Endpoint
		outputs["url"] = fmt.Sprintf("%s://%s@%s:%d/%s", auroraScheme(engine), cluster.MasterUsername, cluster.Endpoint, cluster.Port, cluster.DatabaseName)
	}
	if cluster.ReaderEndpoint != "" {
		outputs["readerEndpoint"] = cluster.ReaderEndpoint
	}
	if cluster.MasterUserSecret != "" {
		outputs["secretArn"] = cluster.MasterUserSecret
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *SQLDatabaseDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	name := awsDBClusterName(in)
	arn := ""
	if cluster, ok, err := d.backend.GetDBCluster(ctx, name); err == nil && ok {
		arn = cluster.ARN
	}
	return d.backend.DeleteDBCluster(ctx, arn, name)
}

func decodeAuroraSpec(in registry.AssetInput) (*assetdefs.SQLDatabaseSpec, error) {
	typed, err := driverutil.DecodeAsset(in)
	if err != nil {
		return nil, err
	}
	spec, ok := typed.(*assetdefs.SQLDatabaseSpec)
	if !ok {
		return nil, fmt.Errorf("expected SQLDatabaseSpec, got %T", typed)
	}
	return spec, nil
}

func auroraEngine(engine string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "aurora", "aurora-mysql", "mysql":
		return "aurora-mysql", nil
	case "aurora-postgresql", "postgres", "postgresql":
		return "aurora-postgresql", nil
	default:
		return "", fmt.Errorf("unsupported aurora engine %q (use aurora-mysql or aurora-postgresql)", engine)
	}
}

func auroraScheme(engine string) string {
	if engine == "aurora-postgresql" {
		return "postgresql"
	}
	return "mysql"
}

func auroraPort(engine string, port *int) int {
	if port != nil && *port > 0 {
		return *port
	}
	if engine == "aurora-mysql" {
		return 3306
	}
	return 5432
}

func auroraUsername(spec *assetdefs.SQLDatabaseSpec) string {
	if user := strings.TrimSpace(spec.User); user != "" {
		return user
	}
	return "guardian"
}

func auroraDatabaseName(spec *assetdefs.SQLDatabaseSpec, fallback string) string {
	name := strings.TrimSpace(spec.Database)
	if name == "" {
		name = fallback
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune('_')
		}
	}
	name = strings.Trim(b.String(), "_")
	if name == "" {
		return "app"
	}
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}
