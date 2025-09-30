package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_Valid(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	validYAML := `profiles:
  Videos:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
    patterns:
      - "{meta.taken.date} {meta.taken.time}.mkv"
    target:
      path: /organized/videos/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
  Pictures:
    sources:
      - path: /path/to/pictures
        recurse: false
        types: [image]
    target:
      path: /organized/pictures/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
`

	if err := os.WriteFile(configPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, expected nil", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfig() returned nil config")
	}

	// Verify profiles were loaded
	if len(cfg.Profiles) != 2 {
		t.Errorf("Expected 2 profiles, got %d", len(cfg.Profiles))
	}

	// Verify Videos profile
	if videos, ok := cfg.Profiles["Videos"]; ok {
		if len(videos.Sources) != 1 {
			t.Errorf("Videos profile: expected 1 source, got %d", len(videos.Sources))
		}
		if videos.Sources[0].Path != "/path/to/videos" {
			t.Errorf("Videos profile: expected path '/path/to/videos', got '%s'", videos.Sources[0].Path)
		}
		if !videos.Sources[0].Recurse {
			t.Error("Videos profile: expected recurse to be true")
		}
		if len(videos.Sources[0].Types) != 1 || videos.Sources[0].Types[0] != "video" {
			t.Errorf("Videos profile: expected types [video], got %v", videos.Sources[0].Types)
		}
		if len(videos.Patterns) != 1 {
			t.Errorf("Videos profile: expected 1 pattern, got %d", len(videos.Patterns))
		}
		if videos.Target.Path != "/organized/videos/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}" {
			t.Errorf("Videos profile: unexpected target path: %s", videos.Target.Path)
		}
	} else {
		t.Error("Videos profile not found")
	}

	// Verify Pictures profile
	if pictures, ok := cfg.Profiles["Pictures"]; ok {
		if len(pictures.Sources) != 1 {
			t.Errorf("Pictures profile: expected 1 source, got %d", len(pictures.Sources))
		}
		if pictures.Sources[0].Path != "/path/to/pictures" {
			t.Errorf("Pictures profile: expected path '/path/to/pictures', got '%s'", pictures.Sources[0].Path)
		}
		if pictures.Sources[0].Recurse {
			t.Error("Pictures profile: expected recurse to be false")
		}
		if len(pictures.Patterns) != 0 {
			t.Errorf("Pictures profile: expected 0 patterns, got %d", len(pictures.Patterns))
		}
	} else {
		t.Error("Pictures profile not found")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("LoadConfig() expected error for nonexistent file, got nil")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	invalidYAML := `profiles:
  Videos:
    sources: [invalid yaml structure
`

	if err := os.WriteFile(configPath, []byte(invalidYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for invalid YAML, got nil")
	}
}

func TestLoadConfig_MissingTargetPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlMissingTarget := `profiles:
  Videos:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
`

	if err := os.WriteFile(configPath, []byte(yamlMissingTarget), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for missing target.path, got nil")
	}
	if !strings.Contains(err.Error(), "missing target.path") {
		t.Errorf("Expected error about missing target.path, got: %v", err)
	}
}

func TestLoadConfig_EmptySourcePath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlEmptySource := `profiles:
  Videos:
    sources:
      - path: ""
        recurse: true
        types: [video]
    target:
      path: /organized/videos
`

	if err := os.WriteFile(configPath, []byte(yamlEmptySource), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for empty source path, got nil")
	}
	if !strings.Contains(err.Error(), "source path is empty") {
		t.Errorf("Expected error about empty source path, got: %v", err)
	}
}

func TestLoadConfig_DuplicateSourcePath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlDuplicateSource := `profiles:
  Videos:
    sources:
      - path: /shared/path
        recurse: true
        types: [video]
    target:
      path: /organized/videos
  Pictures:
    sources:
      - path: /shared/path
        recurse: false
        types: [image]
    target:
      path: /organized/pictures
`

	if err := os.WriteFile(configPath, []byte(yamlDuplicateSource), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	_, err := LoadConfig(configPath)
	if err == nil {
		t.Fatal("LoadConfig() expected error for duplicate source path, got nil")
	}
	if !strings.Contains(err.Error(), "source path") || !strings.Contains(err.Error(), "defined in profile") {
		t.Errorf("Expected error about duplicate source path, got: %v", err)
	}
}

func TestLoadConfig_MultipleSourcesInProfile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlMultipleSources := `profiles:
  Media:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
      - path: /path/to/pictures
        recurse: false
        types: [image]
    target:
      path: /organized/media
`

	if err := os.WriteFile(configPath, []byte(yamlMultipleSources), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	media, ok := cfg.Profiles["Media"]
	if !ok {
		t.Fatal("Media profile not found")
	}

	if len(media.Sources) != 2 {
		t.Errorf("Expected 2 sources in Media profile, got %d", len(media.Sources))
	}
}

func TestLoadConfig_OptionalFields(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	yamlWithOptionals := `profiles:
  Videos:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
        filenames:
          - "{meta.taken.date} {meta.taken.time}.mkv"
    patterns:
      - "{meta.taken.date}-{meta.taken.time}.mkv"
    target:
      path: /organized/videos
`

	if err := os.WriteFile(configPath, []byte(yamlWithOptionals), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("LoadConfig() unexpected error: %v", err)
	}

	videos, ok := cfg.Profiles["Videos"]
	if !ok {
		t.Fatal("Videos profile not found")
	}

	if len(videos.Sources[0].Filenames) != 1 {
		t.Errorf("Expected 1 filename pattern, got %d", len(videos.Sources[0].Filenames))
	}

	if len(videos.Patterns) != 1 {
		t.Errorf("Expected 1 pattern, got %d", len(videos.Patterns))
	}
}

func TestLoadConfigPrefer_PreferredPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "my-config.yaml")

	validYAML := `profiles:
  Test:
    sources:
      - path: /test/path
        recurse: true
        types: [image]
    target:
      path: /organized/test
`

	if err := os.WriteFile(configPath, []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := LoadConfigPrefer(configPath)
	if err != nil {
		t.Fatalf("LoadConfigPrefer() error = %v, expected nil", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfigPrefer() returned nil config")
	}

	if len(cfg.Profiles) != 1 {
		t.Errorf("Expected 1 profile, got %d", len(cfg.Profiles))
	}
}

func TestLoadConfigPrefer_CurrentDirectory(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	// Create a temporary directory and change to it
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create config.yaml in current directory
	validYAML := `profiles:
  Test:
    sources:
      - path: /test/path
        recurse: true
        types: [image]
    target:
      path: /organized/test
`

	if err := os.WriteFile("config.yaml", []byte(validYAML), 0644); err != nil {
		t.Fatalf("Failed to create config.yaml: %v", err)
	}

	// LoadConfigPrefer with empty preferred path should find config.yaml in current directory
	cfg, err := LoadConfigPrefer("")
	if err != nil {
		t.Fatalf("LoadConfigPrefer() error = %v, expected nil", err)
	}

	if cfg == nil {
		t.Fatal("LoadConfigPrefer() returned nil config")
	}
}

func TestLoadConfigPrefer_NoConfigFound(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	// Create a temporary directory with no config file and change to it
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Try to load config with a nonexistent preferred path
	_, err = LoadConfigPrefer("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("LoadConfigPrefer() expected error when no config found, got nil")
	}

	if !strings.Contains(err.Error(), "no config file found") {
		t.Errorf("Expected error message about no config found, got: %v", err)
	}

	// Error should mention the paths that were tried
	if !strings.Contains(err.Error(), "tried:") {
		t.Errorf("Expected error to list tried paths, got: %v", err)
	}
}

func TestLoadConfigPrefer_PreferredOverCurrent(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	// Create a temporary directory and change to it
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create config.yaml in current directory
	currentYAML := `profiles:
  Current:
    sources:
      - path: /current/path
        recurse: true
        types: [image]
    target:
      path: /organized/current
`
	if err := os.WriteFile("config.yaml", []byte(currentYAML), 0644); err != nil {
		t.Fatalf("Failed to create current config.yaml: %v", err)
	}

	// Create a preferred config file
	preferredPath := filepath.Join(tmpDir, "preferred.yaml")
	preferredYAML := `profiles:
  Preferred:
    sources:
      - path: /preferred/path
        recurse: true
        types: [image]
    target:
      path: /organized/preferred
`
	if err := os.WriteFile(preferredPath, []byte(preferredYAML), 0644); err != nil {
		t.Fatalf("Failed to create preferred config: %v", err)
	}

	// LoadConfigPrefer should use the preferred path
	cfg, err := LoadConfigPrefer(preferredPath)
	if err != nil {
		t.Fatalf("LoadConfigPrefer() error = %v, expected nil", err)
	}

	// Verify it loaded the preferred config, not the current directory one
	if _, ok := cfg.Profiles["Preferred"]; !ok {
		t.Error("Expected Preferred profile from preferred config file")
	}
	if _, ok := cfg.Profiles["Current"]; ok {
		t.Error("Should not have loaded Current profile from current directory")
	}
}

func TestLoadConfigPrefer_InvalidPreferredFallsBackToCurrent(t *testing.T) {
	// Save current directory
	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Failed to get current directory: %v", err)
	}
	defer os.Chdir(originalDir)

	// Create a temporary directory and change to it
	tmpDir := t.TempDir()
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("Failed to change to temp directory: %v", err)
	}

	// Create valid config.yaml in current directory
	currentYAML := `profiles:
  Current:
    sources:
      - path: /current/path
        recurse: true
        types: [image]
    target:
      path: /organized/current
`
	if err := os.WriteFile("config.yaml", []byte(currentYAML), 0644); err != nil {
		t.Fatalf("Failed to create current config.yaml: %v", err)
	}

	// Try with nonexistent preferred path - should fall back to current directory
	cfg, err := LoadConfigPrefer("/nonexistent/preferred.yaml")
	if err != nil {
		t.Fatalf("LoadConfigPrefer() error = %v, expected to fall back to current directory", err)
	}

	// Verify it loaded the current directory config
	if _, ok := cfg.Profiles["Current"]; !ok {
		t.Error("Expected to fall back to Current profile from current directory")
	}
}
