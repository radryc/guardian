package awsobserve

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const stackStatusEvent = `{
  "version": "0",
  "id": "6a7e8feb-b491-4cf7-a9f1-bf3703467718",
  "detail-type": "CloudFormation Stack Status Change",
  "source": "aws.cloudformation",
  "account": "111122223333",
  "time": "2017-12-22T18:43:48Z",
  "region": "us-west-1",
  "resources": ["arn:aws:cloudformation:us-west-1:111122223333:stack/teststack"],
  "detail": {
    "stack-id": "arn:aws:cloudformation:us-west-1:111122223333:stack/teststack/abc-123",
    "status-details": {"status": "UPDATE_COMPLETE", "status-reason": ""},
    "client-request-token": ""
  }
}`

const driftEvent = `{
  "version": "0",
  "id": "7a7e8feb-b491-4cf7-a9f1-bf3703467718",
  "detail-type": "CloudFormation Drift Detection Status Change",
  "source": "aws.cloudformation",
  "account": "111122223333",
  "time": "2017-12-22T18:44:48Z",
  "region": "us-west-1",
  "resources": ["arn:aws:cloudformation:us-west-1:111122223333:stack/teststack"],
  "detail": {
    "stack-id": "arn:aws:cloudformation:us-west-1:111122223333:stack/teststack/abc-123",
    "stack-drift-detection-id": "624af370-311a-11e8-b6b7-500cexample",
    "status-details": {"stack-drift-status": "DRIFTED", "detection-status": "DETECTION_COMPLETE"},
    "drift-detection-details": {"drifted-stack-resource-count": 2}
  }
}`

func TestParseEventStackStatus(t *testing.T) {
	ev, ok, err := ParseEvent([]byte(stackStatusEvent))
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("ParseEvent() ok = false, want true")
	}
	if ev.StackName != "teststack" {
		t.Fatalf("StackName = %q, want teststack", ev.StackName)
	}
	if ev.Status != "UPDATE_COMPLETE" {
		t.Fatalf("Status = %q, want UPDATE_COMPLETE", ev.Status)
	}
	if ev.Account != "111122223333" || ev.Region != "us-west-1" {
		t.Fatalf("account/region = %q/%q", ev.Account, ev.Region)
	}
}

func TestParseEventDrift(t *testing.T) {
	ev, ok, err := ParseEvent([]byte(driftEvent))
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}
	if !ok {
		t.Fatalf("ParseEvent() ok = false, want true")
	}
	if ev.DriftStatus != "DRIFTED" || ev.DriftedResourceCount != 2 {
		t.Fatalf("drift = %q/%d, want DRIFTED/2", ev.DriftStatus, ev.DriftedResourceCount)
	}
}

func TestParseEventIgnoresOtherSources(t *testing.T) {
	_, ok, err := ParseEvent([]byte(`{"source":"aws.s3","detail-type":"Object Created"}`))
	if err != nil {
		t.Fatalf("ParseEvent() error = %v", err)
	}
	if ok {
		t.Fatalf("ParseEvent() ok = true, want false")
	}
}

func TestStackNameFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:cloudformation:eu-west-1:123:stack/name/guid": "name",
		"arn:aws-cn:cloudformation:cn-north-1:123:stack/name":  "name",
		"stack/direct-name": "direct-name",
		"":                  "",
		"not-an-arn":        "",
	}
	for input, want := range cases {
		if got := StackNameFromARN(input); got != want {
			t.Fatalf("StackNameFromARN(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestCacheAppliesEvents(t *testing.T) {
	cache := NewCache(10)
	ev, _, err := ParseEvent([]byte(stackStatusEvent))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	cache.ApplyEvent(ev)

	state, ok := cache.Get("111122223333", "us-west-1", "teststack")
	if !ok {
		t.Fatalf("cache.Get() not found")
	}
	if state.StackStatus != "UPDATE_COMPLETE" || state.Source != sourceEventBridge {
		t.Fatalf("state = %+v", state)
	}

	drift, _, err := ParseEvent([]byte(driftEvent))
	if err != nil {
		t.Fatalf("ParseEvent(drift): %v", err)
	}
	cache.ApplyEvent(drift)

	state, _ = cache.Get("111122223333", "us-west-1", "teststack")
	if state.DriftStatus != "DRIFTED" || state.DriftedResourceCount != 2 {
		t.Fatalf("state after drift = %+v", state)
	}
	if state.StackStatus != "UPDATE_COMPLETE" {
		t.Fatalf("stack status should be preserved, got %q", state.StackStatus)
	}
	if got := len(cache.Events("111122223333", "us-west-1", "teststack", time.Time{}, 0)); got != 2 {
		t.Fatalf("events = %d, want 2", got)
	}
	if got := len(cache.List("111122223333", "us-west-1")); got != 1 {
		t.Fatalf("list = %d, want 1", got)
	}
	if got := len(cache.List("111122223333", "eu-west-1")); got != 0 {
		t.Fatalf("list other region = %d, want 0", got)
	}
}

func TestCacheEventBound(t *testing.T) {
	cache := NewCache(2)
	for i := 0; i < 5; i++ {
		cache.ApplyEvent(Event{
			ID:        string(rune('a' + i)),
			Account:   "1",
			Region:    "r",
			StackName: "s",
			Time:      time.Now().UTC().Add(time.Duration(i) * time.Second),
		})
	}
	if got := len(cache.Events("1", "r", "s", time.Time{}, 0)); got != 2 {
		t.Fatalf("events = %d, want 2", got)
	}
}

type fakeSQS struct {
	messages []sqstypes.Message
	deleted  []string
}

func (f *fakeSQS) ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	out := &sqs.ReceiveMessageOutput{Messages: f.messages}
	f.messages = nil
	return out, nil
}

func (f *fakeSQS) DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(params.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

func TestConsumerAppliesAndDeletesMessages(t *testing.T) {
	cache := NewCache(10)
	client := &fakeSQS{messages: []sqstypes.Message{
		{Body: aws.String(stackStatusEvent), ReceiptHandle: aws.String("r1"), MessageId: aws.String("m1")},
		{Body: aws.String("not json"), ReceiptHandle: aws.String("r2"), MessageId: aws.String("m2")},
	}}
	consumer := &Consumer{Client: client, QueueURL: "http://queue", Cache: cache}
	if err := consumer.pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce: %v", err)
	}
	if len(client.deleted) != 2 {
		t.Fatalf("deleted = %v, want both messages dropped", client.deleted)
	}
	state, ok := cache.Get("111122223333", "us-west-1", "teststack")
	if !ok || state.StackStatus != "UPDATE_COMPLETE" {
		t.Fatalf("cache state = %+v ok=%v", state, ok)
	}
}
