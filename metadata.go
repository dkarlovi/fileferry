package main

import (
	"time"
)

type FileMetadata struct {
	TakenTime   *time.Time
	Extension   string
	CameraMaker string
	CameraModel string
}

// extractImageMetadata should be implemented elsewhere, but for now provide a stub.
func extractImageMetadata(path string) (*FileMetadata, error) {
	// TODO: Implement actual EXIF extraction logic
	return &FileMetadata{}, nil
}
