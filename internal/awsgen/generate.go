package awsgen

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rydzu/ainfra/guardian/internal/awsscan"
	assetdomain "github.com/rydzu/ainfra/guardian/internal/domain/asset"
	assetdefs "github.com/rydzu/ainfra/guardian/internal/domain/assets"
	intentdomain "github.com/rydzu/ainfra/guardian/internal/domain/intent"
	partitiondomain "github.com/rydzu/ainfra/guardian/internal/domain/partition"
	targetdomain "github.com/rydzu/ainfra/guardian/internal/domain/target"
	"github.com/rydzu/ainfra/guardian/internal/paths"
	"github.com/rydzu/ainfra/guardian/pkg/guardianapi"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion       = "guardian/v1alpha1"
	GroupingStack    = "stack"
	GroupingResource = "resource"
	defaultInterval  = "10m"
	importNamePrefix = "import-"
	maxNameLen       = 40
)

const (
	KindBucket       = "bucket"
	KindFileSystem   = "fileSystem"
	KindParameter    = "parameter"
	KindSecret       = "secret"
	KindService      = "service"
	KindLoadBalancer = "loadBalancer"
	KindStack        = "stack"
	KindInventory    = "inventory"
)

var allImportKinds = []string{KindBucket, KindFileSystem, KindParameter, KindSecret, KindService, KindLoadBalancer}

type Options struct {
	PartitionName string
	Grouping      string
	IncludeTypes  []string
	Regions       []string
	Stacks        []string
	TargetPusher  string
}

type ManagedEntry struct {
	Kind           string `json:"kind"`
	Identifier     string `json:"identifier"`
	Region         string `json:"region,omitempty"`
	Partition      string `json:"partition,omitempty"`
	Intent         string `json:"intent,omitempty"`
	Asset          string `json:"asset,omitempty"`
	ExistingIntent bool   `json:"existingIntent"`
}

type UnmappedEntry struct {
	Kind         string `json:"kind"`
	ResourceType string `json:"resourceType,omitempty"`
	Identifier   string `json:"identifier"`
	Region       string `json:"region,omitempty"`
	Reason       string `json:"reason"`
}

type Draft struct {
	Partition partitiondomain.Partition `json:"partition"`
	Intents   []intentdomain.Intent     `json:"intents"`
	Managed   []ManagedEntry            `json:"managed"`
	Unmapped  []UnmappedEntry           `json:"unmapped"`
	Warnings  []string                  `json:"warnings"`
}

type resourceGroup struct {
	baseName string
	region   string
	assets   []assetdomain.Spec
}

func Generate(result *awsscan.ScanResult, opts Options, store guardianapi.ReadStore) (*Draft, error) {
	if result == nil {
		return nil, fmt.Errorf("scan result is required")
	}
	if strings.TrimSpace(opts.PartitionName) == "" {
		return nil, fmt.Errorf("partition name is required")
	}
	if result.Status != awsscan.ScanStatusSucceeded {
		return nil, fmt.Errorf("scan %s has status %s, not %s", result.ScanID, result.Status, awsscan.ScanStatusSucceeded)
	}

	grouping := strings.ToLower(strings.TrimSpace(opts.Grouping))
	if grouping == "" {
		grouping = GroupingStack
	}
	if grouping != GroupingStack && grouping != GroupingResource {
		return nil, fmt.Errorf("unsupported grouping %q (use %s or %s)", opts.Grouping, GroupingStack, GroupingResource)
	}

	account := strings.TrimSpace(result.Account)
	pusher := strings.TrimSpace(opts.TargetPusher)
	if pusher == "" {
		pusher = strings.TrimSpace(result.Pusher)
	}
	if pusher == "" {
		return nil, fmt.Errorf("target pusher is required")
	}

	importKinds := make(map[string]struct{})
	if len(opts.IncludeTypes) == 0 {
		for _, kind := range allImportKinds {
			importKinds[kind] = struct{}{}
		}
	} else {
		for _, kind := range opts.IncludeTypes {
			importKinds[strings.ToLower(strings.TrimSpace(kind))] = struct{}{}
		}
	}

	regionFilter := make(map[string]struct{})
	for _, region := range opts.Regions {
		if name := strings.ToLower(strings.TrimSpace(region)); name != "" {
			regionFilter[name] = struct{}{}
		}
	}
	inRegion := func(region string) bool {
		if len(regionFilter) == 0 {
			return true
		}
		_, ok := regionFilter[strings.ToLower(strings.TrimSpace(region))]
		return ok
	}
	kindEnabled := func(kind string) bool {
		_, ok := importKinds[kind]
		return ok
	}

	stackFilter := make(map[string]struct{})
	for _, stack := range opts.Stacks {
		if name := strings.TrimSpace(stack); name != "" {
			stackFilter[name] = struct{}{}
		}
	}
	inStack := func(stack string) bool {
		if len(stackFilter) == 0 {
			return true
		}
		_, ok := stackFilter[strings.TrimSpace(stack)]
		return ok
	}

	draft := &Draft{}
	if count := len(result.Errors); count > 0 {
		draft.Warnings = append(draft.Warnings, fmt.Sprintf("scan reported %d errors; affected resources may be missing", count))
	}

	stacksByPhysical := buildStacksByPhysical(result)
	existing, err := loadExistingAdoptions(store, opts.PartitionName)
	if err != nil {
		return nil, err
	}

	groups := make(map[string]*resourceGroup)
	take := func(kind, identifier, region, stack string) *resourceGroup {
		base := importNamePrefix + sanitizeName(kind) + "-" + sanitizeName(identifier)
		if grouping == GroupingStack && strings.TrimSpace(stack) != "" {
			base = importNamePrefix + sanitizeName(stack)
		}
		key := base + "|" + strings.ToLower(strings.TrimSpace(region))
		group, ok := groups[key]
		if !ok {
			group = &resourceGroup{baseName: base, region: strings.TrimSpace(region)}
			groups[key] = group
		}
		return group
	}

	addAsset := func(kind, identifier, region, stack string, asset assetdomain.Spec) {
		group := take(kind, identifier, region, stack)
		asset.Name = uniqueAssetName(group, asset.Name)
		group.assets = append(group.assets, asset)
	}

	for _, bucket := range result.Buckets {
		if !inRegion(bucket.Region) {
			continue
		}
		if bucket.Managed.Managed || existing.buckets[bucket.Name] {
			draft.Managed = append(draft.Managed, managedEntry(KindBucket, bucket.Name, bucket.Region, bucket.Managed, existing.buckets[bucket.Name]))
			continue
		}
		if !kindEnabled(KindBucket) {
			continue
		}
		stack := bucket.Stack
		if stack == "" {
			stack = stacksByPhysical[bucket.Region+"/"+bucket.Name]
		}
		if !inStack(stack) {
			continue
		}
		asset := assetdomain.Spec{
			Type: assetdomain.TypeObjectStore,
			Name: sanitizeName(bucket.Name),
			Properties: map[string]any{
				"engine":         "s3",
				"existingBucket": bucket.Name,
				"region":         bucket.Region,
				"versioning":     bucket.Versioned,
			},
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindBucket, bucket.Name, bucket.Region, stack, asset)
	}

	for _, fs := range result.FileSystems {
		if !inRegion(fs.Region) {
			continue
		}
		if fs.Managed.Managed || existing.fileSystems[fs.ID] {
			draft.Managed = append(draft.Managed, managedEntry(KindFileSystem, fs.ID, fs.Region, fs.Managed, existing.fileSystems[fs.ID]))
			continue
		}
		if fs.LifeCycleState != "" && !strings.EqualFold(fs.LifeCycleState, "available") {
			draft.Unmapped = append(draft.Unmapped, UnmappedEntry{
				Kind: KindFileSystem, Identifier: fs.ID, Region: fs.Region,
				Reason: fmt.Sprintf("EFS filesystem state is %s", fs.LifeCycleState),
			})
			continue
		}
		if !kindEnabled(KindFileSystem) {
			continue
		}
		stack := fs.Stack
		if stack == "" {
			stack = stacksByPhysical[fs.Region+"/"+fs.ID]
		}
		if !inStack(stack) {
			continue
		}
		assetName := sanitizeName(fs.Name)
		if assetName == "" {
			assetName = sanitizeName(fs.ID)
		}
		asset := assetdomain.Spec{
			Type: assetdomain.TypeVolume,
			Name: assetName,
			Properties: map[string]any{
				"existingID": fs.ID,
			},
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindFileSystem, fs.ID, fs.Region, stack, asset)
	}

	for _, param := range result.Parameters {
		if !inRegion(param.Region) {
			continue
		}
		if param.Managed.Managed || existing.parameters[param.Name] {
			draft.Managed = append(draft.Managed, managedEntry(KindParameter, param.Name, param.Region, param.Managed, existing.parameters[param.Name]))
			continue
		}
		if param.Type == "SecureString" {
			draft.Unmapped = append(draft.Unmapped, UnmappedEntry{
				Kind: KindParameter, Identifier: param.Name, Region: param.Region,
				Reason: "SecureString values cannot be imported as Config assets",
			})
			continue
		}
		if !kindEnabled(KindParameter) {
			continue
		}
		stack := param.Stack
		if stack == "" {
			stack = stacksByPhysical[param.Region+"/"+param.Name]
		}
		if !inStack(stack) {
			continue
		}
		asset := assetdomain.Spec{
			Type: assetdomain.TypeConfig,
			Name: sanitizeName(param.Name),
			Properties: map[string]any{
				"existingParameter": param.Name,
				"content":           param.Value,
			},
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindParameter, param.Name, param.Region, stack, asset)
	}

	for _, secret := range result.Secrets {
		if !inRegion(secret.Region) {
			continue
		}
		if secret.Managed.Managed || existing.secrets[secret.Name] {
			draft.Managed = append(draft.Managed, managedEntry(KindSecret, secret.Name, secret.Region, secret.Managed, existing.secrets[secret.Name]))
			continue
		}
		if !kindEnabled(KindSecret) {
			continue
		}
		stack := secret.Stack
		if stack == "" {
			stack = stacksByPhysical[secret.Region+"/"+secret.Name]
		}
		if !inStack(stack) {
			continue
		}
		asset := assetdomain.Spec{
			Type: assetdomain.TypeSecret,
			Name: sanitizeName(secret.Name),
			Properties: map[string]any{
				"existingSecret": secret.Name,
			},
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindSecret, secret.Name, secret.Region, stack, asset)
	}

	for _, svc := range result.Services {
		if !inRegion(svc.Region) {
			continue
		}
		serviceKey := svc.ClusterName + "/" + svc.Name
		if svc.Managed.Managed || existing.services[serviceKey] {
			draft.Managed = append(draft.Managed, managedEntry(KindService, serviceKey, svc.Region, svc.Managed, existing.services[serviceKey]))
			continue
		}
		if !kindEnabled(KindService) {
			continue
		}
		if strings.TrimSpace(svc.Image) == "" {
			draft.Unmapped = append(draft.Unmapped, UnmappedEntry{
				Kind: KindService, Identifier: serviceKey, Region: svc.Region,
				Reason: "task definition container image could not be resolved",
			})
			continue
		}
		stack := svc.Stack
		if stack == "" {
			stack = stacksByPhysical[svc.Region+"/"+svc.Name]
		}
		if !inStack(stack) {
			continue
		}
		properties := map[string]any{
			"image":               svc.Image,
			"observeExisting":     true,
			"existingServiceName": svc.Name,
			"cluster":             svc.ClusterName,
			"replicas":            svc.DesiredCount,
		}
		if len(svc.Command) > 0 {
			properties["command"] = svc.Command
		}
		if len(svc.Env) > 0 {
			env := make(map[string]any, len(svc.Env))
			for key, value := range svc.Env {
				env[key] = value
			}
			properties["env"] = env
		}
		if len(svc.Ports) > 0 {
			ports := make([]any, 0, len(svc.Ports))
			for _, port := range svc.Ports {
				entry := map[string]any{
					"containerPort": port.ContainerPort,
				}
				if port.Name != "" {
					entry["name"] = port.Name
				}
				if port.HostPort != 0 {
					entry["hostPort"] = port.HostPort
				}
				if port.Protocol != "" {
					entry["protocol"] = port.Protocol
				}
				ports = append(ports, entry)
			}
			properties["ports"] = ports
		}
		if svc.CPU > 0 || svc.Memory > 0 {
			limits := map[string]any{}
			if svc.CPU > 0 {
				limits["cpu"] = fmt.Sprintf("%dm", svc.CPU*1000/1024)
			}
			if svc.Memory > 0 {
				limits["memory"] = fmt.Sprintf("%d", svc.Memory)
			}
			properties["resources"] = map[string]any{"limits": limits}
		}
		asset := assetdomain.Spec{
			Type:       assetdomain.TypeCompute,
			Name:       sanitizeName(svc.Name),
			Properties: properties,
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindService, svc.Name, svc.Region, stack, asset)
	}

	for _, lb := range result.LoadBalancers {
		if !inRegion(lb.Region) {
			continue
		}
		if lb.Managed.Managed || existing.loadBalancers[lb.Name] {
			draft.Managed = append(draft.Managed, managedEntry(KindLoadBalancer, lb.Name, lb.Region, lb.Managed, existing.loadBalancers[lb.Name]))
			continue
		}
		if !kindEnabled(KindLoadBalancer) {
			continue
		}
		stack := lb.Stack
		if stack == "" {
			stack = stacksByPhysical[lb.Region+"/"+lb.Name]
		}
		if !inStack(stack) {
			continue
		}
		properties := map[string]any{
			"existingName": lb.Name,
			"serviceType":  "LoadBalancer",
		}
		if strings.EqualFold(lb.Scheme, "internal") {
			properties["serviceType"] = "ClusterIP"
		}
		if len(lb.Listeners) > 0 {
			listeners := make([]any, 0, len(lb.Listeners))
			for _, listener := range lb.Listeners {
				entry := map[string]any{"port": listener.Port}
				if listener.Protocol != "" {
					entry["protocol"] = strings.ToLower(listener.Protocol)
				}
				listeners = append(listeners, entry)
			}
			properties["listeners"] = listeners
		}
		asset := assetdomain.Spec{
			Type:       assetdomain.TypeLoadBalancer,
			Name:       sanitizeName(lb.Name),
			Properties: properties,
		}
		if !appendValidated(draft, asset) {
			continue
		}
		addAsset(KindLoadBalancer, lb.Name, lb.Region, stack, asset)
	}

	for _, stack := range result.Stacks {
		if !inRegion(stack.Region) {
			continue
		}
		if stack.Managed.Managed {
			draft.Managed = append(draft.Managed, managedEntry(KindStack, stack.Name, stack.Region, stack.Managed, false))
			continue
		}
		draft.Unmapped = append(draft.Unmapped, UnmappedEntry{
			Kind: KindStack, ResourceType: "AWS::CloudFormation::Stack", Identifier: stack.Name, Region: stack.Region,
			Reason: "CloudFormation stack without Guardian CDK source in the store",
		})
	}

	draft.Intents = buildIntents(groups, account, pusher)
	draft.Partition = buildPartition(opts.PartitionName, account, pusher, draft.Intents)

	sort.Slice(draft.Managed, func(i, j int) bool {
		if draft.Managed[i].Kind != draft.Managed[j].Kind {
			return draft.Managed[i].Kind < draft.Managed[j].Kind
		}
		return draft.Managed[i].Identifier < draft.Managed[j].Identifier
	})
	sort.Slice(draft.Unmapped, func(i, j int) bool {
		if draft.Unmapped[i].Kind != draft.Unmapped[j].Kind {
			return draft.Unmapped[i].Kind < draft.Unmapped[j].Kind
		}
		return draft.Unmapped[i].Identifier < draft.Unmapped[j].Identifier
	})
	return draft, nil
}

func appendValidated(draft *Draft, asset assetdomain.Spec) bool {
	if err := assetdefs.Validate(asset, assetdefs.ValidationContext{}); err != nil {
		draft.Warnings = append(draft.Warnings, fmt.Sprintf("skipped %s %q: %v", asset.Type, asset.Name, err))
		return false
	}
	return true
}

func managedEntry(kind, identifier, region string, info awsscan.ManagedInfo, existingIntent bool) ManagedEntry {
	entry := ManagedEntry{
		Kind:           kind,
		Identifier:     identifier,
		Region:         region,
		Partition:      info.Partition,
		Intent:         info.Intent,
		Asset:          info.Asset,
		ExistingIntent: existingIntent,
	}
	return entry
}

func buildStacksByPhysical(result *awsscan.ScanResult) map[string]string {
	out := make(map[string]string)
	for _, stack := range result.Stacks {
		for _, physicalID := range stack.ResourceIDs {
			if physicalID == "" {
				continue
			}
			out[stack.Region+"/"+physicalID] = stack.Name
		}
	}
	return out
}

type existingAdoptions struct {
	buckets       map[string]bool
	fileSystems   map[string]bool
	parameters    map[string]bool
	secrets       map[string]bool
	loadBalancers map[string]bool
	services      map[string]bool
}

func loadExistingAdoptions(store guardianapi.ReadStore, partition string) (*existingAdoptions, error) {
	out := &existingAdoptions{
		buckets:       map[string]bool{},
		fileSystems:   map[string]bool{},
		parameters:    map[string]bool{},
		secrets:       map[string]bool{},
		loadBalancers: map[string]bool{},
		services:      map[string]bool{},
	}
	if store == nil || strings.TrimSpace(partition) == "" {
		return out, nil
	}
	ctx := context.Background()
	entries, err := store.ListDir(ctx, paths.PartitionIntentsDir(partition))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return out, nil
		}
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir || (!strings.HasSuffix(entry.Name, ".yaml") && !strings.HasSuffix(entry.Name, ".yml")) {
			continue
		}
		raw, err := store.ReadFile(ctx, paths.PartitionIntentsDir(partition)+"/"+entry.Name)
		if err != nil {
			continue
		}
		var manifest intentdomain.Intent
		if err := yaml.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		for _, asset := range manifest.Spec.Assets {
			recordExistingAdoption(out, asset)
		}
	}
	return out, nil
}

func recordExistingAdoption(out *existingAdoptions, asset assetdomain.Spec) {
	switch asset.Type {
	case assetdomain.TypeObjectStore:
		if name, ok := stringProperty(asset.Properties, "existingBucket"); ok {
			out.buckets[name] = true
		}
	case assetdomain.TypeVolume:
		if id, ok := stringProperty(asset.Properties, "existingID"); ok {
			out.fileSystems[id] = true
		}
	case assetdomain.TypeConfig:
		if name, ok := stringProperty(asset.Properties, "existingParameter"); ok {
			out.parameters[name] = true
		}
	case assetdomain.TypeSecret:
		if name, ok := stringProperty(asset.Properties, "existingSecret"); ok {
			out.secrets[name] = true
		}
	case assetdomain.TypeLoadBalancer:
		if name, ok := stringProperty(asset.Properties, "existingName"); ok {
			out.loadBalancers[name] = true
		}
	case assetdomain.TypeCompute:
		name, hasName := stringProperty(asset.Properties, "existingServiceName")
		if !hasName {
			return
		}
		cluster, _ := stringProperty(asset.Properties, "cluster")
		out.services[cluster+"/"+name] = true
	}
}

func stringProperty(properties map[string]any, key string) (string, bool) {
	value, ok := properties[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func uniqueAssetName(group *resourceGroup, name string) string {
	if name == "" {
		name = "asset"
	}
	used := make(map[string]struct{}, len(group.assets))
	for _, asset := range group.assets {
		used[asset.Name] = struct{}{}
	}
	if _, taken := used[name]; !taken {
		return name
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, taken := used[candidate]; !taken {
			return candidate
		}
	}
}

func buildIntents(groups map[string]*resourceGroup, account, pusher string) []intentdomain.Intent {
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	nameCount := make(map[string]int)
	for _, key := range keys {
		nameCount[groups[key].baseName]++
	}
	usedNames := make(map[string]struct{})
	intents := make([]intentdomain.Intent, 0, len(groups))
	for _, key := range keys {
		group := groups[key]
		if len(group.assets) == 0 {
			continue
		}
		intentName := group.baseName
		if nameCount[group.baseName] > 1 {
			intentName = group.baseName + "-" + sanitizeName(group.region)
		}
		for i := 2; ; i++ {
			if _, taken := usedNames[intentName]; !taken {
				break
			}
			intentName = fmt.Sprintf("%s-%d", group.baseName, i)
		}
		usedNames[intentName] = struct{}{}

		sort.Slice(group.assets, func(i, j int) bool { return group.assets[i].Name < group.assets[j].Name })
		intent := intentdomain.Intent{
			APIVersion: APIVersion,
			Kind:       "Intent",
			Metadata:   intentdomain.Metadata{Name: intentName},
			Spec: intentdomain.IntentSpec{
				IntentType:   "standard",
				TargetPusher: pusher,
				Target: targetdomain.Placement{
					Account: account,
					Region:  group.region,
				},
				Assets: group.assets,
			},
		}
		intents = append(intents, intent)
	}
	return intents
}

func buildPartition(name, account, pusher string, intents []intentdomain.Intent) partitiondomain.Partition {
	defaultRegion := ""
	regions := make(map[string]struct{})
	for _, intent := range intents {
		if intent.Spec.Target.Region != "" {
			regions[intent.Spec.Target.Region] = struct{}{}
		}
	}
	if len(regions) == 1 {
		for region := range regions {
			defaultRegion = region
		}
	}
	partition := partitiondomain.Partition{
		APIVersion: APIVersion,
		Kind:       "Partition",
		Metadata:   partitiondomain.Metadata{Name: strings.TrimSpace(name)},
		Spec: partitiondomain.Spec{
			DeletionPolicy: "orphan",
			Reconciliation: partitiondomain.ReconciliationSpec{
				Mode:     "auto",
				Interval: defaultInterval,
			},
			Defaults: partitiondomain.PartitionDefaults{
				TargetPusher: pusher,
				Target: targetdomain.Placement{
					Account: account,
					Region:  defaultRegion,
				},
			},
		},
	}
	return partition
}

func sanitizeName(input string) string {
	value := strings.ToLower(strings.TrimSpace(input))
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
	if len(out) > maxNameLen {
		out = strings.Trim(out[:maxNameLen], "-")
	}
	return out
}
