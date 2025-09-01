package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

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
