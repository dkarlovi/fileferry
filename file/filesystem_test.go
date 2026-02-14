package file

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCollapseSeparators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		sep      string
		expected string
	}{
		{
			name:     "collapse double dashes",
			input:    "hello--world",
			sep:      "-",
			expected: "hello-world",
		},
		{
			name:     "collapse multiple dashes",
			input:    "hello----world",
			sep:      "-",
			expected: "hello-world",
		},
		{
			name:     "collapse underscores",
			input:    "hello__world",
			sep:      "_",
			expected: "hello_world",
		},
		{
			name:     "no collapse needed",
			input:    "hello-world",
			sep:      "-",
			expected: "hello-world",
		},
		{
			name:     "empty string",
			input:    "",
			sep:      "-",
			expected: "",
		},
		{
			name:     "only separators",
			input:    "----",
			sep:      "-",
			expected: "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := collapseSeparators(tt.input, tt.sep)
			if result != tt.expected {
				t.Errorf("collapseSeparators(%q, %q) = %q; want %q", tt.input, tt.sep, result, tt.expected)
			}
		})
	}
}

func TestNormalizeSeparators(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "collapse dashes in filename",
			input:    "test--file.jpg",
			expected: "test-file.jpg",
		},
		{
			name:     "collapse underscores in filename",
			input:    "test__file.jpg",
			expected: "test_file.jpg",
		},
		{
			name:     "collapse both dashes and underscores",
			input:    "test--file__name.jpg",
			expected: "test-file_name.jpg",
		},
		{
			name:     "trim leading and trailing separators",
			input:    "-_test_file_-.jpg",
			expected: "test_file.jpg",
		},
		{
			name:     "with directory path",
			input:    "/path/to/test--file.jpg",
			expected: filepath.Join("/path/to", "test-file.jpg"),
		},
		{
			name:     "relative path",
			input:    "dir/test__file.jpg",
			expected: filepath.Join("dir", "test_file.jpg"),
		},
		{
			name:     "no normalization needed",
			input:    "testfile.jpg",
			expected: "testfile.jpg",
		},
		{
			name:     "spaces should be trimmed",
			input:    " test file .jpg",
			expected: "test file.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeSeparators(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeSeparators(%q) = %q; want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFileTypeRegistry_IsFileType(t *testing.T) {
	registry := &FileTypeRegistry{
		Categories: map[string][]string{
			"image": {".jpg", ".jpeg", ".png"},
			"video": {".mp4", ".mov"},
		},
	}

	tests := []struct {
		name     string
		path     string
		types    []string
		expected bool
	}{
		{
			name:     "jpg is image",
			path:     "photo.jpg",
			types:    []string{"image"},
			expected: true,
		},
		{
			name:     "jpeg is image",
			path:     "photo.jpeg",
			types:    []string{"image"},
			expected: true,
		},
		{
			name:     "uppercase JPG is image",
			path:     "photo.JPG",
			types:    []string{"image"},
			expected: true,
		},
		{
			name:     "mp4 is video",
			path:     "video.mp4",
			types:    []string{"video"},
			expected: true,
		},
		{
			name:     "mp4 not image",
			path:     "video.mp4",
			types:    []string{"image"},
			expected: false,
		},
		{
			name:     "txt not in registry",
			path:     "file.txt",
			types:    []string{"image", "video"},
			expected: false,
		},
		{
			name:     "match one of multiple types",
			path:     "video.mp4",
			types:    []string{"image", "video"},
			expected: true,
		},
		{
			name:     "no extension",
			path:     "noext",
			types:    []string{"image"},
			expected: false,
		},
		{
			name:     "empty types",
			path:     "photo.jpg",
			types:    []string{},
			expected: false,
		},
		{
			name:     "non-existent category",
			path:     "photo.jpg",
			types:    []string{"document"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := registry.IsFileType(tt.path, tt.types)
			if result != tt.expected {
				t.Errorf("IsFileType(%q, %v) = %v; want %v", tt.path, tt.types, result, tt.expected)
			}
		})
	}
}

func TestIsFileType(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		types    []string
		expected bool
	}{
		{
			name:     "standard image format",
			path:     "photo.jpg",
			types:    []string{"image"},
			expected: true,
		},
		{
			name:     "video format",
			path:     "video.mp4",
			types:    []string{"video"},
			expected: true,
		},
		{
			name:     "raw image format",
			path:     "photo.dng",
			types:    []string{"image.raw"},
			expected: true,
		},
		{
			name:     "multiple types - match video",
			path:     "video.mkv",
			types:    []string{"image", "video"},
			expected: true,
		},
		{
			name:     "not a supported type",
			path:     "document.pdf",
			types:    []string{"image", "video"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isFileType(tt.path, tt.types)
			if result != tt.expected {
				t.Errorf("isFileType(%q, %v) = %v; want %v", tt.path, tt.types, result, tt.expected)
			}
		})
	}
}

func TestResolveTargetPath(t *testing.T) {
	testTime := time.Date(2024, 1, 15, 14, 30, 45, 0, time.UTC)

	tests := []struct {
		name     string
		tmpl     string
		meta     *FileMetadata
		expected string
		wantErr  bool
	}{
		{
			name:    "nil metadata",
			tmpl:    "/path/{meta.taken.year}",
			meta:    nil,
			wantErr: true,
		},
		{
			name: "year template",
			tmpl: "/organized/{meta.taken.year}/file.jpg",
			meta: &FileMetadata{
				TakenTime: &testTime,
				Extension: "jpg",
			},
			expected: filepath.Join("/organized", testTime.Local().Format("2006"), "file.jpg"),
			wantErr:  false,
		},
		{
			name: "date template",
			tmpl: "/organized/{meta.taken.date}/file.jpg",
			meta: &FileMetadata{
				TakenTime: &testTime,
				Extension: "jpg",
			},
			expected: filepath.Join("/organized", testTime.Local().Format("2006-01-02"), "file.jpg"),
			wantErr:  false,
		},
		{
			name: "datetime template",
			tmpl: "/organized/{meta.taken.datetime}.jpg",
			meta: &FileMetadata{
				TakenTime: &testTime,
				Extension: "jpg",
			},
			expected: filepath.Join("/organized", testTime.Local().Format("2006-01-02-15-04-05")+".jpg"),
			wantErr:  false,
		},
		{
			name: "extension template",
			tmpl: "/organized/file.{file.extension}",
			meta: &FileMetadata{
				Extension: "mp4",
			},
			expected: filepath.Join("/organized", "file.mp4"),
			wantErr:  false,
		},
		{
			name: "camera maker template",
			tmpl: "/organized/{meta.camera.maker}/file.jpg",
			meta: &FileMetadata{
				CameraMaker: "Canon",
				Extension:   "jpg",
			},
			expected: filepath.Join("/organized", "Canon", "file.jpg"),
			wantErr:  false,
		},
		{
			name: "camera model template",
			tmpl: "/organized/{meta.camera.model}/file.jpg",
			meta: &FileMetadata{
				CameraModel: "EOS 5D",
				Extension:   "jpg",
			},
			expected: filepath.Join("/organized", "EOS 5D", "file.jpg"),
			wantErr:  false,
		},
		{
			name: "combined template",
			tmpl: "/organized/{meta.taken.year}/{meta.camera.maker}/{meta.taken.date}.{file.extension}",
			meta: &FileMetadata{
				TakenTime:   &testTime,
				Extension:   "jpg",
				CameraMaker: "Sony",
			},
			expected: filepath.Join("/organized", testTime.Local().Format("2006"), "Sony", testTime.Local().Format("2006-01-02")+".jpg"),
			wantErr:  false,
		},
		{
			name: "no time data in metadata",
			tmpl: "/organized/{meta.taken.year}/file.jpg",
			meta: &FileMetadata{
				Extension: "jpg",
			},
			expected: filepath.Join("/organized", "{meta.taken.year}", "file.jpg"),
			wantErr:  false,
		},
		{
			name: "normalization applies",
			tmpl: "/organized/test--file__{file.extension}",
			meta: &FileMetadata{
				Extension: "jpg",
			},
			expected: filepath.Join("/organized", "test-file_jpg"),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := resolveTargetPath(tt.tmpl, tt.meta)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveTargetPath() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && result != tt.expected {
				t.Errorf("resolveTargetPath() = %q; want %q", result, tt.expected)
			}
		})
	}
}

func TestHasUnpopulatedTokens(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "no tokens",
			path:     "/organized/2024/file.jpg",
			expected: false,
		},
		{
			name:     "single unpopulated token",
			path:     "/organized/{meta.taken.year}/file.jpg",
			expected: true,
		},
		{
			name:     "multiple unpopulated tokens",
			path:     "/organized/{meta.taken.year}/{meta.taken.date}/file.jpg",
			expected: true,
		},
		{
			name:     "token in filename",
			path:     "/organized/{meta.taken.datetime}.jpg",
			expected: true,
		},
		{
			name:     "only opening brace",
			path:     "/organized/{test/file.jpg",
			expected: false,
		},
		{
			name:     "only closing brace",
			path:     "/organized/test}/file.jpg",
			expected: false,
		},
		{
			name:     "unpaired braces",
			path:     "/path/{token1}/middle}/more/{token2}",
			expected: true, // has two valid tokens: {token1} and {token2}
		},
		{
			name:     "braces in filename but not a token",
			path:     "/organized/file{123}.jpg",
			expected: true, // {123} is a valid token pattern even if not our template token
		},
		{
			name:     "empty path",
			path:     "",
			expected: false,
		},
		{
			name:     "nested braces (outer)",
			path:     "/organized/{{nested}}/file.jpg",
			expected: true, // {nested} is detected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasUnpopulatedTokens(tt.path)
			if result != tt.expected {
				t.Errorf("hasUnpopulatedTokens(%q) = %v; want %v", tt.path, result, tt.expected)
			}
		})
	}
}

// Note: scanFiles() is not directly tested here because it requires filesystem access.
// It's an internal function that's tested indirectly through the FileIterator functions
// which are used by the actual application. The logic of scanFiles is straightforward:
// it walks a directory tree using filepath.Walk and filters files by type using isFileType.
// Both the filtering logic (isFileType) and path handling are tested above.
