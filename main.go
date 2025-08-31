package main

import (
	"fmt"
	"os"
	"path/filepath"
)

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
			var meta *FileMetadata
			for _, pat := range src.Filenames {
				meta = parseMetadataFromFilenamePattern(filepath.Base(f), pat)
				if meta != nil {
					break
				}
			}
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
