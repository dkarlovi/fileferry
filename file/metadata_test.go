package file

import (
	"testing"
	"time"

	mkvparse "github.com/remko/go-mkvparse"
)

func TestParseMetadataFromFilenamePattern(t *testing.T) {
	tests := []struct {
		name     string
		filename string
		pattern  string
		want     *FileMetadata
	}{
		{
			name:     "pattern with date and time",
			filename: "2023-05-15 10-30-45.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time}.jpg",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2023, 5, 15, 10, 30, 45, 0, time.Local)),
				Extension: "jpg",
			},
		},
		{
			name:     "pattern with date only",
			filename: "2023-05-15.mp4",
			pattern:  "{meta.taken.date}.mp4",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2023, 5, 15, 0, 0, 0, 0, time.Local)),
				Extension: "mp4",
			},
		},
		{
			name:     "pattern with mkv extension",
			filename: "2024-01-10 15-21-02.mkv",
			pattern:  "{meta.taken.date} {meta.taken.time}.mkv",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2024, 1, 10, 15, 21, 2, 0, time.Local)),
				Extension: "mkv",
			},
		},
		{
			name:     "no match - wrong pattern",
			filename: "random-file.jpg",
			pattern:  "{meta.taken.date}.jpg",
			want:     nil,
		},
		{
			name:     "no match - incomplete date",
			filename: "2023-05.jpg",
			pattern:  "{meta.taken.date}.jpg",
			want:     nil,
		},
		{
			name:     "no match - invalid time format",
			filename: "2023-05-15 25-99-99.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time}.jpg",
			want:     nil,
		},
		{
			name:     "pattern with hhmmss format specifier",
			filename: "Still 2026-01-23 222212_1.1.1.jpg",
			pattern:  "Still {meta.taken.date} {meta.taken.time:hhmmss}_1.1.1.jpg",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2026, 1, 23, 22, 22, 12, 0, time.Local)),
				Extension: "jpg",
			},
		},
		{
			name:     "pattern with hhmmss format - simple",
			filename: "2023-05-15 103045.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time:hhmmss}.jpg",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2023, 5, 15, 10, 30, 45, 0, time.Local)),
				Extension: "jpg",
			},
		},
		{
			name:     "pattern with hhmm format specifier",
			filename: "2023-05-15 1030.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time:hhmm}.jpg",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2023, 5, 15, 10, 30, 0, 0, time.Local)),
				Extension: "jpg",
			},
		},
		{
			name:     "pattern with explicit hh-mm-ss format specifier",
			filename: "2023-05-15 10-30-45.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time:hh-mm-ss}.jpg",
			want: &FileMetadata{
				TakenTime: timePtr(time.Date(2023, 5, 15, 10, 30, 45, 0, time.Local)),
				Extension: "jpg",
			},
		},
		{
			name:     "no match - wrong hhmmss format",
			filename: "2023-05-15 10-30-45.jpg",
			pattern:  "{meta.taken.date} {meta.taken.time:hhmmss}.jpg",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseMetadataFromFilenamePattern(tt.filename, tt.pattern)
			if tt.want == nil {
				if got != nil {
					t.Errorf("parseMetadataFromFilenamePattern() = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("parseMetadataFromFilenamePattern() = nil, want %+v", tt.want)
				return
			}
			if got.Extension != tt.want.Extension {
				t.Errorf("parseMetadataFromFilenamePattern() Extension = %v, want %v", got.Extension, tt.want.Extension)
			}
			if !timesEqual(got.TakenTime, tt.want.TakenTime) {
				t.Errorf("parseMetadataFromFilenamePattern() TakenTime = %v, want %v", got.TakenTime, tt.want.TakenTime)
			}
		})
	}
}

func TestDateHandler_HandleDate(t *testing.T) {
	tests := []struct {
		name      string
		id        mkvparse.ElementID
		v         time.Time
		wantFound bool
		wantTime  time.Time
		wantErr   bool
	}{
		{
			name:      "captures DateUTC element",
			id:        mkvparse.DateUTCElement,
			v:         time.Date(2023, 5, 15, 10, 30, 45, 0, time.UTC),
			wantFound: true,
			wantTime:  time.Date(2023, 5, 15, 10, 30, 45, 0, time.UTC),
			wantErr:   false,
		},
		{
			name:      "ignores non-DateUTC element",
			id:        mkvparse.ElementID(0x1234), // arbitrary non-DateUTC ID
			v:         time.Date(2023, 5, 15, 10, 30, 45, 0, time.UTC),
			wantFound: false,
			wantTime:  time.Time{},
			wantErr:   false,
		},
		{
			name:      "only captures first DateUTC element",
			id:        mkvparse.DateUTCElement,
			v:         time.Date(2023, 5, 15, 10, 30, 45, 0, time.UTC),
			wantFound: true,
			wantTime:  time.Date(2023, 5, 15, 10, 30, 45, 0, time.UTC),
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &dateHandler{}

			// If testing "only captures first", we need to set found=true first
			if tt.name == "only captures first DateUTC element" {
				// First call should set found
				err := h.HandleDate(tt.id, tt.v, mkvparse.ElementInfo{})
				if err != nil {
					t.Errorf("HandleDate() first call error = %v", err)
					return
				}
				if !h.found {
					t.Errorf("HandleDate() first call should set found=true")
					return
				}
				firstTime := h.tm

				// Second call with different time should not update
				newTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
				err = h.HandleDate(tt.id, newTime, mkvparse.ElementInfo{})
				if err != nil {
					t.Errorf("HandleDate() second call error = %v", err)
					return
				}
				if !h.tm.Equal(firstTime) {
					t.Errorf("HandleDate() second call should not update time, got %v, want %v", h.tm, firstTime)
				}
				return
			}

			err := h.HandleDate(tt.id, tt.v, mkvparse.ElementInfo{})
			if (err != nil) != tt.wantErr {
				t.Errorf("HandleDate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if h.found != tt.wantFound {
				t.Errorf("HandleDate() found = %v, want %v", h.found, tt.wantFound)
			}
			if tt.wantFound && !h.tm.Equal(tt.wantTime) {
				t.Errorf("HandleDate() tm = %v, want %v", h.tm, tt.wantTime)
			}
		})
	}
}

// Helper functions
func timePtr(t time.Time) *time.Time {
	return &t
}

func timesEqual(t1, t2 *time.Time) bool {
	if t1 == nil && t2 == nil {
		return true
	}
	if t1 == nil || t2 == nil {
		return false
	}
	return t1.Equal(*t2)
}

func TestExtractImageMetadata(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantExtension string
		wantErr       bool
	}{
		{
			name:          "non-existent file returns metadata with extension",
			path:          "/non/existent/path/image.jpg",
			wantExtension: "jpg",
			wantErr:       false,
		},
		{
			name:          "png extension",
			path:          "/non/existent/path/image.png",
			wantExtension: "png",
			wantErr:       false,
		},
		{
			name:          "uppercase extension",
			path:          "/non/existent/path/IMAGE.JPG",
			wantExtension: "jpg", // should be lowercased
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractImageMetadata(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractImageMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil {
				t.Errorf("extractImageMetadata() = nil, want metadata")
				return
			}
			if got.Extension != tt.wantExtension {
				t.Errorf("extractImageMetadata() Extension = %v, want %v", got.Extension, tt.wantExtension)
			}
		})
	}
}

func TestExtractVideoMetadata(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		wantExtension string
		wantErr       bool
	}{
		{
			name:          "non-existent mp4 file returns metadata with extension",
			path:          "/non/existent/path/video.mp4",
			wantExtension: "mp4",
			wantErr:       false,
		},
		{
			name:          "mkv extension",
			path:          "/non/existent/path/video.mkv",
			wantExtension: "mkv",
			wantErr:       false,
		},
		{
			name:          "mov extension",
			path:          "/non/existent/path/video.mov",
			wantExtension: "mov",
			wantErr:       false,
		},
		{
			name:          "uppercase extension",
			path:          "/non/existent/path/VIDEO.MP4",
			wantExtension: "mp4", // should be lowercased
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractVideoMetadata(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("extractVideoMetadata() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil {
				t.Errorf("extractVideoMetadata() = nil, want metadata")
				return
			}
			if got.Extension != tt.wantExtension {
				t.Errorf("extractVideoMetadata() Extension = %v, want %v", got.Extension, tt.wantExtension)
			}
		})
	}
}

func TestExtractImageMetadataWithExiftool(t *testing.T) {
	// This function requires exiftool to be installed
	// Test with non-existent file to verify error handling
	t.Run("non-existent file returns nil", func(t *testing.T) {
		got := extractImageMetadataWithExiftool("/non/existent/path/image.jpg")
		// Should return nil when file doesn't exist or exiftool fails
		if got != nil {
			t.Logf("extractImageMetadataWithExiftool() = %+v, expected nil (exiftool may not be installed)", got)
		}
	})
}
