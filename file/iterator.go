package file

import (
	"path/filepath"
	"runtime"
	"sync"

	ffcfg "github.com/dkarlovi/fileferry/config"
)

type File struct {
	OldPath  string
	NewPath  string
	ShouldOp bool
	Metadata *FileMetadata
	Error    error
}

func FileIterator(cfg *ffcfg.Config) <-chan File {
	ch, _ := FileIteratorWithEvents(cfg, "")
	return ch
}

// ScanEvent is emitted by FileIteratorWithEvents for progress reporting.
type ScanEvent struct {
	Profile   string
	SrcPath   string
	Recurse   bool
	Types     []string
	Found     int    // number of files found (if >=0)
	Error     error  // optional error that happened while scanning
	EventType string // one of: "start", "found", "error"
}

// FileIteratorWithEvents returns a channel of Files and a channel of ScanEvent.
// The file package itself does not format or print events; callers may consume
// events and colorize/print them as desired.
// If profileName is non-empty, only that profile is processed.
func FileIteratorWithEvents(cfg *ffcfg.Config, profileName string) (<-chan File, <-chan ScanEvent) {
	ch := make(chan File, 100)
	evCh := make(chan ScanEvent, 100)

	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8
	}

	go func() {
		defer close(ch)
		defer close(evCh)

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
				// Skip this profile if profileName filter is set and doesn't match
				if profileName != "" && profName != profileName {
					continue
				}

				for _, src := range prof.Sources {
					evCh <- ScanEvent{Profile: profName, SrcPath: src.Path, Recurse: src.Recurse, Types: src.Types, EventType: "start"}
					files, err := scanFiles(src)
					if err != nil {
						evCh <- ScanEvent{Profile: profName, SrcPath: src.Path, EventType: "error", Error: err}
						ch <- File{
							OldPath: src.Path,
							Error:   err,
						}
						continue
					}

					evCh <- ScanEvent{Profile: profName, SrcPath: src.Path, Found: len(files), EventType: "found"}
					for _, f := range files {
						filePaths <- fileJob{path: f, src: src, profile: profName}
					}
				}
			}
		}()

		wg.Wait()
	}()

	return ch, evCh
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
