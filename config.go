package main

import (
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
	return &cfg, nil
}
