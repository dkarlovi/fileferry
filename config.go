package main

import (
	"fmt"
	"os"

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
