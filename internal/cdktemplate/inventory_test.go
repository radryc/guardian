package cdktemplate

import (
	"reflect"
	"testing"
)

func TestInventoryDerivesResourceDependencies(t *testing.T) {
	const template = `{
	  "Parameters": {"Env": {"Type": "String"}},
	  "Resources": {
	    "Table": {"Type": "AWS::DynamoDB::Table"},
	    "Queue": {"Type": "AWS::SQS::Queue", "Properties": {"Tags": [{"Key": "Env", "Value": {"Ref": "Env"}}]}},
	    "Role": {"Type": "AWS::IAM::Role"},
	    "Function": {
	      "Type": "AWS::Lambda::Function",
	      "Condition": "CreateFunction",
	      "DependsOn": ["Role"],
	      "Properties": {
	        "Environment": {"Variables": {
	          "TABLE": {"Ref": "Table"},
	          "QUEUE": {"Fn::GetAtt": ["Queue", "Arn"]},
	          "NAME": {"Fn::Sub": "${AWS::StackName}-${Table}-${!Literal}"}
	        }}
	      }
	    }
	  }
	}`

	resources, err := Inventory(template)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(resources) != 4 {
		t.Fatalf("resources = %+v, want 4", resources)
	}
	byID := map[string]Resource{}
	for _, resource := range resources {
		byID[resource.LogicalID] = resource
	}
	fn := byID["Function"]
	if fn.Type != "AWS::Lambda::Function" || fn.Condition != "CreateFunction" {
		t.Fatalf("function = %+v", fn)
	}
	want := []string{"Queue", "Role", "Table"}
	if !reflect.DeepEqual(fn.DependsOn, want) {
		t.Fatalf("function dependsOn = %v, want %v", fn.DependsOn, want)
	}
	if got := byID["Queue"].DependsOn; len(got) != 0 {
		t.Fatalf("queue dependsOn = %v, want none (parameter refs are ignored)", got)
	}
	if resources[0].LogicalID != "Function" {
		t.Fatalf("resources not sorted by logical ID: %+v", resources)
	}
}

func TestInventoryAcceptsYAML(t *testing.T) {
	const template = `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  Bucket:
    Type: AWS::S3::Bucket
  Policy:
    Type: AWS::S3::BucketPolicy
    Properties:
      Bucket: !Ref Bucket
`
	resources, err := Inventory(template)
	if err != nil {
		t.Fatalf("Inventory: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %+v, want 2", resources)
	}
	if !reflect.DeepEqual(resources[1].DependsOn, []string{"Bucket"}) {
		t.Fatalf("policy dependsOn = %v", resources[1].DependsOn)
	}
}

func TestInventoryRejectsInvalidTemplate(t *testing.T) {
	if _, err := Inventory("not: [valid"); err == nil {
		t.Fatal("expected error")
	}
}
