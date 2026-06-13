package file

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	ffcfg "github.com/dkarlovi/fileferry/config"
	"github.com/dkarlovi/fileferry/mtp"
)

// Entry is a single source file. It abstracts over a regular file on disk and a
// file on an MTP device (an Android phone), neither of which expose the same
// API: an MTP object has no real filesystem path and can only be streamed and
// deleted through the WPD COM API.
type Entry interface {
	// Name is the base filename, e.g. "PXL_20240101_120000.dng".
	Name() string
	// DisplayPath is a human-readable location used for logging (a real path
	// for local files, an mtp:// URL for device files).
	DisplayPath() string
	// Size is the content length in bytes.
	Size() int64
	// ModTime is the last-modified time, or the zero time if unknown.
	ModTime() time.Time
	// Open streams the file's content for reading.
	Open() (io.ReadCloser, error)
	// Delete removes the file from its source.
	Delete() error
}

// localPathProvider is implemented by entries backed by a real filesystem path.
// Metadata extraction uses it to decide whether the exiftool/ffprobe fallbacks
// (which require a path on disk) are available.
type localPathProvider interface {
	LocalPath() string
}

// Source is an open scan target. For MTP sources the underlying device session
// must stay alive for as long as the returned Entries are used (their Open and
// Delete calls reuse it), so callers must not Close the Source until all moves
// are done.
type Source interface {
	// Scan returns the entries under the source whose extension matches one of
	// the given type categories (see DefaultFileTypes). When recurse is true it
	// descends into subfolders.
	Scan(types []string, recurse bool) ([]Entry, error)
	// Close releases any resources held by the source.
	Close() error
}

// OpenSource opens the source described by src. A path with the "mtp://" scheme
// opens an MTP device session (Windows only); anything else is a local
// filesystem directory.
func OpenSource(src ffcfg.SourceConfig) (Source, error) {
	if mtp.IsURL(src.Path) {
		device, path, err := mtp.ParseURL(src.Path)
		if err != nil {
			return nil, err
		}
		sess, err := mtp.Open(device, path)
		if err != nil {
			return nil, err
		}
		return &mtpSource{sess: sess, base: strings.TrimRight(src.Path, "/")}, nil
	}
	return &localSource{root: src.Path}, nil
}

// localSource scans a directory on the local filesystem.
type localSource struct {
	root string
}

func (s *localSource) Scan(types []string, recurse bool) ([]Entry, error) {
	var entries []Entry
	walkFn := func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if !recurse && path != s.root {
				return filepath.SkipDir
			}
			return nil
		}
		if isFileType(path, types) {
			entries = append(entries, &localEntry{path: path, info: info})
		}
		return nil
	}
	if err := filepath.Walk(s.root, walkFn); err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *localSource) Close() error { return nil }

// localEntry is a file on the local filesystem.
type localEntry struct {
	path string
	info os.FileInfo
}

func (e *localEntry) Name() string                 { return filepath.Base(e.path) }
func (e *localEntry) DisplayPath() string          { return e.path }
func (e *localEntry) Size() int64                  { return e.info.Size() }
func (e *localEntry) ModTime() time.Time           { return e.info.ModTime() }
func (e *localEntry) Open() (io.ReadCloser, error) { return os.Open(e.path) }
func (e *localEntry) Delete() error                { return os.Remove(e.path) }
func (e *localEntry) LocalPath() string            { return e.path }

// mtpSource scans a folder on an MTP device.
type mtpSource struct {
	sess mtp.Session
	base string // source root, e.g. "mtp://Pixel 9 Pro/DCIM/Camera"
}

func (s *mtpSource) Scan(types []string, recurse bool) ([]Entry, error) {
	objs, err := s.sess.List(recurse)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, o := range objs {
		if isFileType(o.Name(), types) {
			entries = append(entries, &mtpEntry{obj: o, base: s.base})
		}
	}
	return entries, nil
}

func (s *mtpSource) Close() error { return s.sess.Close() }

// mtpEntry is a file on an MTP device.
type mtpEntry struct {
	obj  mtp.Object
	base string
}

func (e *mtpEntry) Name() string                 { return e.obj.Name() }
func (e *mtpEntry) DisplayPath() string          { return e.base + "/" + e.obj.RelPath() }
func (e *mtpEntry) Size() int64                  { return e.obj.Size() }
func (e *mtpEntry) ModTime() time.Time           { return e.obj.ModTime() }
func (e *mtpEntry) Open() (io.ReadCloser, error) { return e.obj.Open() }
func (e *mtpEntry) Delete() error                { return e.obj.Delete() }
