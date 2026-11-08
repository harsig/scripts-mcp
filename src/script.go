package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/harsig/harness-kit/text"
	"github.com/harsig/harness-kit/yamlconfig"
)

const headerDelimiter = "# ---"

var (
	validScriptName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
	validParamName  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// Script is an executable file served as a tool.
type Script struct {
	Name        string
	Description string
	File        string // absolute path
	Params      map[string]Param
}

// Param is a script parameter, declared in the metadata header.
type Param struct {
	Type        string   `yaml:"type"`
	Description string   `yaml:"description"`
	Required    bool     `yaml:"required"`
	Default     any      `yaml:"default"`
	Enum        []string `yaml:"enum"`
}

type header struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Parameters  map[string]Param `yaml:"parameters"`
}

// LoadScripts collects and validates all scripts of the configured sources.
func LoadScripts(cfg *Config) ([]Script, error) {
	var scripts []Script
	seen := map[string]string{}
	for _, src := range cfg.Sources {
		files := src.Files
		if len(files) == 0 {
			files = []string{src.Root}
		}
		for _, file := range files {
			s, err := loadScript(file)
			if err != nil {
				return nil, err
			}
			if other, ok := seen[s.Name]; ok {
				return nil, fmt.Errorf("duplicate script name %q: %s and %s", s.Name, other, s.File)
			}
			seen[s.Name] = s.File
			scripts = append(scripts, s)
		}
	}
	return scripts, nil
}

func loadScript(file string) (Script, error) {
	info, err := os.Stat(file)
	if err != nil {
		return Script{}, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return Script{}, fmt.Errorf("%s: not an executable file", file)
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return Script{}, err
	}
	h, err := parseHeader(string(data))
	if err != nil {
		return Script{}, fmt.Errorf("%s: %w", file, err)
	}

	s := Script{Name: h.Name, Description: h.Description, File: file, Params: h.Parameters}
	if s.Name == "" {
		base := filepath.Base(file)
		s.Name = strings.TrimSuffix(base, filepath.Ext(base))
	}
	if !validScriptName.MatchString(s.Name) {
		return Script{}, fmt.Errorf("%s: invalid script name %q (allowed: %s)", file, s.Name, validScriptName)
	}
	for _, name := range sortedKeys(s.Params) {
		if err := s.Params[name].validate(name); err != nil {
			return Script{}, fmt.Errorf("%s: parameter %q: %w", file, name, err)
		}
	}
	return s, nil
}

// parseHeader reads the metadata header: a block of comment lines between two
// "# ---" lines at the top of the script, directly after an optional shebang.
// Content without a header yields an empty header.
func parseHeader(content string) (header, error) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if strings.HasPrefix(lines[0], "#!") {
		lines = lines[1:]
	}
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != headerDelimiter {
		return header{}, nil
	}

	var yamlLines []string
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == headerDelimiter {
			return decodeHeader(strings.Join(yamlLines, "\n"))
		}
		text, ok := strings.CutPrefix(line, "#")
		if !ok {
			break // end of the comment block
		}
		yamlLines = append(yamlLines, strings.TrimPrefix(text, " "))
	}
	return header{}, fmt.Errorf("header is not closed by a %q line", headerDelimiter)
}

func decodeHeader(text string) (header, error) {
	var h header
	if err := yamlconfig.Decode([]byte(text), &h); err != nil {
		return header{}, fmt.Errorf("invalid header: %w", err)
	}
	return h, nil
}

func (p Param) validate(name string) error {
	if !validParamName.MatchString(name) {
		return fmt.Errorf("invalid name (allowed: %s)", validParamName)
	}
	switch p.Type {
	case "string", "integer", "number", "boolean":
	case "":
		return errors.New("type is required")
	default:
		return fmt.Errorf("unknown type %q (allowed: string, integer, number, boolean)", p.Type)
	}
	if len(p.Enum) > 0 && p.Type != "string" {
		return errors.New("enum is only allowed for string parameters")
	}
	if p.Default != nil {
		if _, err := p.format(p.Default); err != nil {
			return fmt.Errorf("default %w", err)
		}
	}
	return nil
}

// format checks a value against the parameter definition and returns it as
// an environment variable value. The value is either a YAML-decoded default or
// a JSON argument decoded with json.Decoder.UseNumber.
func (p Param) format(v any) (string, error) {
	var s string
	ok := false
	switch p.Type {
	case "string":
		s, ok = v.(string)
	case "boolean":
		var b bool
		b, ok = v.(bool)
		s = strconv.FormatBool(b)
	case "integer":
		switch n := v.(type) {
		case int:
			s, ok = strconv.Itoa(n), true
		case json.Number:
			i, err := n.Int64()
			s, ok = strconv.FormatInt(i, 10), err == nil
		}
	case "number":
		var f float64
		switch n := v.(type) {
		case int:
			f, ok = float64(n), true
		case float64:
			f, ok = n, true
		case json.Number:
			var err error
			f, err = n.Float64()
			ok = err == nil
		}
		s = strconv.FormatFloat(f, 'f', -1, 64)
	}
	if !ok {
		return "", fmt.Errorf("must be of type %s", p.Type)
	}
	if len(p.Enum) > 0 && !slices.Contains(p.Enum, s) {
		return "", fmt.Errorf("must be one of: %s", strings.Join(p.Enum, ", "))
	}
	return s, nil
}

// InputSchema returns the JSON schema of the script's parameters.
func (s Script) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, name := range sortedKeys(s.Params) {
		p := s.Params[name]
		prop := map[string]any{"type": p.Type}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		if len(p.Enum) > 0 {
			prop["enum"] = p.Enum
		}
		if p.Default != nil {
			prop["default"] = p.Default
		}
		props[name] = prop
		if p.Required {
			required = append(required, name)
		}
	}
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// Env validates the call arguments and returns them, with defaults applied,
// as PARAM_<NAME>=value environment entries.
func (s Script) Env(args json.RawMessage) ([]string, error) {
	var values map[string]any
	if len(args) > 0 {
		dec := json.NewDecoder(bytes.NewReader(args))
		dec.UseNumber()
		if err := dec.Decode(&values); err != nil {
			return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
		}
	}

	var errs []error
	for _, name := range sortedKeys(values) {
		if _, ok := s.Params[name]; !ok {
			errs = append(errs, fmt.Errorf("unknown argument %q", name))
		}
	}
	var env []string
	for _, name := range sortedKeys(s.Params) {
		p := s.Params[name]
		v, given := values[name]
		switch {
		case given:
		case p.Required:
			errs = append(errs, fmt.Errorf("missing required argument %q", name))
			continue
		case p.Default != nil:
			v = p.Default
		default:
			continue // not set
		}
		value, err := p.format(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("argument %q %w", name, err))
			continue
		}
		env = append(env, "PARAM_"+strings.ToUpper(name)+"="+value)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return env, nil
}

// Run executes the script in its directory with params added to the inherited
// environment. It kills the script's process group when ctx is done or the
// timeout expires. stdout and stderr are each cut after maxOutput bytes.
func (s Script) Run(ctx context.Context, params []string, timeout time.Duration, maxOutput int) (stdout, stderr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	outBuf, errBuf := &cappedBuffer{max: maxOutput}, &cappedBuffer{max: maxOutput}
	cmd := exec.CommandContext(ctx, s.File)
	cmd.Dir = filepath.Dir(s.File)
	cmd.Env = append(inheritedEnv(), params...)
	cmd.Stdout, cmd.Stderr = outBuf, errBuf // stdin is /dev/null
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second // don't hang on processes that escaped the group but hold the pipes

	err = cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		err = fmt.Errorf("timed out after %s", timeout)
	case ctx.Err() != nil:
		err = errors.New("canceled")
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		err = fmt.Errorf("exit code %d", exitErr.ExitCode())
	default:
		err = fmt.Errorf("failed: %w", err)
	}
	return outBuf.String(), errBuf.String(), err
}

// inheritedEnv returns the server's environment without PARAM_ variables, so
// a parameter that is not set cannot leak in from the server.
func inheritedEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(e string) bool { return strings.HasPrefix(e, "PARAM_") })
}

// cappedBuffer keeps the first max+1 bytes written to it, one more than it
// returns, so String can tell that output was cut.
type cappedBuffer struct {
	buf bytes.Buffer
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if room := b.max + 1 - b.buf.Len(); room > 0 {
		b.buf.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

func (b *cappedBuffer) String() string { return text.Truncate(b.buf.String(), b.max) }

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
