package main

import (
	"fmt"
	"gopkg.in/yaml.v3"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rwcarlsen/goexif/exif"
)
import (
	"errors"
)
func resolveTargetPath(tmpl string, meta *FileMetadata) (string, error) {
	if meta == nil {
		return "", errors.New("no metadata")
	}
	path := tmpl
	if meta.TakenTime != nil {
		path = strings.ReplaceAll(path, "{exif.taken.year}", fmt.Sprintf("%04d", meta.TakenTime.Year()))
		path = strings.ReplaceAll(path, "{exif.taken.date}", meta.TakenTime.Format("2006-01-02"))
		path = strings.ReplaceAll(path, "{exif.taken.datetime}", meta.TakenTime.Format("2006-01-02_15-04-05"))
	}
	path = strings.ReplaceAll(path, "{file.extension}", meta.Extension)
	return path, nil
}
type FileMetadata struct {
	TakenTime *time.Time
	Extension string
}

func extractImageMetadata(path string) (*FileMetadata, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	x, err := exif.Decode(f)
	if err != nil {
		return nil, err
	}
	tm, err := x.DateTime()
	if err != nil {
		return nil, err
	}
	return &FileMetadata{
		TakenTime: &tm,
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}, nil
}

func extractVideoMetadata(path string) (*FileMetadata, error) {
	// TODO: Use ffprobe or similar to extract video metadata
	return &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}, nil
}

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
		fmt.Println("Usage: fileferry <config.yaml> [--ack]")
		os.Exit(1)
	}
	ack := false
	configPath := ""
	for _, arg := range os.Args[1:] {
		if arg == "--ack" {
			ack = true
		} else if configPath == "" {
			configPath = arg
		}
	}
	if configPath == "" {
		fmt.Println("Usage: fileferry <config.yaml> [--ack]")
		os.Exit(1)
	}
	cfg, err := LoadConfig(configPath)
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
			var meta *FileMetadata
			var err error
			var targetTmpl string
			if isFileType(f, []string{"image"}) {
				meta, err = extractImageMetadata(f)
				targetTmpl = cfg.Target.Image.Path
			} else if isFileType(f, []string{"video"}) {
				meta, err = extractVideoMetadata(f)
				targetTmpl = cfg.Target.Video.Path
			}
			if err != nil {
				fmt.Printf("%s: metadata error: %v\n", f, err)
				continue
			}
			targetPath, err := resolveTargetPath(targetTmpl, meta)
			if err != nil {
				fmt.Printf("%s: target path error: %v\n", f, err)
				continue
			}
			dir := filepath.Dir(targetPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("%s: failed to create dir %s: %v\n", f, dir, err)
				continue
			}
			if ack {
				fmt.Printf("Moving %s -> %s\n", f, targetPath)
				if err := os.Rename(f, targetPath); err != nil {
					fmt.Printf("%s: failed to move: %v\n", f, err)
					continue
				}
			} else {
				fmt.Printf("Would move %s -> %s (use --ack to actually move)\n", f, targetPath)
			}
		}
	}
}
