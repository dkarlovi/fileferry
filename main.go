package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
)

type SourceConfig struct {
	Path    string   `yaml:"path"`
	Recurse bool     `yaml:"recurse"`
	Types   []string `yaml:"types"`
}

type TargetConfig struct {
	Image TargetPathConfig `yaml:"image"`
	Video TargetPathConfig `yaml:"video"`
}

type TargetPathConfig struct {
	Path string `yaml:"path"`
}

type Config struct {
	Sources []SourceConfig `yaml:"sources"`
	Target  TargetConfig   `yaml:"target"`
}

var imageExts = []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".webp"}
var videoExts = []string{".mp4", ".mov", ".avi", ".mkv", ".webm", ".flv", ".wmv"}

func isFileType(path string, types []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, t := range types {
		switch t {
		case "image":
			for _, e := range imageExts {
				if ext == e {
					return true
				}
			}
		case "video":
			for _, e := range videoExts {
				if ext == e {
					return true
				}
			}
		}
	}
	return false
}

func scanFiles(src SourceConfig) ([]string, error) {
	var files []string
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if !src.Recurse && path != src.Path {
				return filepath.SkipDir
			}
			return nil
		}
		if isFileType(path, src.Types) {
			files = append(files, path)
		}
		return nil
	}
	err := filepath.Walk(src.Path, walkFn)
	return files, err
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfg Config
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: fileferry <config.yaml>")
		os.Exit(1)
	}
	cfg, err := LoadConfig(os.Args[1])
	if err != nil {
		fmt.Printf("Failed to load config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Loaded config: %+v\n", cfg)
	for _, src := range cfg.Sources {
		fmt.Printf("Scanning %s (recurse=%v, types=%v)\n", src.Path, src.Recurse, src.Types)
		files, err := scanFiles(src)
		if err != nil {
			fmt.Printf("Error scanning %s: %v\n", src.Path, err)
			continue
		}
		fmt.Printf("Found %d files:\n", len(files))
		for _, f := range files {
			fmt.Println(f)
		}
	}
}
