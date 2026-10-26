package cdktemplate

import "testing"

const relationsTemplate = `{
  "Resources": {
    "Cluster": {"Type": "AWS::ECS::Cluster", "Properties": {}},
    "TaskDef": {
      "Type": "AWS::ECS::TaskDefinition",
      "Properties": {
        "TaskRoleArn": {"Fn::GetAtt": ["TaskRole", "Arn"]},
        "ContainerDefinitions": [{
          "LogConfiguration": {"Options": {"awslogs-group": {"Ref": "ServiceLogs"}}}
        }]
      }
    },
    "Service": {
      "Type": "AWS::ECS::Service",
      "Properties": {
        "Cluster": {"Ref": "Cluster"},
        "TaskDefinition": {"Ref": "TaskDef"},
        "LoadBalancers": [{"TargetGroupArn": {"Ref": "TargetGroup"}}],
        "NetworkConfiguration": {"AwsvpcConfiguration": {"Subnets": [{"Ref": "Subnet"}]}}
      }
    },
    "LoadBalancer": {"Type": "AWS::ElasticLoadBalancingV2::LoadBalancer", "Properties": {}},
    "Listener": {
      "Type": "AWS::ElasticLoadBalancingV2::Listener",
      "Properties": {
        "LoadBalancerArn": {"Ref": "LoadBalancer"},
        "DefaultActions": [{"Type": "forward", "TargetGroupArn": {"Ref": "TargetGroup"}}]
      }
    },
    "TargetGroup": {"Type": "AWS::ElasticLoadBalancingV2::TargetGroup", "Properties": {}},
    "Queue": {"Type": "AWS::SQS::Queue", "Properties": {}},
    "EventMapping": {
      "Type": "AWS::Lambda::EventSourceMapping",
      "Properties": {
        "FunctionName": {"Ref": "Function"},
        "EventSourceArn": {"Fn::GetAtt": ["Queue", "Arn"]}
      }
    },
    "Function": {
      "Type": "AWS::Lambda::Function",
      "Properties": {
        "Role": {"Fn::GetAtt": ["FunctionRole", "Arn"]},
        "LoggingConfig": {"LogGroup": {"Ref": "FunctionLogs"}},
        "Environment": {"Variables": {"TABLE": {"Ref": "Table"}}}
      }
    },
    "FunctionRole": {"Type": "AWS::IAM::Role", "Properties": {}},
    "FunctionLogs": {"Type": "AWS::Logs::LogGroup", "Properties": {}},
    "ServiceLogs": {"Type": "AWS::Logs::LogGroup", "Properties": {}},
    "TaskRole": {"Type": "AWS::IAM::Role", "Properties": {}},
    "Table": {"Type": "AWS::DynamoDB::Table", "Properties": {}},
    "Vpc": {"Type": "AWS::EC2::VPC", "Properties": {}},
    "Subnet": {"Type": "AWS::EC2::Subnet", "Properties": {"VpcId": {"Ref": "Vpc"}}}
  }
}`

func TestInventoryExtractsSemanticRelations(t *testing.T) {
	resources, err := Inventory(relationsTemplate)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	byID := map[string]Resource{}
	for _, resource := range resources {
		byID[resource.LogicalID] = resource
	}

	if got := byID["Service"].Parent; got != "Cluster" {
		t.Fatalf("Service parent = %q, want Cluster", got)
	}
	if got := byID["Listener"].Parent; got != "LoadBalancer" {
		t.Fatalf("Listener parent = %q, want LoadBalancer", got)
	}
	if got := byID["EventMapping"].Parent; got != "Function" {
		t.Fatalf("EventMapping parent = %q, want Function", got)
	}
	if got := byID["Subnet"].Parent; got != "Vpc" {
		t.Fatalf("Subnet parent = %q, want Vpc", got)
	}

	hasRelation := func(resource, kind, target string) bool {
		for _, relation := range byID[resource].Relations {
			if relation.Kind == kind && relation.Target == target {
				return true
			}
		}
		return false
	}
	for _, check := range []struct {
		resource string
		kind     string
		target   string
	}{
		{"Service", "uses", "TaskDef"},
		{"Service", "network", "Subnet"},
		{"TaskDef", "uses", "TaskRole"},
		{"TaskDef", "logsTo", "ServiceLogs"},
		{"Listener", "routesTo", "TargetGroup"},
		{"TargetGroup", "routesTo", "Service"},
		{"Queue", "triggers", "Function"},
		{"Function", "uses", "FunctionRole"},
		{"Function", "uses", "Table"},
		{"Function", "logsTo", "FunctionLogs"},
	} {
		if !hasRelation(check.resource, check.kind, check.target) {
			t.Fatalf("%s missing relation %s -> %s: %+v", check.resource, check.kind, check.target, byID[check.resource].Relations)
		}
	}
}

func TestInventoryKeepsDependencyFallback(t *testing.T) {
	resources, err := Inventory(relationsTemplate)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	for _, resource := range resources {
		if resource.LogicalID == "Service" {
			found := false
			for _, dep := range resource.DependsOn {
				if dep == "Cluster" {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Service DependsOn lost Cluster: %v", resource.DependsOn)
			}
			return
		}
	}
	t.Fatal("Service not found")
}
