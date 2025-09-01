package file

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sync"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

type File struct {
	OldPath  string        // Original file path
	NewPath  string        // Target file path after processing
	ShouldOp bool          // Whether any operation should be done (true if old path differs from new path)
	Metadata *FileMetadata // File metadata for processing
	Error    error         // Any error encountered during processing
}

func FileIterator(cfg *ffcfg.Config) <-chan File {
	ch := make(chan File, 100) // Buffered channel for better performance

	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8
	}

	go func() {
		defer close(ch)

		filePaths := make(chan fileJob, workerCount*2)

		var wg sync.WaitGroup
		for i := 0; i < workerCount; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for job := range filePaths {
					file := processFile(job.path, job.src, job.profile, cfg)
					ch <- file
				}
			}()
		}

		go func() {
			defer close(filePaths)

			for profName, prof := range cfg.Profiles {
				for _, src := range prof.Sources {
					fmt.Printf("Scanning profile=%s %s (recurse=%v, types=%v)\n", profName, src.Path, src.Recurse, src.Types)
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
						filePaths <- fileJob{path: f, src: src, profile: profName}
					}
				}
			}
		}()

		wg.Wait()
	}()

	return ch
}

type fileJob struct {
	path    string
	src     ffcfg.SourceConfig
	profile string
}

func processFile(filePath string, src ffcfg.SourceConfig, profileName string, cfg *ffcfg.Config) File {
	file := File{
		OldPath: filePath,
	}

	var meta *FileMetadata
	for _, pat := range src.Filenames {
		meta = parseMetadataFromFilenamePattern(filepath.Base(filePath), pat)
		if meta != nil {
			break
		}
	}
	if meta == nil {
		if prof, ok := cfg.Profiles[profileName]; ok {
			for _, pat := range prof.Patterns {
				meta = parseMetadataFromFilenamePattern(filepath.Base(filePath), pat)
				if meta != nil {
					break
				}
			}
		}
	}

	var actualMeta *FileMetadata
	var err error
	var targetTmpl string

	if isFileType(filePath, []string{"image"}) {
		actualMeta, err = extractImageMetadata(filePath)
	} else if isFileType(filePath, []string{"video"}) {
		actualMeta, err = extractVideoMetadata(filePath)
	}

	if prof, ok := cfg.Profiles[profileName]; ok {
		targetTmpl = prof.Target.Path
	}

	if err != nil {
		file.Error = err
		return file
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

	file.Metadata = meta

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

	absSrc, _ := filepath.Abs(filePath)
	absDst, _ := filepath.Abs(targetPath)
	file.ShouldOp = absSrc != absDst

	return file
}

type TargetTemplateError struct {
	Path string
}

func (e *TargetTemplateError) Error() string {
	return "could not determine target template for " + e.Path
}
