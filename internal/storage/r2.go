package storage

import (
	"context"
	"fmt"

	"stylerag/internal/config"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

// Upload stores a file in R2 and returns its public URL.
func (s *R2Store) Upload(ctx context.Context, input UploadInput) (*UploadOutput, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &input.Key,
		Body:        input.Body,
		ContentType: &input.ContentType,
	})
	if err != nil {
		return nil, fmt.Errorf("uploading to r2: %w", err)
	}

	url := fmt.Sprintf("%s/%s", s.publicURL, input.Key)
	return &UploadOutput{URL: url}, nil
}
