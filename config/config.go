package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dkarlovi/fileferry/mtp"
	"gopkg.in/yaml.v3"
)

// Operation is what a source does with the files it claims.
type Operation string

const (
	// OperationMove transfers the file to the target and removes the source.
	// This is the default: a source is normally an inbox being drained.
	OperationMove Operation = "move"
	// OperationCopy transfers the file to the target and leaves the source in
	// place, for sources that are a library rather than an inbox.
	OperationCopy Operation = "copy"
)

type SourceConfig struct {
	Path      string    `yaml:"path"`
	Recurse   bool      `yaml:"recurse"`
	Types     []string  `yaml:"types"`
	Filenames []string  `yaml:"filenames,omitempty"`
	Operation Operation `yaml:"operation,omitempty"`
}

// Op returns the source's operation, defaulting to move when unset.
func (s SourceConfig) Op() Operation {
	if s.Operation == "" {
		return OperationMove
	}
	return s.Operation
}

// OnConflict is what a profile does when a file's target path is already taken
// by a file with *different* content (identical content is always deduplicated,
// never a conflict).
type OnConflict string

const (
	// OnConflictError leaves both files untouched and reports the collision.
	// This is the default: two different photos wanting the same name is
	// normally a question only a human can answer.
	OnConflictError OnConflict = "error"
	// OnConflictKeepHighestQuality gives the target path to the better of the two
	// renditions and parks the other alongside it under an "-alt" name, so
	// neither is lost. "Better" cascades through pixel count (an original beats a
	// crop or a downscale), then JPEG quantization (the finer encode beats a
	// recompression of the same geometry), then encoded size. Only renditions
	// that match on every one of those fall back to OnConflictError.
	OnConflictKeepHighestQuality OnConflict = "keep-highest-quality"
)

type TargetPathConfig struct {
	Path string `yaml:"path"`
}

type ProfileConfig struct {
	Sources    []SourceConfig   `yaml:"sources"`
	Patterns   []string         `yaml:"patterns,omitempty"`
	Target     TargetPathConfig `yaml:"target"`
	OnConflict OnConflict       `yaml:"on_conflict,omitempty"`
}

// Conflict returns the profile's conflict policy, defaulting to error when
// unset.
func (p ProfileConfig) Conflict() OnConflict {
	if p.OnConflict == "" {
		return OnConflictError
	}
	return p.OnConflict
}

// ParseOnConflict validates a conflict policy name, as given on the command
// line or in the config file.
func ParseOnConflict(name string) (OnConflict, error) {
	switch OnConflict(name) {
	case OnConflictError, OnConflictKeepHighestQuality:
		return OnConflict(name), nil
	}
	return "", fmt.Errorf("unknown conflict policy %q (want %q or %q)", name, OnConflictError, OnConflictKeepHighestQuality)
}

type Config struct {
	Profiles map[string]ProfileConfig `yaml:"profiles"`
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
	// Guard against the same file being processed twice: a (path, type) pair must
	// not appear in more than one profile. The same path with disjoint types
	// (e.g. RAW images in one profile, videos in another) is allowed.
	type seenSource struct {
		path    string
		ty      string
		profile string
	}
	var seenSources []seenSource
	for profName, prof := range cfg.Profiles {
		if prof.Target.Path == "" {
			return nil, fmt.Errorf("profile %q: missing target.path", profName)
		}
		if prof.OnConflict != "" {
			if _, err := ParseOnConflict(string(prof.OnConflict)); err != nil {
				return nil, fmt.Errorf("profile %q: %w", profName, err)
			}
		}
		for _, src := range prof.Sources {
			if src.Path == "" {
				return nil, fmt.Errorf("profile %q: source path is empty", profName)
			}
			switch src.Operation {
			case "", OperationMove, OperationCopy:
			default:
				return nil, fmt.Errorf("profile %q: source %q: unknown operation %q (want %q or %q)", profName, src.Path, src.Operation, OperationMove, OperationCopy)
			}
			// Validate MTP device URLs up front for a clear error before scanning.
			if mtp.IsURL(src.Path) {
				if _, _, err := mtp.ParseURL(src.Path); err != nil {
					return nil, fmt.Errorf("profile %q: %w", profName, err)
				}
			}
			// A source with no types claims the whole path; represent that with a
			// single empty type so it still collides with any other use of the path.
			types := src.Types
			if len(types) == 0 {
				types = []string{""}
			}
			for _, ty := range types {
				for _, prev := range seenSources {
					if prev.path == src.Path && typesOverlap(prev.ty, ty) {
						return nil, fmt.Errorf("source path %q with type %q defined in profile %q and %q", src.Path, ty, prev.profile, profName)
					}
				}
				seenSources = append(seenSources, seenSource{path: src.Path, ty: ty, profile: profName})
			}
		}
	}

	return &cfg, nil
}

// typesOverlap reports whether two source types can claim the same file. Type
// names are hierarchical and dot-separated, so a broader type covers its
// subtypes ("image" also claims "image.raw" files); the empty type means "no
// filter", which claims everything.
func typesOverlap(a, b string) bool {
	if a == "" || b == "" {
		return true
	}
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

// LoadConfigPrefer tries to load a config file using the following order:
//  1. the provided path if non-empty,
//  2. ./config.yaml (current working directory),
//  3. XDG user config dir: $XDG_CONFIG_HOME/fileferry/config.yaml or
//     on Windows the appropriate AppData path (via os.UserConfigDir()).
//
// The first existing file is loaded; if none found an error is returned.
func LoadConfigPrefer(preferred string) (*Config, error) {
	tried := []string{}

	// helper to test existence
	exists := func(p string) bool {
		if p == "" {
			return false
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true
		}
		return false
	}

	if preferred != "" {
		tried = append(tried, preferred)
		if exists(preferred) {
			return LoadConfig(preferred)
		}
	}

	// current directory
	cur := "config.yaml"
	tried = append(tried, cur)
	if exists(cur) {
		return LoadConfig(cur)
	}

	// XDG / user config dir
	if dir, err := os.UserConfigDir(); err == nil {
		p := filepath.Join(dir, "fileferry", "config.yaml")
		tried = append(tried, p)
		if exists(p) {
			return LoadConfig(p)
		}
	}

	return nil, fmt.Errorf("no config file found (tried: %v)", tried)
}
