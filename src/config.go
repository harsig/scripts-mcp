package main

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/harsig/harness-kit/gitsource"
	"github.com/harsig/harness-kit/yamlconfig"
)

const (
	defaultTimeoutSeconds = 60
	defaultMaxOutputBytes = 32768
)

// Config is the parsed configuration file.
type Config struct {
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	MaxOutputBytes int      `yaml:"max_output_bytes"`
	Sources        []Source `yaml:"sources"`
}

// Source is a local path or a git repository containing scripts.
type Source struct {
	Path  string          `yaml:"path"`
	Git   *gitsource.Repo `yaml:"git"`
	Files []string        `yaml:"files"` // absolute after LoadConfig

	// Root is the resolved absolute source path, set by LoadConfig.
	Root string `yaml:"-"`
}

// LoadConfig reads, validates and resolves the configuration file at path.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	configDir, err := yamlconfig.Load(path, &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.TimeoutSeconds < 0 || cfg.MaxOutputBytes < 0 {
		return nil, fmt.Errorf("%s: timeout_seconds and max_output_bytes must be positive", path)
	}
	if cfg.TimeoutSeconds == 0 {
		cfg.TimeoutSeconds = defaultTimeoutSeconds
	}
	if cfg.MaxOutputBytes == 0 {
		cfg.MaxOutputBytes = defaultMaxOutputBytes
	}

	for i := range cfg.Sources {
		if err := resolveSource(&cfg.Sources[i], configDir); err != nil {
			return nil, fmt.Errorf("%s: sources[%d]: %w", path, i, err)
		}
	}
	return &cfg, nil
}

// resolveSource validates src, sets its Root and makes its Files absolute.
func resolveSource(src *Source, configDir string) error {
	if err := resolveRoot(src, configDir); err != nil {
		return err
	}
	for i, f := range src.Files {
		src.Files[i] = yamlconfig.ResolvePath(src.Root, f)
	}
	return nil
}

func resolveRoot(src *Source, configDir string) error {
	switch {
	case src.Path != "" && src.Git != nil:
		return errors.New("only one of path or git is allowed")
	case src.Path != "":
		src.Root = yamlconfig.ResolvePath(configDir, src.Path)
		return nil
	case src.Git != nil:
		if err := src.Git.Validate(); err != nil {
			return err
		}
		if len(src.Files) == 0 {
			return errors.New("files is required for git sources")
		}
		src.Git.SetDir(filepath.Join(configDir, ".scripts-mcp", "repos"))
		src.Root = src.Git.Dir
		return nil
	default:
		return errors.New("one of path or git is required")
	}
}
