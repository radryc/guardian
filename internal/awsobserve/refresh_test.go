package awsobserve

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	cftypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"
)

func TestStackStateFromSDKIncludesTagsAndOutputs(t *testing.T) {
	state := stackStateFromSDK("123", "eu-west-1", cftypes.Stack{
		StackId:     aws.String("arn:aws:cloudformation:eu-west-1:123:stack/CustomerAppStack/abc"),
		StackName:   aws.String("CustomerAppStack"),
		StackStatus: cftypes.StackStatusCreateComplete,
		Tags: []cftypes.Tag{
			{Key: aws.String("guardian.io/observe"), Value: aws.String("true")},
			{Key: aws.String("guardian.io/partition"), Value: aws.String("customer-prod-1")},
			{Key: aws.String(""), Value: aws.String("ignored")},
		},
		Outputs: []cftypes.Output{
			{OutputKey: aws.String("ServiceUrl"), OutputValue: aws.String("https://api.example.com")},
			{OutputKey: aws.String(""), OutputValue: aws.String("ignored")},
		},
	})
	if state.Tags["guardian.io/partition"] != "customer-prod-1" {
		t.Fatalf("tags = %#v", state.Tags)
	}
	if _, ok := state.Tags[""]; ok {
		t.Fatalf("empty tag key was kept: %#v", state.Tags)
	}
	if state.Outputs["ServiceUrl"] != "https://api.example.com" {
		t.Fatalf("outputs = %#v", state.Outputs)
	}
	if _, ok := state.Outputs[""]; ok {
		t.Fatalf("empty output key was kept: %#v", state.Outputs)
	}
}
