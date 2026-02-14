package file

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

func TestTargetTemplateError_Error(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple path",
			path:     "/path/to/file.jpg",
			expected: "could not determine target template for /path/to/file.jpg",
		},
		{
			name:     "empty path",
			path:     "",
			expected: "could not determine target template for ",
		},
		{
			name:     "relative path",
			path:     "relative/file.jpg",
			expected: "could not determine target template for relative/file.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &TargetTemplateError{Path: tt.path}
			result := err.Error()
			if result != tt.expected {
				t.Errorf("TargetTemplateError.Error() = %q; want %q", result, tt.expected)
			}
		})
	}
}

func TestUnpopulatedTokensError_Error(t *testing.T) {
	tests := []struct {
		name       string
		path       string
		targetPath string
		expected   string
	}{
		{
			name:       "simple unpopulated token",
			path:       "/source/file.jpg",
			targetPath: "/organized/{meta.taken.year}/file.jpg",
			expected:   "skipping file because target path contains unpopulated tokens: /organized/{meta.taken.year}/file.jpg",
		},
		{
			name:       "multiple unpopulated tokens",
			path:       "/source/file.png",
			targetPath: "/organized/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.png",
			expected:   "skipping file because target path contains unpopulated tokens: /organized/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &UnpopulatedTokensError{Path: tt.path, TargetPath: tt.targetPath}
			result := err.Error()
			if result != tt.expected {
				t.Errorf("UnpopulatedTokensError.Error() = %q; want %q", result, tt.expected)
			}
		})
	}
}

func TestProcessFile(t *testing.T) {
	// Create a temporary directory for test files
	tmpDir := t.TempDir()
	testImagePath := filepath.Join(tmpDir, "test.jpg")
	testVideoPath := filepath.Join(tmpDir, "test.mp4")

	// Create test files
	if err := os.WriteFile(testImagePath, []byte("fake image"), 0644); err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	if err := os.WriteFile(testVideoPath, []byte("fake video"), 0644); err != nil {
		t.Fatalf("Failed to create test video: %v", err)
	}

	tests := []struct {
		name        string
		filePath    string
		src         ffcfg.SourceConfig
		profileName string
		cfg         *ffcfg.Config
		wantErr     bool
		checkPath   bool
	}{
		{
			name:        "missing profile",
			filePath:    testImagePath,
			src:         ffcfg.SourceConfig{},
			profileName: "nonexistent",
			cfg: &ffcfg.Config{
				Profiles: map[string]ffcfg.ProfileConfig{},
			},
			wantErr:   true,
			checkPath: false,
		},
		{
			name:        "valid image with target template",
			filePath:    testImagePath,
			src:         ffcfg.SourceConfig{},
			profileName: "test-profile",
			cfg: &ffcfg.Config{
				Profiles: map[string]ffcfg.ProfileConfig{
					"test-profile": {
						Target: ffcfg.TargetPathConfig{
							Path: "/target/{file.extension}",
						},
					},
				},
			},
			wantErr:   false,
			checkPath: true,
		},
		{
			name:        "valid video with target template",
			filePath:    testVideoPath,
			src:         ffcfg.SourceConfig{},
			profileName: "video-profile",
			cfg: &ffcfg.Config{
				Profiles: map[string]ffcfg.ProfileConfig{
					"video-profile": {
						Target: ffcfg.TargetPathConfig{
							Path: "/videos/{file.extension}",
						},
					},
				},
			},
			wantErr:   false,
			checkPath: true,
		},
		{
			name:     "filename pattern extraction",
			filePath: filepath.Join(tmpDir, "2024-01-15.jpg"),
			src: ffcfg.SourceConfig{
				Filenames: []string{"{meta.taken.date}.jpg"},
			},
			profileName: "pattern-profile",
			cfg: &ffcfg.Config{
				Profiles: map[string]ffcfg.ProfileConfig{
					"pattern-profile": {
						Target: ffcfg.TargetPathConfig{
							Path: "/organized/{meta.taken.year}/{file.extension}",
						},
					},
				},
			},
			wantErr:   false,
			checkPath: true,
		},
		{
			name:        "profile-level pattern extraction",
			filePath:    filepath.Join(tmpDir, "2024-01-15.jpg"),
			src:         ffcfg.SourceConfig{},
			profileName: "profile-pattern",
			cfg: &ffcfg.Config{
				Profiles: map[string]ffcfg.ProfileConfig{
					"profile-pattern": {
						Patterns: []string{"{meta.taken.date}.jpg"},
						Target: ffcfg.TargetPathConfig{
							Path: "/organized/{meta.taken.year}/{file.extension}",
						},
					},
				},
			},
			wantErr:   false,
			checkPath: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create the file if it doesn't exist for pattern tests
			if _, err := os.Stat(tt.filePath); os.IsNotExist(err) {
				if err := os.WriteFile(tt.filePath, []byte("test"), 0644); err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			result := processFile(tt.filePath, tt.src, tt.profileName, tt.cfg)

			if tt.wantErr && result.Error == nil {
				t.Error("processFile() expected error but got none")
			}
			if !tt.wantErr && result.Error != nil {
				t.Errorf("processFile() unexpected error: %v", result.Error)
			}

			if result.OldPath != tt.filePath {
				t.Errorf("processFile() OldPath = %q; want %q", result.OldPath, tt.filePath)
			}

			if tt.checkPath && !tt.wantErr {
				if result.NewPath == "" {
					t.Error("processFile() NewPath is empty")
				}
			}
		})
	}
}

func TestFileIterator(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()
	testFile1 := filepath.Join(tmpDir, "test1.jpg")
	testFile2 := filepath.Join(tmpDir, "test2.mp4")

	if err := os.WriteFile(testFile1, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"test": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    tmpDir,
						Recurse: false,
						Types:   []string{"image", "video"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: "/target/{file.extension}",
				},
			},
		},
	}

	ch := FileIterator(cfg)
	if ch == nil {
		t.Fatal("FileIterator() returned nil channel")
	}

	count := 0
	for file := range ch {
		count++
		if file.OldPath == "" {
			t.Error("FileIterator() returned file with empty OldPath")
		}
	}

	if count != 2 {
		t.Errorf("FileIterator() processed %d files; want 2", count)
	}
}

func TestFileIteratorWithEvents(t *testing.T) {
	// Create a temporary directory with test files
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.jpg")

	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"test": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    tmpDir,
						Recurse: false,
						Types:   []string{"image"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: "/target/{file.extension}",
				},
			},
		},
	}

	fileCh, eventCh := FileIteratorWithEvents(cfg, "")
	if fileCh == nil {
		t.Fatal("FileIteratorWithEvents() returned nil file channel")
	}
	if eventCh == nil {
		t.Fatal("FileIteratorWithEvents() returned nil event channel")
	}

	// Collect events in a separate goroutine
	eventsDone := make(chan []ScanEvent)
	go func() {
		events := []ScanEvent{}
		for ev := range eventCh {
			events = append(events, ev)
		}
		eventsDone <- events
	}()

	// Collect files
	files := []File{}
	for file := range fileCh {
		files = append(files, file)
	}

	// Wait for events to be collected
	events := <-eventsDone

	if len(files) != 1 {
		t.Errorf("FileIteratorWithEvents() processed %d files; want 1", len(files))
	}

	// Check that we got events
	if len(events) < 2 {
		t.Errorf("FileIteratorWithEvents() produced %d events; want at least 2 (start, found)", len(events))
	}

	// Verify event types
	hasStart := false
	hasFound := false
	for _, ev := range events {
		if ev.EventType == "start" {
			hasStart = true
			if ev.Profile != "test" {
				t.Errorf("Start event has profile %q; want %q", ev.Profile, "test")
			}
			if ev.SrcPath != tmpDir {
				t.Errorf("Start event has SrcPath %q; want %q", ev.SrcPath, tmpDir)
			}
		}
		if ev.EventType == "found" {
			hasFound = true
			if ev.Found != 1 {
				t.Errorf("Found event has Found=%d; want 1", ev.Found)
			}
		}
	}

	if !hasStart {
		t.Error("FileIteratorWithEvents() did not produce a start event")
	}
	if !hasFound {
		t.Error("FileIteratorWithEvents() did not produce a found event")
	}
}

func TestFileIteratorWithEvents_ScanError(t *testing.T) {
	// Use a non-existent directory to trigger an error
	nonExistentDir := "/this/path/does/not/exist/for/testing"

	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"error-test": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    nonExistentDir,
						Recurse: false,
						Types:   []string{"image"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: "/target/{file.extension}",
				},
			},
		},
	}

	fileCh, eventCh := FileIteratorWithEvents(cfg, "")

	// Collect events in a separate goroutine
	eventsDone := make(chan []ScanEvent)
	go func() {
		events := []ScanEvent{}
		for ev := range eventCh {
			events = append(events, ev)
		}
		eventsDone <- events
	}()

	// Collect files
	hasErrorFile := false
	for file := range fileCh {
		if file.Error != nil {
			hasErrorFile = true
		}
	}

	// Wait for events to be collected
	events := <-eventsDone

	if !hasErrorFile {
		t.Error("Expected file with error, got none")
	}

	// Check that we got an error event
	hasError := false
	for _, ev := range events {
		if ev.EventType == "error" {
			hasError = true
			if ev.Error == nil {
				t.Error("Error event has nil Error field")
			}
		}
	}

	if !hasError {
		t.Error("FileIteratorWithEvents() did not produce an error event for non-existent directory")
	}
}

func TestFileIteratorWithEvents_EmptyConfig(t *testing.T) {
	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{},
	}

	fileCh, eventCh := FileIteratorWithEvents(cfg, "")

	// Channels should be closed immediately
	fileCount := 0
	for range fileCh {
		fileCount++
	}

	eventCount := 0
	for range eventCh {
		eventCount++
	}

	if fileCount != 0 {
		t.Errorf("FileIteratorWithEvents() with empty config produced %d files; want 0", fileCount)
	}

	if eventCount != 0 {
		t.Errorf("FileIteratorWithEvents() with empty config produced %d events; want 0", eventCount)
	}
}

func TestFileIteratorWithEvents_Recursion(t *testing.T) {
	// Create a nested directory structure
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	if err := os.Mkdir(subDir, 0755); err != nil {
		t.Fatalf("Failed to create subdir: %v", err)
	}

	testFile1 := filepath.Join(tmpDir, "test1.jpg")
	testFile2 := filepath.Join(subDir, "test2.jpg")

	if err := os.WriteFile(testFile1, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if err := os.WriteFile(testFile2, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Test with recursion enabled
	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"recursive": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    tmpDir,
						Recurse: true,
						Types:   []string{"image"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: "/target/{file.extension}",
				},
			},
		},
	}

	fileCh, eventCh := FileIteratorWithEvents(cfg, "")

	// Consume events
	go func() {
		for range eventCh {
		}
	}()

	// Collect files
	files := []File{}
	for file := range fileCh {
		files = append(files, file)
	}

	if len(files) != 2 {
		t.Errorf("FileIteratorWithEvents() with recursion found %d files; want 2", len(files))
	}

	// Test without recursion
	cfgNoRecurse := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"no-recurse": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    tmpDir,
						Recurse: false,
						Types:   []string{"image"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: "/target/{file.extension}",
				},
			},
		},
	}

	fileCh2, eventCh2 := FileIteratorWithEvents(cfgNoRecurse, "")

	// Consume events
	go func() {
		for range eventCh2 {
		}
	}()

	// Collect files
	files2 := []File{}
	for file := range fileCh2 {
		files2 = append(files2, file)
	}

	if len(files2) != 1 {
		t.Errorf("FileIteratorWithEvents() without recursion found %d files; want 1", len(files2))
	}
}

func TestFileIteratorWithEvents_ProfileFilter(t *testing.T) {
	// Create test directory structure
	tmpDir := t.TempDir()
	videosDir := filepath.Join(tmpDir, "videos")
	imagesDir := filepath.Join(tmpDir, "images")

	if err := os.MkdirAll(videosDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create test files
	videoFile := filepath.Join(videosDir, "test.mp4")
	imageFile := filepath.Join(imagesDir, "test.jpg")
	if err := os.WriteFile(videoFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(imageFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create config with two profiles
	cfg := &ffcfg.Config{
		Profiles: map[string]ffcfg.ProfileConfig{
			"videos": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    videosDir,
						Recurse: false,
						Types:   []string{"video"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: filepath.Join(tmpDir, "organized", "videos", "{file.extension}"),
				},
			},
			"images": {
				Sources: []ffcfg.SourceConfig{
					{
						Path:    imagesDir,
						Recurse: false,
						Types:   []string{"image"},
					},
				},
				Target: ffcfg.TargetPathConfig{
					Path: filepath.Join(tmpDir, "organized", "images", "{file.extension}"),
				},
			},
		},
	}

	// Test filtering by "videos" profile
	fileCh, eventCh := FileIteratorWithEvents(cfg, "videos")

	// Consume events
	eventsDone := make(chan []ScanEvent)
	go func() {
		events := []ScanEvent{}
		for ev := range eventCh {
			events = append(events, ev)
		}
		eventsDone <- events
	}()

	// Collect files
	files := []File{}
	for f := range fileCh {
		files = append(files, f)
	}

	events := <-eventsDone

	// Verify only videos profile was processed
	if len(files) != 1 {
		t.Errorf("Expected 1 file when filtering by videos profile, got %d", len(files))
	}

	if len(files) > 0 && !strings.HasSuffix(files[0].OldPath, "test.mp4") {
		t.Errorf("Expected video file, got %s", files[0].OldPath)
	}

	// Verify events are only for videos profile
	for _, ev := range events {
		if ev.Profile != "videos" {
			t.Errorf("Expected all events to be for videos profile, got event for %s", ev.Profile)
		}
	}

	// Test filtering by "images" profile
	fileCh2, eventCh2 := FileIteratorWithEvents(cfg, "images")

	// Consume events
	eventsDone2 := make(chan []ScanEvent)
	go func() {
		events := []ScanEvent{}
		for ev := range eventCh2 {
			events = append(events, ev)
		}
		eventsDone2 <- events
	}()

	// Collect files
	files2 := []File{}
	for f := range fileCh2 {
		files2 = append(files2, f)
	}

	events2 := <-eventsDone2

	// Verify only images profile was processed
	if len(files2) != 1 {
		t.Errorf("Expected 1 file when filtering by images profile, got %d", len(files2))
	}

	if len(files2) > 0 && !strings.HasSuffix(files2[0].OldPath, "test.jpg") {
		t.Errorf("Expected image file, got %s", files2[0].OldPath)
	}

	// Verify events are only for images profile
	for _, ev := range events2 {
		if ev.Profile != "images" {
			t.Errorf("Expected all events to be for images profile, got event for %s", ev.Profile)
		}
	}

	// Test with empty profile name (should process all profiles)
	fileCh3, eventCh3 := FileIteratorWithEvents(cfg, "")

	// Consume events
	eventsDone3 := make(chan []ScanEvent)
	go func() {
		events := []ScanEvent{}
		for ev := range eventCh3 {
			events = append(events, ev)
		}
		eventsDone3 <- events
	}()

	// Collect files
	files3 := []File{}
	for f := range fileCh3 {
		files3 = append(files3, f)
	}

	events3 := <-eventsDone3

	// Verify both profiles were processed
	if len(files3) != 2 {
		t.Errorf("Expected 2 files when not filtering by profile, got %d", len(files3))
	}

	// Verify we got events from both profiles
	profilesSeen := make(map[string]bool)
	for _, ev := range events3 {
		profilesSeen[ev.Profile] = true
	}
	if !profilesSeen["videos"] || !profilesSeen["images"] {
		t.Errorf("Expected events from both profiles, got events from: %v", profilesSeen)
	}
}
