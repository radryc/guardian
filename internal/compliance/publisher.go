package compliance

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type Config struct {
	S3Bucket       string
	S3Region       string
	S3Prefix       string
	S3Endpoint     string
	ForcePathStyle bool
}

type Publisher interface {
	Publish(logicalPath string, content []byte, contentType string)
	Close(ctx context.Context) error
}

func NewNoop() Publisher { return noopPublisher{} }

type noopPublisher struct{}

func (noopPublisher) Publish(string, []byte, string) {}

func (noopPublisher) Close(context.Context) error { return nil }

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
	pub := &s3Publisher{
		client: client,
		bucket: cfg.S3Bucket,
		prefix: strings.Trim(cfg.S3Prefix, "/"),
		queue:  make(chan uploadRequest, 128),
	}
	pub.wg.Add(1)
	go pub.run()
	return pub, nil
}

type uploadRequest struct {
	key         string
	content     []byte
	contentType string
}

type s3Publisher struct {
	client *s3.Client
	bucket string
	prefix string
	queue  chan uploadRequest
	once   sync.Once
	wg     sync.WaitGroup
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
	p.queue <- req
}

func (p *s3Publisher) Close(ctx context.Context) error {
	done := make(chan struct{})
	p.once.Do(func() {
		close(p.queue)
		go func() {
			p.wg.Wait()
			close(done)
		}()
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (p *s3Publisher) run() {
	defer p.wg.Done()
	for req := range p.queue {
		_, err := p.client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket:      &p.bucket,
			Key:         &req.key,
			Body:        bytes.NewReader(req.content),
			ContentType: stringPtr(req.contentType),
		})
		if err != nil {
			log.Printf("publish compliance archive %s: %v", req.key, err)
		}
	}
}

func (p *s3Publisher) objectKey(logicalPath string) string {
	key := strings.TrimLeft(logicalPath, "/")
	if p.prefix == "" {
		return key
	}
	return p.prefix + "/" + key
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
