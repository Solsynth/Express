package developadapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/minio/minio-go/v7"

	"src.solsynth.dev/solsynth/distribution/internal/config"
	"src.solsynth.dev/solsynth/distribution/internal/develop"
)

// PackageStore keeps mini app archives in the process's S3 bucket. It is a
// separate prefix from release artifacts so retention rules never delete a
// published mini app.
type PackageStore struct {
	client        *minio.Client
	bucket        string
	prefix        string
	publicBaseURL string
	maxBytes      int64
}

// NewPackageStore builds the archive store from the process S3 settings and
// the [develop.packages] block.
func NewPackageStore(cfg *config.Config, client *minio.Client, bucket string) develop.PackageStore {
	if cfg == nil || client == nil || strings.TrimSpace(bucket) == "" {
		return nil
	}
	prefix := strings.Trim(cfg.Develop.Packages.KeyPrefix, "/")
	if prefix == "" {
		prefix = "plugins"
	}
	return &PackageStore{
		client:        client,
		bucket:        bucket,
		prefix:        prefix,
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg.Develop.Packages.PublicBaseURL), "/"),
		maxBytes:      cfg.Develop.Packages.MaxBytes,
	}
}

// Save writes an archive under "{prefix}/{miniAppID}/{random}.zip".
func (s *PackageStore) Save(ctx context.Context, miniAppID, fileName string, content []byte) (develop.PackageUpload, error) {
	if s == nil {
		return develop.PackageUpload{}, fmt.Errorf("package store is not configured")
	}
	if s.maxBytes > 0 && int64(len(content)) > s.maxBytes {
		return develop.PackageUpload{}, fmt.Errorf("package exceeds %d bytes", s.maxBytes)
	}
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])
	key := path.Join(s.prefix, miniAppID, digest+".zip")
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{
		ContentType: "application/zip",
	})
	if err != nil {
		return develop.PackageUpload{}, fmt.Errorf("put package: %w", err)
	}
	return develop.PackageUpload{
		Key:      key,
		URL:      s.publicURL(key),
		Size:     int64(len(content)),
		Sha256:   digest,
		FileName: path.Base(fileName),
	}, nil
}

// Open reads a stored archive back into memory. Mini app packages are capped
// at a few megabytes, so a buffered read keeps the HTTP handler simple.
func (s *PackageStore) Open(ctx context.Context, key string) ([]byte, error) {
	if s == nil {
		return nil, fmt.Errorf("package store is not configured")
	}
	object, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get package: %w", err)
	}
	defer object.Close()
	info, err := object.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat package: %w", err)
	}
	if s.maxBytes > 0 && info.Size > s.maxBytes {
		return nil, fmt.Errorf("package exceeds %d bytes", s.maxBytes)
	}
	buffer := make([]byte, 0, info.Size)
	chunk := make([]byte, 64*1024)
	for {
		read, readErr := object.Read(chunk)
		if read > 0 {
			buffer = append(buffer, chunk[:read]...)
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return nil, fmt.Errorf("read package: %w", readErr)
		}
	}
	return buffer, nil
}

// Delete removes a stored archive.
func (s *PackageStore) Delete(ctx context.Context, key string) error {
	if s == nil {
		return fmt.Errorf("package store is not configured")
	}
	return s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
}

// publicURL composes the archive's public URL, empty when the bucket is not
// publicly readable.
func (s *PackageStore) publicURL(key string) string {
	if s.publicBaseURL == "" {
		return ""
	}
	base, err := url.Parse(s.publicBaseURL + "/")
	if err != nil {
		return ""
	}
	relative, err := url.Parse(key)
	if err != nil {
		return ""
	}
	return base.ResolveReference(relative).String()
}
