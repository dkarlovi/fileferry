package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type FilenameMetaRule struct {
	Path string
	Exp  string
}

var FilenameMetaRules = []FilenameMetaRule{
	{Path: "meta.taken.date", Exp: `\\d{4}-\\d{2}-\\d{2}`},
	{Path: "meta.taken.time", Exp: `\\d{2}-\\d{2}-\\d{2}`},
}

func resolveTargetPath(tmpl string, meta *FileMetadata) (string, error) {
	if meta == nil {
		return "", errors.New("no metadata")
	}
	path := tmpl
	if meta.TakenTime != nil {
		localTime := meta.TakenTime.Local()
		path = strings.ReplaceAll(path, "{meta.taken.year}", fmt.Sprintf("%04d", localTime.Year()))
		path = strings.ReplaceAll(path, "{meta.taken.date}", localTime.Format("2006-01-02"))
		path = strings.ReplaceAll(path, "{meta.taken.datetime}", localTime.Format("2006-01-02-15-04-05"))
	}
	path = strings.ReplaceAll(path, "{file.extension}", (meta.Extension))
	path = strings.ReplaceAll(path, "{meta.camera.maker}", (meta.CameraMaker))
	path = strings.ReplaceAll(path, "{meta.camera.model}", (meta.CameraModel))

	path = normalizeSeparators(path)
	return path, nil
}

func normalizeSeparators(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = collapseSeparators(name, "-")
	name = collapseSeparators(name, "_")

	name = strings.Trim(name, "-_ ")

	// Rebuild the path
	newBase := name + ext
	if dir == "." || dir == "" {
		return newBase
	}
	return filepath.Join(dir, newBase)
}

// collapseSeparators replaces multiple consecutive sep with a single sep
func collapseSeparators(s, sep string) string {
	for strings.Contains(s, sep+sep) {
		s = strings.ReplaceAll(s, sep+sep, sep)
	}
	return s
}

func extractVideoMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	// Call ffprobe to get all format tags
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return meta, nil // Return what we have, but no date
	}
	var ffprobe struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &ffprobe); err != nil {
		return meta, nil
	}
	tags := ffprobe.Format.Tags

	// Helper to look up the first non-empty value from a list of possible tag keys
	lookup := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := tags[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}

	// Arrays of possible tag keys for each field
	makerKeys := []string{"com.android.manufacturer", "make", "manufacturer"}
	modelKeys := []string{"com.android.model", "model"}
	creationTimeKeys := []string{"creation_time"}

	meta.CameraMaker = lookup(makerKeys...)
	meta.CameraModel = lookup(modelKeys...)

	ct := lookup(creationTimeKeys...)
	if ct != "" {
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04:05.000000Z",
			"2006-01-02 15:04:05",
		}
		var tm time.Time
		var parseErr error
		for _, layout := range layouts {
			tm, parseErr = time.Parse(layout, ct)
			if parseErr == nil {
				localTm := tm.Local()
				meta.TakenTime = &localTm
				break
			}
		}
	}
	return meta, nil
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
	skipped := 0
	moved := 0
	for _, src := range cfg.Sources {
		fmt.Printf("Scanning %s (recurse=%v, types=%v)\n", src.Path, src.Recurse, src.Types)
		files, err := scanFiles(src)
		if err != nil {
			fmt.Printf("Error scanning %s: %v\n", src.Path, err)
			continue
		}
		fmt.Printf("Found %d files:\n", len(files))
		for _, f := range files {
			// 1. Prepopulate metadata from filename patterns (if provided)
			var meta *FileMetadata
			for _, pat := range src.Filenames {
				meta = parseMetadataFromFilenamePattern(filepath.Base(f), pat)
				if meta != nil {
					break
				}
			}
			// 2. Add to/override with actual file metadata
			var actualMeta *FileMetadata
			var err error
			var targetTmpl string
			if isFileType(f, []string{"image"}) {
				actualMeta, err = extractImageMetadata(f)
				targetTmpl = cfg.Target.Image.Path
			} else if isFileType(f, []string{"video"}) {
				actualMeta, err = extractVideoMetadata(f)
				targetTmpl = cfg.Target.Video.Path
			}
			if err != nil {
				fmt.Printf("%s: metadata error: %v\n", f, err)
				continue
			}
			if actualMeta != nil {
				if meta == nil {
					meta = actualMeta
				} else {
					// Override/extend fields from actualMeta
					if actualMeta.TakenTime != nil {
						meta.TakenTime = actualMeta.TakenTime
					}
					if actualMeta.Extension != "" {
						meta.Extension = actualMeta.Extension
					}
					if actualMeta.CameraMaker != "" {
						meta.CameraMaker = actualMeta.CameraMaker
					}
					if actualMeta.CameraModel != "" {
						meta.CameraModel = actualMeta.CameraModel
					}
				}
			}
			if targetTmpl == "" {
				// If not set, skip this file
				fmt.Printf("%s: could not determine target template\n", f)
				skipped++
				continue
			}
			targetPath, err := resolveTargetPath(targetTmpl, meta)
			if err != nil {
				fmt.Printf("%s: target path error: %v\n", f, err)
				skipped++
				continue
			}

			// Skip if current path is already the target path
			absSrc, _ := filepath.Abs(f)
			absDst, _ := filepath.Abs(targetPath)
			if absSrc == absDst {
				skipped++
				continue
			}
			dir := filepath.Dir(targetPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				fmt.Printf("%s: failed to create dir %s: %v\n", f, dir, err)
				skipped++
				continue
			}
			if ack {
				fmt.Printf("Moving %s -> %s\n", f, targetPath)
				if err := os.Rename(f, targetPath); err != nil {
					fmt.Printf("%s: failed to move: %v\n", f, err)
					skipped++
					continue
				}
				moved++
			} else {
				fmt.Printf("Would move %s -> %s (use --ack to actually move)\n", f, targetPath)
				moved++
			}
		}
	}
	fmt.Printf("Summary: %d moved, %d skipped.\n", moved, skipped)
}

func parseMetadataFromFilenamePattern(filename, pattern string) *FileMetadata {
	ext := filepath.Ext(filename)
	name := strings.TrimSuffix(filename, ext)
	// Build a regex from the pattern and the rules
	regexPattern := pattern
	for _, rule := range FilenameMetaRules {
		regexPattern = strings.ReplaceAll(regexPattern, "{"+rule.Path+"}", "(?P<"+rule.Path+">"+rule.Exp+")")
	}
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil
	}
	match := re.FindStringSubmatch(name)
	if match == nil {
		return nil
	}
	groups := make(map[string]string)
	for i, n := range re.SubexpNames() {
		if i > 0 && n != "" {
			groups[n] = match[i]
		}
	}
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(ext, "."),
	}
	if date, ok := groups["meta.taken.date"]; ok {
		if t, ok2 := groups["meta.taken.time"]; ok2 {
			tm, err := time.Parse("2006-01-02 15-04-05", date+" "+t)
			if err == nil {
				meta.TakenTime = &tm
			}
		} else {
			tm, err := time.Parse("2006-01-02", date)
			if err == nil {
				meta.TakenTime = &tm
			}
		}
	}
	// Add more fields as needed
	if meta.TakenTime != nil {
		return meta
	}
	return nil
}
