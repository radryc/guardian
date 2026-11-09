package gcpdriver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
	assetdefs "github.com/radryc/guardian/internal/domain/assets"
	taskdomain "github.com/radryc/guardian/internal/domain/task"
	"github.com/radryc/guardian/internal/pusher/driverutil"
	"github.com/radryc/guardian/internal/pusher/registry"
)

// SQLDatabaseDriver maps Database/SQLDatabase assets to Cloud SQL.
type SQLDatabaseDriver struct {
	baseDriver
	typeName string
}

func (d *SQLDatabaseDriver) Type() string { return d.typeName }

func (d *SQLDatabaseDriver) Validate(props map[string]any) error {
	return d.ValidateInContext(props, nil)
}

// ValidateInContext validates Volume/Config references against the intent graph.
func (d *SQLDatabaseDriver) ValidateInContext(props map[string]any, assets map[string]taskdomain.AbstractAsset) error {
	return assetdefs.Validate(
		assetdomain.Spec{Type: d.typeName, Properties: props},
		assetdefs.ValidationContext{AssetTypes: assetTypeMap(assets)},
	)
}

type sqlEngineInfo struct {
	Engine      string
	Version     string
	Scheme      string
	DefaultPort int
}

func resolveSQLEngine(spec *assetdefs.SQLDatabaseSpec) (sqlEngineInfo, error) {
	engine := strings.ToLower(strings.TrimSpace(spec.Engine))
	switch {
	case strings.Contains(engine, "postgres"):
		return sqlEngineInfo{Engine: "POSTGRES", Version: databaseVersion("POSTGRES", spec.Version), Scheme: "postgres", DefaultPort: 5432}, nil
	case strings.Contains(engine, "mysql"), strings.Contains(engine, "maria"):
		return sqlEngineInfo{Engine: "MYSQL", Version: databaseVersion("MYSQL", spec.Version), Scheme: "mysql", DefaultPort: 3306}, nil
	case strings.Contains(engine, "sqlserver"), strings.Contains(engine, "mssql"):
		return sqlEngineInfo{Engine: "SQLSERVER", Version: databaseVersion("SQLSERVER", spec.Version), Scheme: "sqlserver", DefaultPort: 1433}, nil
	default:
		return sqlEngineInfo{}, fmt.Errorf("unsupported database engine %q for GCP Cloud SQL", spec.Engine)
	}
}

func databaseVersion(engine, version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		switch engine {
		case "POSTGRES":
			return "POSTGRES_16"
		case "MYSQL":
			return "MYSQL_8_0"
		default:
			return "SQLSERVER_2019_STANDARD"
		}
	}
	if strings.Contains(version, "_") {
		return strings.ToUpper(version)
	}
	parts := strings.Split(version, ".")
	switch engine {
	case "POSTGRES":
		return "POSTGRES_" + parts[0]
	case "MYSQL":
		if len(parts) > 1 {
			return "MYSQL_" + parts[0] + "_" + parts[1]
		}
		return "MYSQL_" + parts[0] + "_0"
	default:
		return "SQLSERVER_" + parts[0] + "_STANDARD"
	}
}

func databaseIdentifier(name string) string {
	clean := sanitizeName(name)
	clean = strings.ReplaceAll(clean, "-", "_")
	clean = strings.Trim(clean, "_")
	if clean == "" {
		return "guardian"
	}
	if len(clean) > 63 {
		clean = clean[:63]
	}
	return clean
}

func (d *SQLDatabaseDriver) request(in registry.AssetInput, spec *assetdefs.SQLDatabaseSpec) (StackRequest, sqlEngineInfo, error) {
	info, err := resolveSQLEngine(spec)
	if err != nil {
		return StackRequest{}, sqlEngineInfo{}, err
	}
	port := info.DefaultPort
	if spec.Port != nil && *spec.Port > 0 {
		port = *spec.Port
	}
	network := ""
	if len(spec.Networks) > 0 {
		network = networkName(in.PartitionName, in.IntentName, spec.Networks[0])
	}
	database := strings.TrimSpace(spec.Database)
	if database == "" {
		database = databaseIdentifier(in.Asset.Name)
	}
	user := strings.TrimSpace(spec.User)
	if user == "" {
		user = "guardian"
	}
	return StackRequest{
		Partition:   in.PartitionName,
		Intent:      in.IntentName,
		Asset:       in.Asset.Name,
		Project:     in.Target.Account,
		Region:      in.Target.Region,
		Blueprint:   "sqldatabase",
		DesiredHash: driverutil.CompositeHash(in, database, strconv.Itoa(port), network, user),
		Variables: map[string]any{
			"name":             sqlInstanceName(in.PartitionName, in.IntentName, in.Asset.Name),
			"engine":           info.Engine,
			"database_version": info.Version,
			"tier":             "db-f1-micro",
			"database":         database,
			"user":             user,
			"port":             port,
			"network":          network,
			"scheme":           info.Scheme,
		},
	}, info, nil
}

func (d *SQLDatabaseDriver) Check(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return err
	}
	req, _, err := d.request(in, spec)
	if err != nil {
		return err
	}
	return d.backend.CheckEnvironment(ctx, req)
}

func (d *SQLDatabaseDriver) Diff(ctx context.Context, in registry.AssetInput) (taskdomain.DriftReport, error) {
	if err := ctx.Err(); err != nil {
		return taskdomain.DriftReport{}, err
	}
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	req, _, err := d.request(in, spec)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	state, err := d.backend.Get(ctx, req)
	if err != nil {
		return taskdomain.DriftReport{}, err
	}
	if !state.Exists {
		return changedDrift(in.Asset.Name, "Cloud SQL instance does not exist"), nil
	}
	if shortHash(req.DesiredHash) != state.Outputs["guardian_hash"] {
		return changedDrift(in.Asset.Name, "Cloud SQL instance differs"), nil
	}
	return inSyncDrift(in.Asset.Name, "Cloud SQL instance is in sync"), nil
}

func (d *SQLDatabaseDriver) Apply(ctx context.Context, in registry.AssetInput) (registry.AssetResult, error) {
	if err := ctx.Err(); err != nil {
		return registry.AssetResult{}, err
	}
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return registry.AssetResult{}, err
	}
	req, info, err := d.request(in, spec)
	if err != nil {
		return registry.AssetResult{}, err
	}
	outputs, err := d.backend.Apply(ctx, req)
	if err != nil {
		return registry.AssetResult{}, fmt.Errorf("apply Cloud SQL instance: %w", err)
	}
	if outputs["engine"] == "" {
		outputs["engine"] = info.Engine
	}
	if outputs["type"] == "" {
		outputs["type"] = "cloudsql"
	}
	return registry.AssetResult{Outputs: outputs}, nil
}

func (d *SQLDatabaseDriver) Destroy(ctx context.Context, in registry.AssetInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	spec, err := decodeSQLDatabase(in)
	if err != nil {
		return err
	}
	req, _, err := d.request(in, spec)
	if err != nil {
		return err
	}
	return d.backend.Destroy(ctx, req)
}

func decodeSQLDatabase(in registry.AssetInput) (*assetdefs.SQLDatabaseSpec, error) {
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
