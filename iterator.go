package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
)

// File represents a file to be processed with source path, target path, and operation flag
type File struct {
	OldPath  string        // Original file path
	NewPath  string        // Target file path after processing
	ShouldOp bool          // Whether any operation should be done (true if old path differs from new path)
	Metadata *FileMetadata // File metadata for processing
	Error    error         // Any error encountered during processing
}

// FileIterator processes files from the configuration and yields File objects through a channel
func FileIterator(cfg *Config) <-chan File {
	ch := make(chan File, 100) // Buffered channel for better performance

	// Worker pool size - use number of CPUs or a reasonable default
	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8 // Cap at 8 workers to avoid overwhelming I/O
	}

	go func() {
		defer close(ch)

		// Channel for file paths to be processed
		filePaths := make(chan fileJob, workerCount*2)

		// Start worker goroutines
		var wg sync.WaitGroup
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range filePaths {
					file := processFile(job.path, job.src, cfg)
					ch <- file
				}
			}()
		}

		// Send file paths to workers
		go func() {
			defer close(filePaths)

			for _, src := range cfg.Sources {
				fmt.Printf("Scanning %s (recurse=%v, types=%v)\n", src.Path, src.Recurse, src.Types)
				files, err := scanFiles(src)
				if err != nil {
					fmt.Printf("Error scanning %s: %v\n", src.Path, err)
					// Send an error file to indicate scanning failed
					ch <- File{
						OldPath: src.Path,
						Error:   err,
					}
					continue
				}

				fmt.Printf("Found %d files:\n", len(files))
				for _, f := range files {
					filePaths <- fileJob{path: f, src: src}
				}
			}
		}()

		// Wait for all workers to finish
		wg.Wait()
	}()

	return ch
}

// fileJob represents a file processing job
type fileJob struct {
	path string
	src  SourceConfig
}

// processFile handles the metadata extraction and path resolution for a single file
func processFile(filePath string, src SourceConfig, cfg *Config) File {
	file := File{
		OldPath: filePath,
	}

	// Try to parse metadata from filename patterns
	var meta *FileMetadata
	for _, pat := range src.Filenames {
		meta = parseMetadataFromFilenamePattern(filepath.Base(filePath), pat)
		if meta != nil {
			break
		}
	}

	// Extract actual metadata based on file type
	var actualMeta *FileMetadata
	var err error
	var targetTmpl string

	if isFileType(filePath, []string{"image"}) {
		actualMeta, err = extractImageMetadata(filePath)
		targetTmpl = cfg.Target.Image.Path
	} else if isFileType(filePath, []string{"video"}) {
		actualMeta, err = extractVideoMetadata(filePath)
		targetTmpl = cfg.Target.Video.Path
	}

	if err != nil {
		file.Error = err
		return file
	}

	// Merge metadata
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

	file.Metadata = meta

	// Resolve target path
	if targetTmpl == "" {
		file.Error = &TargetTemplateError{Path: filePath}
		return file
	}

	targetPath, err := resolveTargetPath(targetTmpl, meta)
	if err != nil {
		file.Error = err
		return file
	}

	file.NewPath = targetPath

	// Determine if operation should be performed
	absSrc, _ := filepath.Abs(filePath)
	absDst, _ := filepath.Abs(targetPath)
	file.ShouldOp = absSrc != absDst

	return file
}

// TargetTemplateError represents an error when target template cannot be determined
type TargetTemplateError struct {
	Path string
}

func (e *TargetTemplateError) Error() string {
	return "could not determine target template for " + e.Path
}
