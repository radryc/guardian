// Package cdkgen compiles an intent's declarative assets into a deterministic
// TypeScript AWS CDK application. The generated app composes per-asset
// templates from the catalog (aws/templates/aws/v1) and is deployed by the AWS
// pusher as a single CloudFormation stack.
package cdkgen

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	assetdomain "github.com/radryc/guardian/internal/domain/asset"
)

// LibraryVersion is embedded into generated package metadata so payloads can
// be traced back to the construct library revision that produced them.
const LibraryVersion = "v1"

// PayloadManifestFileName is the generated manifest read by the AWS pusher.
const PayloadManifestFileName = "guardian.payload.json"

// PayloadManifest describes a generated stack to the pusher driver.
type PayloadManifest struct {
	StackName       string            `json:"stackName"`
	StackID         string            `json:"stackID"`
	SourceDir       string            `json:"sourceDir"`
	GeneratedAssets []string          `json:"generatedAssets"`
	NativeAssets    []string          `json:"nativeAssets"`
	OutputMap       map[string]string `json:"outputMap"`
	LibraryVersion  string            `json:"libraryVersion"`
	// CatalogVersion is the template catalog revision that produced the stack.
	CatalogVersion string `json:"catalogVersion,omitempty"`
	// Templates records the exact templates (id, version, digest, source) that
	// were composed into the stack so a deployment is reproducible.
	Templates []TemplateRef `json:"templates,omitempty"`
	// RequiredActions is the union of the selected templates' declared IAM
	// actions. The pusher uses it to provision a per-intent CloudFormation
	// execution role instead of relying on the account-wide bootstrap role.
	RequiredActions []string `json:"requiredActions,omitempty"`
}

// TemplateRef identifies one composed template in the payload manifest.
type TemplateRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	Export  string `json:"export"`
	Module  string `json:"module"`
	Digest  string `json:"digest"`
	Source  string `json:"source,omitempty"`
}

// Input is the resolved intent data required to generate a stack.
type Input struct {
	Partition string
	Intent    string
	Account   string
	Region    string
	SourceDir string
	Assets    []assetdomain.Spec
	// Order is the asset dependency order from the planner (dependencies
	// first). Assets missing from Order are appended in name order.
	Order []string
	// Templates optionally supplies the effective catalog and template files.
	// When nil the embedded baseline catalog is used.
	Templates TemplateProvider
}

// Result is the generated file tree plus the pusher manifest.
type Result struct {
	Generated       bool
	Files           map[string][]byte
	Manifest        PayloadManifest
	GeneratedAssets []string
	NativeAssets    []string
	// Warnings surfaces non-fatal template overlay problems.
	Warnings []string
}

// IsSupported reports whether the asset type can be generated as CDK by the
// embedded baseline catalog.
func IsSupported(assetType string) bool {
	return defaultCatalog.SupportsAssetType(assetType)
}

// HasGeneratedAssets reports whether any asset in the list is CDK-expressible.
func HasGeneratedAssets(assets []assetdomain.Spec) bool {
	for _, asset := range assets {
		if IsSupported(asset.Type) {
			return true
		}
	}
	return false
}

// OutputsForType returns the output keys the template for the given asset type
// publishes. LoadBalancer outputs include one tg_<i>_arn per listener present
// in properties.
func OutputsForType(assetType string, properties map[string]any) []string {
	entry, err := defaultCatalog.Select(assetType, properties)
	if err != nil {
		return nil
	}
	return entry.OutputsFor(properties)
}

// OutputLogicalID is the CloudFormation output logical ID used by the
// templates for an asset output key. It must stay in lockstep with
// _shared/helpers.ts.
func OutputLogicalID(assetName, key string) string {
	return "Out" + titleParts(assetName) + titleParts(key)
}

type plannedAsset struct {
	asset     assetdomain.Spec
	entry     *TemplateEntry
	props     map[string]any
	generated map[string]any
	varName   string
}

// Generate compiles the intent into a deterministic file tree. It returns
// Generated=false when the intent has no CDK-expressible assets.
func Generate(in Input) (*Result, error) {
	provider := in.Templates
	if provider == nil {
		provider = EmbeddedProvider()
	}
	catalog := provider.Catalog()

	ordered := orderAssets(in)
	plan := make([]plannedAsset, 0, len(ordered))
	native := make([]string, 0, len(ordered))
	generatedNames := make([]string, 0, len(ordered))
	networkCount := 0
	for _, asset := range ordered {
		if !catalog.SupportsAssetType(asset.Type) {
			native = append(native, asset.Name)
			continue
		}
		entry, err := catalog.Select(asset.Type, asset.Properties)
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", asset.Name, err)
		}
		props, err := normalizeProps(asset.Type, asset.Properties)
		if err != nil {
			return nil, fmt.Errorf("asset %s: %w", asset.Name, err)
		}
		if err := checkResolved(asset.Name, props); err != nil {
			return nil, err
		}
		if err := entry.ValidateProps(asset.Name, props); err != nil {
			return nil, err
		}
		if asset.Type == assetdomain.TypeNetwork {
			networkCount++
		}
		plan = append(plan, plannedAsset{
			asset:     asset,
			entry:     entry,
			props:     props,
			generated: generatorProps(in, asset),
		})
		generatedNames = append(generatedNames, asset.Name)
	}
	if networkCount > 1 {
		return nil, fmt.Errorf("multiple Network assets are not supported in generated CDK stacks")
	}
	if len(plan) == 0 {
		return &Result{Generated: false, NativeAssets: native, Warnings: provider.Warnings()}, nil
	}

	if err := assignVarNames(plan); err != nil {
		return nil, err
	}
	if err := validateRefs(plan); err != nil {
		return nil, err
	}

	networkVar, needsDefault := resolveNetwork(plan)

	outputMap := map[string]string{}
	for i := range plan {
		for _, key := range plan[i].entry.OutputsFor(plan[i].props) {
			outputMap[plan[i].asset.Name+"."+key] = OutputLogicalID(plan[i].asset.Name, key)
		}
	}

	stackName := stackName(in.Partition, in.Intent)
	sourceDir := strings.TrimSpace(in.SourceDir)
	if sourceDir == "" {
		sourceDir = path.Join("/partitions", in.Partition, "payloads", "aws-generated", in.Intent)
	}

	files := map[string][]byte{}
	files["package.json"] = mustJSON(map[string]any{
		"name":    "guardian-generated-stack",
		"version": "1.0.0",
		"private": true,
		"dependencies": map[string]string{
			"@types/node": "^22.0.0",
			"aws-cdk-lib": "^2.160.0",
			"constructs":  "^10.3.0",
			"ts-node":     "^10.9.2",
			"typescript":  "^5.6.0",
		},
	})
	files["tsconfig.json"] = []byte(tsconfigJSON)
	files["cdk.json"] = mustJSON(map[string]any{
		"app": "npx ts-node --prefer-ts-exts bin/app.ts",
	})
	files["bin/app.ts"] = []byte(binAppTS)
	files["lib/stack.ts"] = []byte(renderStack(in, stackName, plan, networkVar, needsDefault))

	templateRefs, err := copyTemplates(files, provider, plan, needsDefault)
	if err != nil {
		return nil, err
	}

	manifest := PayloadManifest{
		StackName:       stackName,
		StackID:         stackName,
		SourceDir:       sourceDir,
		GeneratedAssets: generatedNames,
		NativeAssets:    native,
		OutputMap:       outputMap,
		LibraryVersion:  LibraryVersion,
		CatalogVersion:  catalog.Version,
		Templates:       templateRefs,
		RequiredActions: requiredActions(provider, plan, needsDefault),
	}
	files[PayloadManifestFileName] = mustJSON(manifest)

	return &Result{
		Generated:       true,
		Files:           files,
		Manifest:        manifest,
		GeneratedAssets: generatedNames,
		NativeAssets:    native,
		Warnings:        provider.Warnings(),
	}, nil
}

// requiredActions is the union of the selected templates' declared IAM actions
// (plus the implicit default network when used).
func requiredActions(provider TemplateProvider, plan []plannedAsset, needsDefault bool) []string {
	seen := map[string]bool{}
	for _, action := range PlatformExecutionActions {
		seen[action] = true
	}
	dirs := make([]string, 0, len(plan)+1)
	for i := range plan {
		dirs = append(dirs, plan[i].entry.Dir())
	}
	if needsDefault {
		if entry, ok := provider.Catalog().Entry("aws.network.default"); ok {
			dirs = append(dirs, entry.Dir())
		}
	}
	for _, dir := range dirs {
		for _, action := range TemplateActions(provider, dir) {
			seen[action] = true
		}
	}
	actions := make([]string, 0, len(seen))
	for action := range seen {
		actions = append(actions, action)
	}
	sort.Strings(actions)
	return actions
}

// copyTemplates resolves and copies the selected template directories plus the
// shared helper directory into the payload, returning the manifest refs.
func copyTemplates(files map[string][]byte, provider TemplateProvider, plan []plannedAsset, needsDefault bool) ([]TemplateRef, error) {
	refs := make([]TemplateRef, 0, len(plan)+1)
	copied := map[string]bool{}

	copyDir := func(entry *TemplateEntry) error {
		dir := entry.Dir()
		if copied[dir] {
			return nil
		}
		copied[dir] = true
		templateFiles, source, err := provider.ResolveDir(dir)
		if err != nil {
			return fmt.Errorf("resolve template %s: %w", entry.ID, err)
		}
		refs = append(refs, TemplateRef{
			ID:      entry.ID,
			Version: entry.Version,
			Export:  entry.Export,
			Module:  entry.Module,
			Digest:  TemplateDigest(templateFiles),
			Source:  source,
		})
		for rel, raw := range templateFiles {
			files[path.Join("templates", dir, rel)] = raw
		}
		return nil
	}

	for i := range plan {
		if err := copyDir(plan[i].entry); err != nil {
			return nil, err
		}
	}
	if needsDefault {
		entry, ok := provider.Catalog().Entry("aws.network.default")
		if !ok {
			return nil, fmt.Errorf("catalog is missing the implicit network template aws.network.default")
		}
		if err := copyDir(entry); err != nil {
			return nil, err
		}
	}
	sharedFiles, _, err := provider.ResolveDir("_shared")
	if err != nil {
		return nil, fmt.Errorf("resolve shared templates: %w", err)
	}
	for rel, raw := range sharedFiles {
		files[path.Join("templates", "_shared", rel)] = raw
	}
	return refs, nil
}

func assignVarNames(plan []plannedAsset) error {
	used := map[string]bool{"stack": true, "app": true, "cdk": true, "defaultVpc": true}
	for i := range plan {
		name := tsVar(plan[i].asset.Name)
		if name == "" {
			name = "asset"
		}
		candidate := name
		for suffix := 2; used[candidate]; suffix++ {
			candidate = fmt.Sprintf("%s%d", name, suffix)
		}
		used[candidate] = true
		plan[i].varName = candidate
	}
	return nil
}

// validateRefs checks cross-asset references resolve to generated assets that
// appear earlier in the plan (dependencies first), mirroring the historical
// synth-time errors with earlier generation-time errors.
func validateRefs(plan []plannedAsset) error {
	indexByName := map[string]int{}
	for i := range plan {
		indexByName[plan[i].asset.Name] = i
	}
	for i := range plan {
		item := &plan[i]
		switch item.asset.Type {
		case assetdomain.TypeCompute:
			for _, mount := range mapSlice(item.props["volumeMounts"]) {
				volumeName := strings.TrimSpace(str(mount["volume"]))
				if volumeName == "" {
					continue
				}
				index, ok := indexByName[volumeName]
				if !ok || index >= i {
					return fmt.Errorf("asset %s: volume %s must be generated before it", item.asset.Name, volumeName)
				}
				if plan[index].asset.Type != assetdomain.TypeVolume {
					return fmt.Errorf("asset %s: %s is not a Volume asset", item.asset.Name, volumeName)
				}
			}
		case assetdomain.TypeLoadBalancer, assetdomain.TypeObservability:
			for _, target := range stringSlice(item.props["targets"]) {
				index, ok := indexByName[target]
				if !ok || index >= i {
					return fmt.Errorf("asset %s: target compute %s must be generated before it", item.asset.Name, target)
				}
				if plan[index].asset.Type != assetdomain.TypeCompute {
					return fmt.Errorf("asset %s: target %s is not a Compute asset", item.asset.Name, target)
				}
			}
		}
	}
	return nil
}

// resolveNetwork returns the variable holding the intent network. When no
// Network asset exists but a selected template requires one, the implicit
// default network is used.
func resolveNetwork(plan []plannedAsset) (string, bool) {
	for i := range plan {
		if plan[i].asset.Type == assetdomain.TypeNetwork {
			return plan[i].varName, false
		}
	}
	for i := range plan {
		for _, requirement := range plan[i].entry.Requires {
			if requirement == "network" {
				return "defaultVpc", true
			}
		}
	}
	return "", false
}

func renderStack(in Input, stackName string, plan []plannedAsset, networkVar string, needsDefault bool) string {
	var b strings.Builder
	b.WriteString("import * as cdk from 'aws-cdk-lib';\n")

	imports := map[string]string{}
	for i := range plan {
		imports[plan[i].entry.Module] = plan[i].entry.Export
	}
	if needsDefault {
		imports["network.default/construct"] = "networkDefault"
	}
	modules := make([]string, 0, len(imports))
	for module := range imports {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	for _, module := range modules {
		fmt.Fprintf(&b, "import { %s } from '../templates/%s';\n", imports[module], module)
	}

	b.WriteString("\nexport function buildStack(app: cdk.App): cdk.Stack {\n")
	fmt.Fprintf(&b, "  const stack = new cdk.Stack(app, %s, {\n", quote(stackName))
	fmt.Fprintf(&b, "    env: { account: %s, region: %s },\n", quote(orDefault(in.Account, "default")), quote(orDefault(in.Region, "default")))
	fmt.Fprintf(&b, "    description: %s,\n", quote("Guardian generated stack for "+in.Partition+"/"+in.Intent))
	b.WriteString("    tags: {\n")
	b.WriteString("      'guardian-managed': 'true',\n")
	fmt.Fprintf(&b, "      'guardian-partition': %s,\n", quote(in.Partition))
	fmt.Fprintf(&b, "      'guardian-intent': %s,\n", quote(in.Intent))
	b.WriteString("    },\n")
	b.WriteString("  });\n\n")

	if needsDefault {
		b.WriteString("  const defaultVpc = networkDefault(stack);\n\n")
	}

	clusterVars := map[string]string{}
	for i := range plan {
		if plan[i].asset.Type != assetdomain.TypeCompute {
			continue
		}
		clusterName := str(plan[i].generated["cluster"])
		if _, ok := clusterVars[clusterName]; !ok {
			clusterVars[clusterName] = plan[i].varName
		}
	}

	for i := range plan {
		item := &plan[i]
		refs := renderRefs(item, plan, networkVar, clusterVars)
		propsJSON := strings.TrimSpace(string(mustJSON(item.props)))
		generatedJSON := strings.TrimSpace(string(mustJSON(item.generated)))
		fmt.Fprintf(&b, "  const %s = %s(stack, %s, {\n", item.varName, item.entry.Export, quote(item.asset.Name))
		fmt.Fprintf(&b, "    properties: %s,\n", propsJSON)
		fmt.Fprintf(&b, "    generated: %s,\n", generatedJSON)
		fmt.Fprintf(&b, "    refs: %s,\n", refs)
		fmt.Fprintf(&b, "    partition: %s,\n", quote(in.Partition))
		fmt.Fprintf(&b, "    intent: %s,\n", quote(in.Intent))
		if in.Account != "" {
			fmt.Fprintf(&b, "    account: %s,\n", quote(in.Account))
		}
		if in.Region != "" {
			fmt.Fprintf(&b, "    region: %s,\n", quote(in.Region))
		}
		b.WriteString("  });\n\n")
	}

	b.WriteString("  return stack;\n")
	b.WriteString("}\n")
	return b.String()
}

// renderRefs renders the TS refs object literal for one asset: handle wiring
// to previously instantiated templates.
func renderRefs(item *plannedAsset, plan []plannedAsset, networkVar string, clusterVars map[string]string) string {
	parts := make([]string, 0, 3)
	add := func(key, value string) { parts = append(parts, key+": "+value) }

	varByName := func(name string) (string, bool) {
		for i := range plan {
			if plan[i].asset.Name == name {
				return plan[i].varName, true
			}
		}
		return "", false
	}

	switch item.asset.Type {
	case assetdomain.TypeVolume, assetdomain.TypeCompute, assetdomain.TypeLoadBalancer,
		assetdomain.TypeDatabase, assetdomain.TypeSQLDatabase:
		if networkVar != "" {
			add("network", networkVar)
		}
	}
	switch item.asset.Type {
	case assetdomain.TypeCompute:
		volumeRefs := make([]string, 0, 2)
		for _, mount := range mapSlice(item.props["volumeMounts"]) {
			volumeName := strings.TrimSpace(str(mount["volume"]))
			if volumeName == "" {
				continue
			}
			if varName, ok := varByName(volumeName); ok {
				volumeRefs = append(volumeRefs, fmt.Sprintf("%s: %s", quote(volumeName), varName))
			}
		}
		if len(volumeRefs) > 0 {
			add("volumes", "{ "+strings.Join(volumeRefs, ", ")+" }")
		}
		clusterName := str(item.generated["cluster"])
		if first := clusterVars[clusterName]; first != "" && first != item.varName {
			add("cluster", first+".cluster")
		}
	case assetdomain.TypeLoadBalancer, assetdomain.TypeObservability:
		targets := make([]string, 0, 2)
		for _, name := range stringSlice(item.props["targets"]) {
			if varName, ok := varByName(name); ok {
				targets = append(targets, varName)
			}
		}
		add("targets", "["+strings.Join(targets, ", ")+"]")
	}

	if len(parts) == 0 {
		return "{}"
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func orderAssets(in Input) []assetdomain.Spec {
	byName := make(map[string]assetdomain.Spec, len(in.Assets))
	for _, asset := range in.Assets {
		byName[asset.Name] = asset
	}
	ordered := make([]assetdomain.Spec, 0, len(in.Assets))
	seen := make(map[string]bool, len(in.Assets))
	for _, name := range in.Order {
		asset, ok := byName[name]
		if !ok || seen[name] {
			continue
		}
		ordered = append(ordered, asset)
		seen[name] = true
	}
	rest := make([]string, 0, len(in.Assets))
	for _, asset := range in.Assets {
		if !seen[asset.Name] {
			rest = append(rest, asset.Name)
		}
	}
	sort.Strings(rest)
	for _, name := range rest {
		ordered = append(ordered, byName[name])
	}
	// renderRefs wires an implicit `network` reference into Volume, Compute,
	// LoadBalancer, Database and SQLDatabase assets, but the planner's
	// topological order only knows explicit dependsOn edges. Hoist the
	// intent's Network asset ahead of every asset that references it so the
	// generated stack.ts never uses it before declaring it (TS2448).
	for i := range ordered {
		if ordered[i].Type != assetdomain.TypeNetwork {
			continue
		}
		if i > 0 {
			network := ordered[i]
			copy(ordered[1:i+1], ordered[0:i])
			ordered[0] = network
		}
		break
	}
	return ordered
}

// normalizeProps converts planner values (which may contain map[string]any
// from YAML) into JSON-marshalable data and applies per-type defaults.
func normalizeProps(assetType string, props map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(props)
	if err != nil {
		return nil, fmt.Errorf("encode properties: %w", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode properties: %w", err)
	}
	if out == nil {
		out = map[string]any{}
	}
	switch assetType {
	case assetdomain.TypeCompute:
		if _, ok := out["replicas"]; !ok {
			out["replicas"] = 1
		}
	}
	return out, nil
}

func checkResolved(assetName string, props map[string]any) error {
	raw, err := json.Marshal(props)
	if err != nil {
		return err
	}
	if strings.Contains(string(raw), "${intent.") {
		return fmt.Errorf("asset %s has unresolved output references; cannot generate CDK stack yet", assetName)
	}
	return nil
}

// generatorProps computes deterministic physical names and resolved
// references for the templates so Go and TypeScript never disagree on naming.
func generatorProps(in Input, asset assetdomain.Spec) map[string]any {
	account := strings.TrimSpace(in.Account)
	if account == "" {
		account = "default"
	}
	region := strings.TrimSpace(in.Region)
	if region == "" {
		region = "default"
	}
	base := func(prefix string) string {
		return resourceName(fmt.Sprintf("aws-%s-%s-%s", prefix, account, region), in.Partition, in.Intent, asset.Name)
	}
	switch asset.Type {
	case assetdomain.TypeCompute:
		cluster := strings.TrimSpace(str(asset.Properties["cluster"]))
		if cluster == "" {
			// One cluster per partition/intent: a bare account id collides
			// across stacks in the same account (ECS cluster names are
			// account-wide).
			cluster = resourceName(fmt.Sprintf("aws-ecs-%s-%s", account, region), in.Partition, in.Intent, "")
		}
		configParams := map[string]string{}
		for _, mount := range mapSlice(asset.Properties["configMounts"]) {
			name := strings.TrimSpace(str(mount["config"]))
			if name == "" {
				continue
			}
			configParams[name] = ssmParameterName(account, region, in.Partition, in.Intent, name)
		}
		return map[string]any{
			"cluster":      cluster,
			"serviceName":  base("svc"),
			"taskFamily":   base("task"),
			"logGroup":     fmt.Sprintf("guardian/%s/%s/%s/%s", account, region, in.Partition, in.Intent),
			"configParams": configParams,
		}
	case assetdomain.TypeVolume:
		return map[string]any{"fileSystemName": base("efs")}
	case assetdomain.TypeObjectStore:
		return map[string]any{"bucketName": s3BucketName(account, region, in.Partition, in.Intent, asset.Name)}
	case assetdomain.TypeLoadBalancer:
		// Load balancer and target group names are limited to 32 characters,
		// so they must not carry the account/region prefix: truncation would
		// cut the partition/intent/asset suffix and collide across stacks in
		// the same account ("target group ... already exists").
		lbName := truncateName(resourceName("aws-lb", in.Partition, in.Intent, asset.Name), 32)
		names := make([]string, 0, len(listenerSpecs(asset.Properties)))
		for i := range listenerSpecs(asset.Properties) {
			names = append(names, truncateName(fmt.Sprintf("%s-tg%d", lbName, i), 32))
		}
		return map[string]any{
			"lbName":  lbName,
			"tgNames": names,
		}
	case assetdomain.TypeNetwork:
		return map[string]any{"vpcName": base("vpc")}
	case assetdomain.TypeDatabase, assetdomain.TypeSQLDatabase:
		return map[string]any{
			"clusterName":     dbClusterName(in.Partition, in.Intent, asset.Name),
			"subnetGroupName": truncateName(dbClusterName(in.Partition, in.Intent, asset.Name)+"-sng", 63),
		}
	case assetdomain.TypeObservability:
		return map[string]any{
			"namespace": fmt.Sprintf("guardian/%s/%s", in.Partition, in.Intent),
			"logGroup":  fmt.Sprintf("guardian/%s/%s/%s/%s/otel", account, region, in.Partition, in.Intent),
		}
	default:
		return map[string]any{}
	}
}

func stackName(partition, intent string) string {
	return sanitize(strings.Join([]string{"guardian", partition, intent}, "-"))
}

// StackName returns the deterministic CloudFormation stack name for a
// generated (spec.implementation=cdk) intent. It is exported so stack
// detection can match live stacks against intents.
func StackName(partition, intent string) string {
	return stackName(partition, intent)
}

func ssmParameterName(account, region, partition, intent, asset string) string {
	name := resourceName(fmt.Sprintf("aws-ssm-%s-%s", account, region), partition, intent, asset)
	return "/guardian/" + strings.ReplaceAll(name, "-", "/")
}

func s3BucketName(account, region, partition, intent, asset string) string {
	return resourceName(fmt.Sprintf("guardian-%s-%s", account, region), partition, intent, asset)
}

func dbClusterName(partition, intent, asset string) string {
	name := sanitize(strings.Join([]string{"g", partition, intent, asset}, "-"))
	if len(name) > 63 {
		name = name[:63]
	}
	return strings.Trim(name, "-")
}

// resourceName mirrors driverutil.ResourceName for the placement shape used by
// AWS resource names (prefix, partition, intent, asset).
func resourceName(prefix, partition, intent, asset string) string {
	parts := []string{prefix, partition, intent, asset}
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if sanitized := sanitize(part); sanitized != "" {
			filtered = append(filtered, sanitized)
		}
	}
	if len(filtered) == 0 {
		return "guardian"
	}
	return truncateName(strings.Join(filtered, "-"), 63)
}

func truncateName(name string, limit int) string {
	if len(name) <= limit {
		return name
	}
	if limit <= 0 {
		return ""
	}
	return strings.Trim(name[:limit], "-")
}

func sanitize(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	prevDash := false
	for _, r := range value {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if isAlphaNum {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func titleParts(value string) string {
	var b strings.Builder
	upperNext := true
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			if upperNext && r >= 'a' && r <= 'z' {
				r = r - ('a' - 'A')
			}
			b.WriteRune(r)
			upperNext = false
			continue
		}
		upperNext = true
	}
	return b.String()
}

// tsVar converts an asset name into a unique TS-safe camelCase identifier.
func tsVar(name string) string {
	var b strings.Builder
	upperNext := false
	for _, r := range name {
		isAlphaNum := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if !isAlphaNum {
			upperNext = true
			continue
		}
		if b.Len() == 0 && r >= '0' && r <= '9' {
			b.WriteByte('a')
		}
		if upperNext && r >= 'a' && r <= 'z' {
			r = r - ('a' - 'A')
		}
		upperNext = false
		b.WriteRune(r)
	}
	return b.String()
}

func listenerSpecs(props map[string]any) []map[string]any {
	return mapSlice(props["listeners"])
}

func mapSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(map[string]any); ok {
			out = append(out, typed)
		}
	}
	return out
}

func stringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if typed, ok := item.(string); ok && strings.TrimSpace(typed) != "" {
			out = append(out, typed)
		}
	}
	return out
}

func str(value any) string {
	if value == nil {
		return ""
	}
	if typed, ok := value.(string); ok {
		return typed
	}
	return fmt.Sprint(value)
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// quote renders a TypeScript single-quoted string literal.
func quote(value string) string {
	var b strings.Builder
	b.WriteByte('\'')
	for _, r := range value {
		switch r {
		case '\\', '\'':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('\'')
	return b.String()
}

func mustJSON(value any) []byte {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(raw, '\n')
}

func mustJSONString(value any) string {
	return string(mustJSON(value))
}

const tsconfigJSON = `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "lib": ["es2020"],
    "declaration": false,
    "strict": true,
    "noImplicitAny": false,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "resolveJsonModule": true,
    "typeRoots": ["./node_modules/@types"]
  },
  "include": ["bin/**/*.ts", "lib/**/*.ts", "templates/**/*.ts"]
}
`

const binAppTS = `import * as cdk from 'aws-cdk-lib';
import { buildStack } from '../lib/stack';

const app = new cdk.App();
buildStack(app);
app.synth();
`
