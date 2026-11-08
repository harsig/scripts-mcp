package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// git runs a git command in dir and fails the test on error.
func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestUpdateRepos(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	upstream := t.TempDir()
	git(t, upstream, "init", "-q", "-b", "main")
	writeFile(t, filepath.Join(upstream, "ops", "run.sh"), "#!/bin/sh\necho hi\n", 0o755)
	git(t, upstream, "add", ".")
	git(t, upstream, "commit", "-q", "-m", "one")

	path := filepath.Join(t.TempDir(), "scripts-mcp.yml")
	writeFile(t, path, "sources:\n  - git:\n      url: "+upstream+"\n      ref: main\n    files: [ops/run.sh]\n", 0o644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.RequireRepos(path); err == nil || !strings.Contains(err.Error(), "scripts-mcp --setup "+path) {
		t.Fatalf("err = %v, want hint to run --setup", err)
	}
	for range 2 { // the second run fetches into the existing clone
		if err := cfg.UpdateRepos(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := cfg.RequireRepos(path); err != nil {
		t.Fatal(err)
	}
	scripts, err := LoadScripts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(scripts) != 1 || scripts[0].Name != "run" {
		t.Errorf("scripts = %+v, want one named run", scripts)
	}
}
