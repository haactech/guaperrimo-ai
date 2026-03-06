package storage

import (
	"context"
	"io"
)

// UploadInput contains the data needed to upload an image.
type UploadInput struct {
	Key         string
	Body        io.Reader
	ContentType string
}

// UploadOutput contains the result of a successful upload.
type UploadOutput struct {
	URL string
}

// ImageStore abstracts image storage operations.
type ImageStore interface {
	Upload(ctx context.Context, input UploadInput) (*UploadOutput, error)
	Download(ctx context.Context, key string) ([]byte, error)
	ListKeys(ctx context.Context, prefix string) ([]string, error)
}
