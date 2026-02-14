package file

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

var tokenPattern = regexp.MustCompile(`\{[^}]+\}`)

func resolveTargetPath(tmpl string, meta *FileMetadata) (string, error) {
	if meta == nil {
		return "", errors.New("no metadata")
	}
	path := tmpl
	if meta.TakenTime != nil {
		localTime := meta.TakenTime.Local()
		path = strings.ReplaceAll(path, "{meta.taken.year}", localTime.Format("2006"))
		path = strings.ReplaceAll(path, "{meta.taken.date}", localTime.Format("2006-01-02"))
		path = strings.ReplaceAll(path, "{meta.taken.datetime}", localTime.Format("2006-01-02-15-04-05"))
	}
	path = strings.ReplaceAll(path, "{file.extension}", (meta.Extension))
	path = strings.ReplaceAll(path, "{meta.camera.maker}", (meta.CameraMaker))
	path = strings.ReplaceAll(path, "{meta.camera.model}", (meta.CameraModel))

	path = normalizeSeparators(path)
	return path, nil
}

// hasUnpopulatedTokens checks if a path still contains unpopulated template tokens
// It looks for patterns like {token.name} where braces are properly paired.
// Note: This intentionally matches any {*} pattern, not just known template tokens,
// to err on the side of caution. If a path legitimately contains curly braces,
// the file will be skipped with a warning showing the exact path.
func hasUnpopulatedTokens(path string) bool {
	return tokenPattern.MatchString(path)
}

func normalizeSeparators(path string) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	name = collapseSeparators(name, "-")
	name = collapseSeparators(name, "_")

	name = strings.Trim(name, "-_ ")

	newBase := name + ext
	if dir == "." || dir == "" {
		return newBase
	}
	return filepath.Join(dir, newBase)
}

func collapseSeparators(s, sep string) string {
	for strings.Contains(s, sep+sep) {
		s = strings.ReplaceAll(s, sep+sep, sep)
	}
	return s
}

// FileTypeRegistry defines supported file types and their extensions
type FileTypeRegistry struct {
	Categories map[string][]string
}

// DefaultFileTypes provides the default registry with support for images, RAW images, and videos
var DefaultFileTypes = &FileTypeRegistry{
	Categories: map[string][]string{
		"image": {
			// Standard image formats
			".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".webp",
		},
		"image.raw": {
			// RAW image formats
			".dng",         // Adobe Digital Negative (universal RAW)
			".arw",         // Sony RAW
			".cr2", ".cr3", // Canon RAW
			".nef", // Nikon RAW
			".raw", // Generic RAW
			".raf", // Fujifilm RAW
			".orf", // Olympus RAW
			".rw2", // Panasonic RAW
			".pef", // Pentax RAW
			".srw", // Samsung RAW
			".x3f", // Sigma RAW
		},
		"video": {
			".mp4", ".mov", ".avi", ".mkv", ".webm", ".flv", ".wmv",
		},
	},
}

func isFileType(path string, types []string) bool {
	return DefaultFileTypes.IsFileType(path, types)
}

// IsFileType checks if a file matches any of the specified types using this registry
func (r *FileTypeRegistry) IsFileType(path string, types []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, t := range types {
		if extensions, exists := r.Categories[t]; exists {
			for _, e := range extensions {
				if ext == e {
					return true
				}
			}
		}
	}
	return false
}

func scanFiles(src ffcfg.SourceConfig) ([]string, error) {
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
