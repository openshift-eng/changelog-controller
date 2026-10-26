package firestore

import (
	"context"
	"fmt"
	"io"

	gcs "cloud.google.com/go/storage"
)

// gcsObjectReader reads GCS object contents. It exists so listing and cache
// warming logic can be tested without a live bucket.
type gcsObjectReader interface {
	ReadObject(ctx context.Context, path string) ([]byte, error)
}

// gcsClientReader adapts a GCS client and bucket name to gcsObjectReader.
type gcsClientReader struct {
	client     *gcs.Client
	bucketName string
}

func (r *gcsClientReader) ReadObject(ctx context.Context, path string) ([]byte, error) {
	reader, err := r.client.Bucket(r.bucketName).Object(path).NewReader(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer func() { _ = reader.Close() }()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return data, nil
}
