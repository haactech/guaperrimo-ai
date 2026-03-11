package storage

import (
	"context"
	"fmt"
	"io"

	"stylerag/internal/config"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// R2Store implements ImageStore using Cloudflare R2 (S3-compatible API).
type R2Store struct {
	client    *s3.Client
	bucket    string
	publicURL string
}

// NewR2Store creates an R2Store configured for Cloudflare R2.
func NewR2Store(ctx context.Context, cfg *config.Config) (*R2Store, error) {
	endpoint := fmt.Sprintf("https://%s.r2.cloudflarestorage.com", cfg.R2AccountID)

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.R2AccessKeyID, cfg.R2AccessKeySecret, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("loading aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &endpoint
	})

	return &R2Store{
		client:    client,
		bucket:    cfg.R2BucketName,
		publicURL: cfg.R2PublicURL,
	}, nil
}

var storageTracer = otel.Tracer("stylerag/storage")

// Upload stores a file in R2 and returns its public URL.
func (s *R2Store) Upload(ctx context.Context, input UploadInput) (*UploadOutput, error) {
	ctx, span := storageTracer.Start(ctx, "storage.upload", trace.WithAttributes(
		attribute.String("storage.key", input.Key),
		attribute.String("storage.content_type", input.ContentType),
	))
	defer span.End()

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &input.Key,
		Body:        input.Body,
		ContentType: &input.ContentType,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, fmt.Errorf("uploading to r2: %w", err)
	}

	url := fmt.Sprintf("%s/%s", s.publicURL, input.Key)
	return &UploadOutput{URL: url}, nil
}

// Download retrieves the bytes of an object from R2.
func (s *R2Store) Download(ctx context.Context, key string) ([]byte, error) {
	ctx, span := storageTracer.Start(ctx, "storage.download", trace.WithAttributes(
		attribute.String("storage.key", key),
	))
	defer span.End()

	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	})
	if err != nil {
		return nil, fmt.Errorf("downloading from r2: %w", err)
	}
	defer out.Body.Close()

	data, err := io.ReadAll(out.Body)
	if err != nil {
		return nil, fmt.Errorf("reading r2 object body: %w", err)
	}
	return data, nil
}

// ListKeys returns object keys matching the given prefix.
func (s *R2Store) ListKeys(ctx context.Context, prefix string) ([]string, error) {
	ctx, span := storageTracer.Start(ctx, "storage.list_keys", trace.WithAttributes(
		attribute.String("storage.key", prefix),
	))
	defer span.End()

	out, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: &s.bucket,
		Prefix: &prefix,
	})
	if err != nil {
		return nil, fmt.Errorf("listing r2 keys: %w", err)
	}

	keys := make([]string, 0, len(out.Contents))
	for _, obj := range out.Contents {
		if obj.Key != nil {
			keys = append(keys, *obj.Key)
		}
	}
	return keys, nil
}
