package compliance

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type Config struct {
	S3Bucket       string
	S3Region       string
	S3Prefix       string
	S3Endpoint     string
	ForcePathStyle bool
	// ServerSideEncryption is "AES256" or "aws:kms". Empty leaves the bucket
	// default in place.
	ServerSideEncryption string
	// SSEKMSKeyID is required when ServerSideEncryption is "aws:kms".
	SSEKMSKeyID string
	// ObjectLockMode is "GOVERNANCE" or "COMPLIANCE"; ObjectLockRetainUntil
	// must also be set.
	ObjectLockMode        string
	ObjectLockRetainUntil time.Time
	// MaxRetries is the number of retries after the first attempt.
	MaxRetries int
	// RetryBaseDelay is the first backoff delay; it doubles per attempt.
	RetryBaseDelay time.Duration
	// OnError receives terminal upload failures. When nil, failures are logged.
	OnError func(error)
}

type Publisher interface {
	Publish(logicalPath string, content []byte, contentType string)
	Close(ctx context.Context) error
}

func NewNoop() Publisher { return noopPublisher{} }

type noopPublisher struct{}

func (noopPublisher) Publish(string, []byte, string) {}

func (noopPublisher) Close(context.Context) error { return nil }

// putObjectAPI is the subset of the S3 client used by the publisher. It is an
// interface so retry/encryption behaviour can be tested without AWS.
type putObjectAPI interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
}

func NewS3Publisher(ctx context.Context, cfg Config) (Publisher, error) {
	loadOpts := make([]func(*awsconfig.LoadOptions) error, 0, 1)
	if cfg.S3Region != "" {
		loadOpts = append(loadOpts, awsconfig.WithRegion(cfg.S3Region))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}
	client := s3.NewFromConfig(awsCfg, func(opts *s3.Options) {
		opts.UsePathStyle = cfg.ForcePathStyle
		if cfg.S3Endpoint != "" {
			opts.EndpointResolver = s3.EndpointResolverFromURL(cfg.S3Endpoint)
		}
	})
	return newS3Publisher(client, cfg), nil
}

func newS3Publisher(client putObjectAPI, cfg Config) *s3Publisher {
	if cfg.MaxRetries < 0 {
		cfg.MaxRetries = 0
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 200 * time.Millisecond
	}
	pub := &s3Publisher{
		client:         client,
		bucket:         cfg.S3Bucket,
		prefix:         strings.Trim(cfg.S3Prefix, "/"),
		queue:          make(chan uploadRequest, 128),
		stop:           make(chan struct{}),
		finished:       make(chan struct{}),
		maxRetries:     cfg.MaxRetries,
		retryBaseDelay: cfg.RetryBaseDelay,
		sse:            strings.TrimSpace(cfg.ServerSideEncryption),
		sseKMSKeyID:    strings.TrimSpace(cfg.SSEKMSKeyID),
		objectLockMode: strings.TrimSpace(cfg.ObjectLockMode),
		objectLockTill: cfg.ObjectLockRetainUntil,
		onError:        cfg.OnError,
	}
	pub.wg.Add(1)
	go pub.run()
	return pub
}

type uploadRequest struct {
	key         string
	content     []byte
	contentType string
}

type s3Publisher struct {
	client         putObjectAPI
	bucket         string
	prefix         string
	queue          chan uploadRequest
	stop           chan struct{}
	finished       chan struct{}
	closeOnce      sync.Once
	mu             sync.RWMutex
	closed         bool
	wg             sync.WaitGroup
	maxRetries     int
	retryBaseDelay time.Duration
	sse            string
	sseKMSKeyID    string
	objectLockMode string
	objectLockTill time.Time
	onError        func(error)

	errMu    sync.Mutex
	firstErr error
}

func (p *s3Publisher) Publish(logicalPath string, content []byte, contentType string) {
	if logicalPath == "" {
		return
	}
	req := uploadRequest{
		key:         p.objectKey(logicalPath),
		content:     append([]byte(nil), content...),
		contentType: contentType,
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		return
	}
	select {
	case p.queue <- req:
	case <-p.stop:
	}
}

func (p *s3Publisher) Close(ctx context.Context) error {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		close(p.stop)
		close(p.queue)
		p.mu.Unlock()
		go func() {
			p.wg.Wait()
			close(p.finished)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.finished:
		p.errMu.Lock()
		defer p.errMu.Unlock()
		return p.firstErr
	}
}

func (p *s3Publisher) run() {
	defer p.wg.Done()
	for req := range p.queue {
		if err := p.upload(req); err != nil {
			p.recordError(err)
		}
	}
}

func (p *s3Publisher) upload(req uploadRequest) error {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		attemptCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, err := p.client.PutObject(attemptCtx, p.input(req), func(*s3.Options) {})
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == p.maxRetries {
			break
		}
		delay := p.retryBaseDelay * time.Duration(1<<attempt)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		}
	}
	return lastErr
}

func (p *s3Publisher) input(req uploadRequest) *s3.PutObjectInput {
	in := &s3.PutObjectInput{
		Bucket:      &p.bucket,
		Key:         &req.key,
		Body:        bytes.NewReader(req.content),
		ContentType: stringPtr(req.contentType),
	}
	if p.sse != "" {
		in.ServerSideEncryption = s3types.ServerSideEncryption(p.sse)
	}
	if p.sseKMSKeyID != "" {
		in.SSEKMSKeyId = &p.sseKMSKeyID
	}
	if p.objectLockMode != "" && !p.objectLockTill.IsZero() {
		in.ObjectLockMode = s3types.ObjectLockMode(p.objectLockMode)
		in.ObjectLockRetainUntilDate = &p.objectLockTill
	}
	return in
}

func (p *s3Publisher) recordError(err error) {
	if err == nil {
		return
	}
	p.errMu.Lock()
	if p.firstErr == nil {
		p.firstErr = err
	}
	p.errMu.Unlock()
	if p.onError != nil {
		p.onError(err)
		return
	}
	log.Printf("publish compliance archive: %v", err)
}

func (p *s3Publisher) objectKey(logicalPath string) string {
	key := strings.TrimLeft(logicalPath, "/")
	if p.prefix == "" {
		return key
	}
	return p.prefix + "/" + key
}

func IsTerminalPublishError(err error) bool {
	return err != nil && !errors.Is(err, context.Canceled)
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
