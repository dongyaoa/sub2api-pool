package service

import (
	"context"
	"net/http"
	"strings"
)

// ImageStorage is the object storage abstraction shared by media gateway requests.
type ImageStorage interface {
	Save(ctx context.Context, key, contentType string, data []byte) (url string, err error)
}

// ImageStorageResolver returns the configured storage for video gateway results.
type ImageStorageResolver func() (*ImageResultUploader, bool)

// ImageResultUploader stores generated video content using the existing S3 settings.
type ImageResultUploader struct {
	storage ImageStorage
	prefix  string
}

func NewImageResultUploader(storage ImageStorage, prefix string) *ImageResultUploader {
	return &ImageResultUploader{storage: storage, prefix: prefix}
}

func detectedImageContentType(data []byte) string {
	ct := strings.TrimSpace(strings.Split(http.DetectContentType(data), ";")[0])
	if strings.HasPrefix(ct, "image/") {
		return ct
	}
	return ""
}
