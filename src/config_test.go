package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/harsig/harness-kit/gitsource"
)

// writeFile creates path (and parent directories) with content and mode.
func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile keeps the mode of existing files
		t.Fatal(err)
	}
}

func TestLoadConfig(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		yaml        string
		wantRoots   []string // relative roots are joined with the config dir
		wantTimeout int
		wantMaxOut  int
		wantErr     string
	}{
		{name: "empty uses defaults", yaml: "", wantTimeout: 60, wantMaxOut: 32768},
		{name: "limits", yaml: "timeout_seconds: 5\nmax_output_bytes: 100\n", wantTimeout: 5, wantMaxOut: 100},
		{name: "relative path", yaml: "sources:\n  - path: ./a.sh\n", wantRoots: []string{"a.sh"}},
		{name: "absolute path", yaml: "sources:\n  - path: /x/y\n    files: [a.sh]\n", wantRoots: []string{"/x/y"}},
		{name: "tilde", yaml: "sources:\n  - path: ~/s\n", wantRoots: []string{filepath.Join(home, "s")}},
		{
			name:      "git",
			yaml:      "sources:\n  - git:\n      url: git@github.com:acme/scripts.git\n      ref: v1\n    files: [a.sh]\n",
			wantRoots: []string{repoDir("git@github.com:acme/scripts.git", "v1")},
		},
		{name: "negative timeout", yaml: "timeout_seconds: -1\n", wantErr: "must be positive"},
		{name: "unknown top-level key", yaml: "foo: 1\n", wantErr: "field foo not found"},
		{name: "unknown source key", yaml: "sources:\n  - path: a\n    file: [x]\n", wantErr: "field file not found"},
		{name: "unknown git key", yaml: "sources:\n  - git:\n      url: u/x\n      branch: main\n    files: [a]\n", wantErr: "field branch not found"},
		{name: "path and git", yaml: "sources:\n  - path: a\n    git:\n      url: u/x\n", wantErr: "only one of path or git"},
		{name: "neither", yaml: "sources:\n  - files: [a]\n", wantErr: "one of path or git is required"},
		{name: "git without url", yaml: "sources:\n  - git:\n      ref: main\n    files: [a]\n", wantErr: "git.url is required"},
		{name: "git without files", yaml: "sources:\n  - git:\n      url: u/x\n", wantErr: "files is required"},
		{name: "invalid yaml", yaml: "sources: [", wantErr: "yaml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			writeFile(t, path, tt.yaml, 0o644)

			cfg, err := LoadConfig(path)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantTimeout != 0 && cfg.TimeoutSeconds != tt.wantTimeout {
				t.Errorf("timeout = %d, want %d", cfg.TimeoutSeconds, tt.wantTimeout)
			}
			if tt.wantMaxOut != 0 && cfg.MaxOutputBytes != tt.wantMaxOut {
				t.Errorf("max output = %d, want %d", cfg.MaxOutputBytes, tt.wantMaxOut)
			}
			if len(cfg.Sources) != len(tt.wantRoots) {
				t.Fatalf("got %d sources, want %d", len(cfg.Sources), len(tt.wantRoots))
			}
			for i, want := range tt.wantRoots {
				if !filepath.IsAbs(want) {
					want = filepath.Join(dir, want)
				}
				if got := cfg.Sources[i].Root; got != want {
					t.Errorf("source %d root = %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestLoadConfigFiles(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "sources:\n  - path: src\n    files: [a.sh, tools/b.sh, ../c.sh, /abs/d.sh, ~/e.sh]\n", 0o644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(dir, "src", "a.sh"),
		filepath.Join(dir, "src", "tools", "b.sh"),
		filepath.Join(dir, "c.sh"),
		"/abs/d.sh",
		filepath.Join(home, "e.sh"),
	}
	if got := cfg.Sources[0].Files; !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("expected error")
	}
}

// repoDir is the clone location of a git source, relative to the config directory.
func repoDir(url, ref string) string {
	r := gitsource.Repo{URL: url, Ref: ref}
	r.SetDir(filepath.Join(".scripts-mcp", "repos"))
	return r.Dir
}
