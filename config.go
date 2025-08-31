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

type TargetConfig struct {
	Image TargetPathConfig `yaml:"image"`
	Video TargetPathConfig `yaml:"video"`
}

type TargetPathConfig struct {
	Path string `yaml:"path"`
}

type Config struct {
	Sources []SourceConfig `yaml:"sources"`
	Target  TargetConfig   `yaml:"target"`
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
