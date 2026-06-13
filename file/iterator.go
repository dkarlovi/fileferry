package file

import (
	"io"
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
	Entry    Entry
	Error    error
}

// FileIterator is a convenience wrapper returning only the file channel. It is
// intended for local sources (whose Close is a no-op and whose entries do not
// depend on an open session), so the source closer is intentionally dropped.
func FileIterator(cfg *ffcfg.Config) <-chan File {
	ch, _, _ := FileIteratorWithEvents(cfg, "")
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

// FileIteratorWithEvents returns a channel of Files, a channel of ScanEvent, and
// an io.Closer that releases all opened sources. The file package itself does
// not format or print events; callers may consume events and colorize/print
// them as desired.
//
// For MTP sources the device session must stay alive while the returned Entries
// are read and moved, so callers MUST NOT call the io.Closer until all moves are
// complete (defer it). If profileName is non-empty, only that profile is
// processed.
func FileIteratorWithEvents(cfg *ffcfg.Config, profileName string) (<-chan File, <-chan ScanEvent, io.Closer) {
	ch := make(chan File, 100)
	evCh := make(chan ScanEvent, 100)

	workerCount := runtime.NumCPU()
	if workerCount > 8 {
		workerCount = 8
	}

	// Open all sources up front so the returned closer owns their lifetime,
	// independent of when scanning finishes.
	type openSource struct {
		profile string
		src     ffcfg.SourceConfig
		source  Source
	}
	var opened []openSource
	var openErrs []File
	for profName, prof := range cfg.Profiles {
		if profileName != "" && profName != profileName {
			continue
		}
		for _, src := range prof.Sources {
			source, err := OpenSource(src)
			if err != nil {
				openErrs = append(openErrs, File{OldPath: src.Path, Error: err})
				// Remember the failing source so its error event is emitted in order.
				opened = append(opened, openSource{profile: profName, src: src, source: nil})
				continue
			}
			opened = append(opened, openSource{profile: profName, src: src, source: source})
		}
	}

	closer := closerFunc(func() error {
		var firstErr error
		for _, o := range opened {
			if o.source == nil {
				continue
			}
			if err := o.source.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	})

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
					ch <- processFile(job.entry, job.src, job.profile, cfg)
				}
			}()
		}

		go func() {
			defer close(filePaths)

			for _, o := range opened {
				evCh <- ScanEvent{Profile: o.profile, SrcPath: o.src.Path, Recurse: o.src.Recurse, Types: o.src.Types, EventType: "start"}

				if o.source == nil {
					// OpenSource failed earlier; surface the recorded error.
					for _, f := range openErrs {
						if f.OldPath == o.src.Path {
							evCh <- ScanEvent{Profile: o.profile, SrcPath: o.src.Path, EventType: "error", Error: f.Error}
							ch <- f
							break
						}
					}
					continue
				}

				entries, err := o.source.Scan(o.src.Types, o.src.Recurse)
				if err != nil {
					evCh <- ScanEvent{Profile: o.profile, SrcPath: o.src.Path, EventType: "error", Error: err}
					ch <- File{OldPath: o.src.Path, Error: err}
					continue
				}

				evCh <- ScanEvent{Profile: o.profile, SrcPath: o.src.Path, Found: len(entries), EventType: "found"}
				for _, e := range entries {
					filePaths <- fileJob{entry: e, src: o.src, profile: o.profile}
				}
			}
		}()

		wg.Wait()
	}()

	return ch, evCh, closer
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }

type fileJob struct {
	entry   Entry
	src     ffcfg.SourceConfig
	profile string
}

func processFile(entry Entry, src ffcfg.SourceConfig, profileName string, cfg *ffcfg.Config) File {
	file := File{
		OldPath: entry.DisplayPath(),
		Entry:   entry,
	}

	var meta *FileMetadata
	for _, pat := range src.Filenames {
		meta = parseMetadataFromFilenamePattern(entry.Name(), pat)
		if meta != nil {
			break
		}
	}
	if meta == nil {
		if prof, ok := cfg.Profiles[profileName]; ok {
			for _, pat := range prof.Patterns {
				meta = parseMetadataFromFilenamePattern(entry.Name(), pat)
				if meta != nil {
					break
				}
			}
		}
	}

	var targetTmpl string
	if prof, ok := cfg.Profiles[profileName]; ok {
		targetTmpl = prof.Target.Path
	}
	if targetTmpl == "" {
		file.Error = &TargetTemplateError{Path: entry.DisplayPath()}
		return file
	}

	// Fast path: if the filename pattern alone already fills the target template,
	// don't read the file's content. This matters over MTP, where opening a file
	// streams it in full — reading EXIF from a multi-MB RAW just to learn a date
	// the filename already carries would be wasteful.
	if meta != nil {
		if targetPath, err := resolveTargetPath(targetTmpl, meta); err == nil && !hasUnpopulatedTokens(targetPath) {
			file.Metadata = meta
			setOp(&file, entry, targetPath)
			return file
		}
	}

	// Otherwise read metadata from the file content to fill the gaps. RAW images
	// (image.raw) are TIFF-based, so EXIF extraction applies to them too.
	var actualMeta *FileMetadata
	var err error
	if isFileType(entry.Name(), []string{"image", "image.raw"}) {
		actualMeta, err = extractImageMetadataFromEntry(entry)
	} else if isFileType(entry.Name(), []string{"video"}) {
		actualMeta, err = extractVideoMetadataFromEntry(entry)
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

	targetPath, err := resolveTargetPath(targetTmpl, meta)
	if err != nil {
		file.Error = err
		return file
	}

	// Check if the target path still contains unpopulated template tokens
	if hasUnpopulatedTokens(targetPath) {
		file.Error = &UnpopulatedTokensError{Path: entry.DisplayPath(), TargetPath: targetPath}
		return file
	}

	setOp(&file, entry, targetPath)
	return file
}

// setOp records the resolved destination and whether an actual move is needed.
// For local entries, a file already at its target is a no-op; MTP entries have
// no comparable filesystem path, so they always move.
func setOp(file *File, entry Entry, targetPath string) {
	file.NewPath = targetPath
	file.ShouldOp = true
	if lp, ok := entry.(localPathProvider); ok {
		absSrc, _ := filepath.Abs(lp.LocalPath())
		absDst, _ := filepath.Abs(targetPath)
		file.ShouldOp = absSrc != absDst
	}
}

type TargetTemplateError struct {
	Path string
}

func (e *TargetTemplateError) Error() string {
	return "could not determine target template for " + e.Path
}

type UnpopulatedTokensError struct {
	Path       string
	TargetPath string
}

func (e *UnpopulatedTokensError) Error() string {
	return "skipping file because target path contains unpopulated tokens: " + e.TargetPath
}
