package awsobserve

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

const (
	defaultPollWaitSeconds = 20
	defaultMaxMessages     = 10
)

// NewSQSClient builds an SQS client using the default AWS credential chain for
// the pusher's own account/region (the observe queue lives beside the pusher).
func NewSQSClient(ctx context.Context, region string) (*sqs.Client, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if strings.TrimSpace(region) != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("load aws config for sqs: %w", err)
	}
	cfg.Credentials = aws.NewCredentialsCache(cfg.Credentials)
	return sqs.NewFromConfig(cfg), nil
}

// SQSAPI is the subset of the SQS client used by the consumer.
type SQSAPI interface {
	ReceiveMessage(ctx context.Context, params *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, params *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// Consumer long-polls an SQS queue fed by EventBridge, normalizes
// CloudFormation events and applies them to the observed cache.
type Consumer struct {
	Client            SQSAPI
	QueueURL          string
	Cache             *Cache
	PollWaitSeconds   int32
	MaxMessages       int32
	VisibilityTimeout int32
	// OnEvent, when set, is invoked for every normalized event.
	OnEvent func(Event)
}

// Run polls until the context is cancelled.
func (c *Consumer) Run(ctx context.Context) error {
	if err := c.validate(); err != nil {
		return err
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := c.pollOnce(ctx); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return ctx.Err()
			}
			log.Printf("[awsobserve] receive failed (retrying in 5s): %v", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (c *Consumer) validate() error {
	if c.Client == nil {
		return errors.New("awsobserve: sqs client is required")
	}
	if strings.TrimSpace(c.QueueURL) == "" {
		return errors.New("awsobserve: queue URL is required")
	}
	if c.Cache == nil {
		c.Cache = NewCache(0)
	}
	if c.PollWaitSeconds <= 0 {
		c.PollWaitSeconds = defaultPollWaitSeconds
	}
	if c.MaxMessages <= 0 {
		c.MaxMessages = defaultMaxMessages
	}
	return nil
}

func (c *Consumer) pollOnce(ctx context.Context) error {
	out, err := c.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:              aws.String(c.QueueURL),
		MaxNumberOfMessages:   c.MaxMessages,
		WaitTimeSeconds:       c.PollWaitSeconds,
		VisibilityTimeout:     c.VisibilityTimeout,
		MessageAttributeNames: []string{"All"},
	})
	if err != nil {
		return err
	}
	for _, message := range out.Messages {
		c.handleMessage(message)
	}
	return nil
}

func (c *Consumer) handleMessage(message sqstypes.Message) {
	body := strings.TrimSpace(aws.ToString(message.Body))
	if body == "" {
		c.delete(message)
		return
	}
	ev, ok, err := ParseEvent([]byte(body))
	if err != nil {
		// Permanent parse failure: drop so the queue does not poison-pill.
		log.Printf("[awsobserve] dropping unparseable event %s: %v", aws.ToString(message.MessageId), err)
		c.delete(message)
		return
	}
	if !ok {
		c.delete(message)
		return
	}
	c.Cache.ApplyEvent(ev)
	if c.OnEvent != nil {
		c.OnEvent(ev)
	}
	c.delete(message)
}

func (c *Consumer) delete(message sqstypes.Message) {
	// Best-effort: failures here only delay redelivery, not correctness.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.QueueURL),
		ReceiptHandle: message.ReceiptHandle,
	}); err != nil {
		log.Printf("[awsobserve] delete message %s failed: %v", aws.ToString(message.MessageId), err)
	}
}
