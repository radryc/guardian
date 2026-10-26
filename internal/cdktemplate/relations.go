package cdktemplate

import "strings"

// Relation is a semantic edge derived from a CloudFormation resource's
// properties. Target is a logical resource ID in the same template.
type Relation struct {
	Kind   string `json:"kind" yaml:"kind"`
	Target string `json:"target" yaml:"target"`
	Label  string `json:"label,omitempty" yaml:"label,omitempty"`
}

// applyTemplateRelations adds containment and typed edges that are lost when a
// template is reduced to its DependsOn/Ref graph. It intentionally covers the
// application resources Guardian renders as topology; unsupported types keep
// only their dependency edges.
func applyTemplateRelations(id string, resource map[string]any, resources map[string]map[string]any, parents map[string]string, relations map[string][]Relation) {
	props, _ := resource["Properties"].(map[string]any)
	if props == nil {
		return
	}
	cfnType := stringValue(resource["Type"])
	setParent := func(parent string) {
		parent = strings.TrimSpace(parent)
		if parent == "" || parent == id {
			return
		}
		if _, ok := resources[parent]; ok {
			parents[id] = parent
		}
	}
	add := func(from, to, kind, label string) {
		to = strings.TrimSpace(to)
		if to == "" {
			return
		}
		if _, ok := resources[to]; !ok {
			return
		}
		if from == "" {
			from = id
		}
		if to == from {
			return
		}
		relations[from] = append(relations[from], Relation{Kind: kind, Target: to, Label: label})
	}

	switch cfnType {
	case "AWS::ECS::Service":
		setParent(refTarget(props["Cluster"]))
		add(id, refTarget(props["TaskDefinition"]), "uses", "task definition")
		for _, lb := range objectSlice(props["LoadBalancers"]) {
			tg := refTarget(lb["TargetGroupArn"])
			// Traffic flows from the target group to the ECS service, while the
			// template stores the reference on the service.
			add(tg, id, "routesTo", "service")
		}
		for _, ref := range refsIn(props["NetworkConfiguration"]) {
			add(id, ref, "network", "")
		}

	case "AWS::ECS::TaskDefinition":
		add(id, refTarget(props["TaskRoleArn"]), "uses", "task role")
		add(id, refTarget(props["ExecutionRoleArn"]), "uses", "execution role")
		for _, container := range objectSlice(props["ContainerDefinitions"]) {
			logConfig, _ := container["LogConfiguration"].(map[string]any)
			if logConfig == nil {
				continue
			}
			options, _ := logConfig["Options"].(map[string]any)
			add(id, refTarget(options["awslogs-group"]), "logsTo", "")
		}

	case "AWS::ElasticLoadBalancingV2::Listener":
		setParent(refTarget(props["LoadBalancerArn"]))
		for _, action := range objectSlice(props["DefaultActions"]) {
			add(id, refTarget(action["TargetGroupArn"]), "routesTo", "")
			forward, _ := action["ForwardConfig"].(map[string]any)
			for _, targetGroup := range objectSlice(forward["TargetGroups"]) {
				add(id, refTarget(targetGroup["TargetGroupArn"]), "routesTo", "")
			}
		}

	case "AWS::Lambda::EventSourceMapping":
		function := refTarget(props["FunctionName"])
		if function != "" {
			// Keep the mapping under the function rather than as a peer.
			setParent(function)
			add(refTarget(props["EventSourceArn"]), function, "triggers", "event source")
		}

	case "AWS::Lambda::Function":
		add(id, refTarget(props["Role"]), "uses", "execution role")
		environment, _ := props["Environment"].(map[string]any)
		variables, _ := environment["Variables"].(map[string]any)
		for _, value := range variables {
			for _, ref := range refsIn(value) {
				add(id, ref, "uses", "environment")
			}
		}
		logging, _ := props["LoggingConfig"].(map[string]any)
		add(id, refTarget(logging["LogGroup"]), "logsTo", "")

	case "AWS::EC2::Subnet":
		setParent(refTarget(props["VpcId"]))
	case "AWS::EC2::SecurityGroup", "AWS::EC2::RouteTable", "AWS::EC2::VPCEndpoint":
		setParent(refTarget(props["VpcId"]))
	}
}

func refTarget(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if ref, ok := typed["Ref"].(string); ok {
			return strings.TrimSpace(ref)
		}
		if raw, ok := typed["Fn::GetAtt"]; ok {
			switch detail := raw.(type) {
			case string:
				return strings.TrimSpace(strings.SplitN(detail, ".", 2)[0])
			case []any:
				if len(detail) > 0 {
					ref, _ := detail[0].(string)
					return strings.TrimSpace(ref)
				}
			}
		}
		if raw, ok := typed["Fn::Sub"]; ok {
			refs := refsIn(raw)
			if len(refs) == 1 {
				return refs[0]
			}
		}
	}
	return ""
}

func refsIn(value any) []string {
	out := map[string]struct{}{}
	collectRefs(value, out)
	refs := make([]string, 0, len(out))
	for ref := range out {
		refs = append(refs, ref)
	}
	sortStrings(refs)
	return refs
}

func objectSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if object, ok := item.(map[string]any); ok {
				out = append(out, object)
			}
		}
		return out
	case []map[string]any:
		return typed
	default:
		return nil
	}
}

func dedupeRelations(in []Relation, resources map[string]map[string]any) []Relation {
	if len(in) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]Relation, 0, len(in))
	for _, relation := range in {
		relation.Target = strings.TrimSpace(relation.Target)
		if relation.Target == "" || relation.Kind == "" {
			continue
		}
		if _, ok := resources[relation.Target]; !ok {
			continue
		}
		key := relation.Kind + "\x00" + relation.Target + "\x00" + relation.Label
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, relation)
	}
	if len(out) == 0 {
		return nil
	}
	sortRelations(out)
	return out
}

func sortRelations(relations []Relation) {
	for i := 1; i < len(relations); i++ {
		for j := i; j > 0 && relationLess(relations[j], relations[j-1]); j-- {
			relations[j], relations[j-1] = relations[j-1], relations[j]
		}
	}
}

func relationLess(a, b Relation) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Target != b.Target {
		return a.Target < b.Target
	}
	return a.Label < b.Label
}
