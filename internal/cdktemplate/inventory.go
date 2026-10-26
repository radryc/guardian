package cdktemplate

import (
	"sort"
	"strings"
)

// Resource describes one logical CloudFormation resource in a template. It is
// the identity/dependency subset Guardian needs to build the observed resource
// inventory; properties stay in the template and are never copied into state.
type Resource struct {
	LogicalID string     `json:"logicalId" yaml:"logicalId"`
	Type      string     `json:"type" yaml:"type"`
	Condition string     `json:"condition,omitempty" yaml:"condition,omitempty"`
	DependsOn []string   `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Parent    string     `json:"parent,omitempty" yaml:"parent,omitempty"`
	Relations []Relation `json:"relations,omitempty" yaml:"relations,omitempty"`
}

// Inventory returns every resource in a CloudFormation template (JSON or YAML)
// sorted by logical ID. DependsOn contains only references to other resources
// in the same template, derived from DependsOn, Ref, Fn::GetAtt and Fn::Sub.
func Inventory(raw string) ([]Resource, error) {
	template, err := parseTemplate(raw)
	if err != nil {
		return nil, err
	}
	resources := resourcesOf(template)
	ids := make([]string, 0, len(resources))
	for id := range resources {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	out := make([]Resource, 0, len(ids))
	parents := make(map[string]string)
	relations := make(map[string][]Relation)
	for _, id := range ids {
		resource := resources[id]
		refs := map[string]struct{}{}
		collectDependsOn(resource["DependsOn"], refs)
		collectRefs(resource["Properties"], refs)
		delete(refs, id)

		deps := make([]string, 0, len(refs))
		for ref := range refs {
			if _, ok := resources[ref]; !ok {
				continue
			}
			deps = append(deps, ref)
		}
		sort.Strings(deps)
		out = append(out, Resource{
			LogicalID: id,
			Type:      stringValue(resource["Type"]),
			Condition: stringValue(resource["Condition"]),
			DependsOn: deps,
		})
		applyTemplateRelations(id, resource, resources, parents, relations)
	}
	for i := range out {
		out[i].Parent = parents[out[i].LogicalID]
		out[i].Relations = dedupeRelations(relations[out[i].LogicalID], resources)
	}
	return out, nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func collectDependsOn(value any, out map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		if ref := strings.TrimSpace(typed); ref != "" {
			out[ref] = struct{}{}
		}
	case []any:
		for _, item := range typed {
			collectDependsOn(item, out)
		}
	case []string:
		for _, item := range typed {
			if ref := strings.TrimSpace(item); ref != "" {
				out[ref] = struct{}{}
			}
		}
	}
}

func collectRefs(value any, out map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			collectIntrinsic(key, item, out)
		}
	case map[any]any:
		for key, item := range typed {
			name, _ := key.(string)
			collectIntrinsic(name, item, out)
		}
	case []any:
		for _, item := range typed {
			collectRefs(item, out)
		}
	}
}

func collectIntrinsic(name string, value any, out map[string]struct{}) {
	switch name {
	case "Ref":
		if ref, ok := value.(string); ok {
			if ref = strings.TrimSpace(ref); ref != "" {
				out[ref] = struct{}{}
			}
		}
	case "Fn::GetAtt":
		collectGetAtt(value, out)
	case "Fn::Sub":
		collectSubRefs(value, out)
	default:
		collectRefs(value, out)
	}
}

func collectGetAtt(value any, out map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		if ref := strings.TrimSpace(strings.SplitN(typed, ".", 2)[0]); ref != "" {
			out[ref] = struct{}{}
		}
	case []any:
		if len(typed) == 0 {
			return
		}
		if ref, ok := typed[0].(string); ok {
			if ref = strings.TrimSpace(ref); ref != "" {
				out[ref] = struct{}{}
			}
		}
	}
}

func collectSubRefs(value any, out map[string]struct{}) {
	var template string
	switch typed := value.(type) {
	case string:
		template = typed
	case []any:
		if len(typed) > 0 {
			if text, ok := typed[0].(string); ok {
				template = text
			}
		}
		if len(typed) > 1 {
			collectRefs(typed[1], out)
		}
	}
	for {
		start := strings.Index(template, "${")
		if start < 0 {
			return
		}
		rest := template[start+2:]
		end := strings.Index(rest, "}")
		if end < 0 {
			return
		}
		ref := strings.TrimSpace(rest[:end])
		if dot := strings.Index(ref, "."); dot >= 0 {
			ref = ref[:dot]
		}
		if ref != "" && !strings.HasPrefix(ref, "!") {
			out[ref] = struct{}{}
		}
		template = rest[end+1:]
	}
}
