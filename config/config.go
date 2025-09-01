package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type SourceConfig struct {
	Path      string   `yaml:"path"`
	Recurse   bool     `yaml:"recurse"`
	Types     []string `yaml:"types"`
	Filenames []string `yaml:"filenames,omitempty"`
}

type TargetPathConfig struct {
	Path string `yaml:"path"`
}

type ProfileConfig struct {
	Sources  []SourceConfig   `yaml:"sources"`
	Patterns []string         `yaml:"patterns,omitempty"`
	Target   TargetPathConfig `yaml:"target"`
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
	seenSources := make(map[string]string)
	for profName, prof := range cfg.Profiles {
		if prof.Target.Path == "" {
			return nil, fmt.Errorf("profile %q: missing target.path", profName)
		}
		for _, src := range prof.Sources {
			if src.Path == "" {
				return nil, fmt.Errorf("profile %q: source path is empty", profName)
			}
			if prev, ok := seenSources[src.Path]; ok {
				return nil, fmt.Errorf("source path %q defined in profile %q and %q", src.Path, prev, profName)
			}
			seenSources[src.Path] = profName
		}
	}

	return &cfg, nil
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
