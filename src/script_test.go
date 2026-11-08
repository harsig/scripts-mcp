package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    header
		wantErr string
	}{
		{name: "no header", content: "#!/bin/sh\necho hi\n", want: header{}},
		{name: "empty file", content: "", want: header{}},
		{name: "comment but no delimiter", content: "#!/bin/sh\n# name: x\n", want: header{}},
		{name: "delimiter not directly after shebang", content: "#!/bin/sh\n\n# ---\n# name: x\n# ---\n", want: header{}},
		{name: "empty header", content: "#!/bin/sh\n# ---\n# ---\n", want: header{}},
		{
			name:    "with shebang",
			content: "#!/bin/sh\n# ---\n# name: x\n# description: does x\n# ---\necho\n",
			want:    header{Name: "x", Description: "does x"},
		},
		{name: "without shebang", content: "# ---\n# name: x\n# ---\n", want: header{Name: "x"}},
		{name: "no space after hash", content: "# ---\n#name: x\n# ---\n", want: header{Name: "x"}},
		{name: "crlf", content: "#!/bin/sh\r\n# ---\r\n# name: x\r\n# ---\r\n", want: header{Name: "x"}},
		{
			name:    "parameters keep indentation",
			content: "# ---\n# parameters:\n#   repo:\n#     type: string\n#     required: true\n# ---\n",
			want:    header{Parameters: map[string]Param{"repo": {Type: "string", Required: true}}},
		},
		{name: "unterminated", content: "# ---\n# name: x\n", wantErr: "not closed"},
		{name: "non-comment line ends block", content: "# ---\n# name: x\necho\n# ---\n", wantErr: "not closed"},
		{name: "unknown key", content: "# ---\n# nmae: x\n# ---\n", wantErr: "field nmae not found"},
		{name: "unknown parameter key", content: "# ---\n# parameters:\n#   a:\n#     typ: string\n# ---\n", wantErr: "field typ not found"},
		{name: "invalid yaml", content: "# ---\n# name: [\n# ---\n", wantErr: "invalid header"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeader(tt.content)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// scriptWithHeader builds a script whose header has with the given YAML lines.
func scriptWithHeader(yamlLines ...string) string {
	s := "#!/bin/sh\n# ---\n"
	for _, l := range yamlLines {
		s += "# " + l + "\n"
	}
	return s + "# ---\necho ok\n"
}

func TestLoadScripts(t *testing.T) {
	tests := []struct {
		name      string
		files     map[string]string // path relative to the source root -> content
		mode      os.FileMode       // default 0o755
		srcFiles  []string          // nil: the source root is the single file "single.sh"
		wantNames []string
		wantErr   string
	}{
		{
			name:      "name from header and from file name",
			files:     map[string]string{"a.sh": scriptWithHeader("name: alpha"), "tools/b.sh": "#!/bin/sh\n"},
			srcFiles:  []string{"a.sh", "tools/b.sh"},
			wantNames: []string{"alpha", "b"},
		},
		{name: "single file source", files: map[string]string{"single.sh": "#!/bin/sh\n"}, wantNames: []string{"single"}},
		{name: "missing file", files: map[string]string{}, srcFiles: []string{"a.sh"}, wantErr: "no such file"},
		{name: "not executable", files: map[string]string{"a.sh": "#!/bin/sh\n"}, mode: 0o644, srcFiles: []string{"a.sh"}, wantErr: "not an executable file"},
		{name: "directory", files: map[string]string{"d/a.sh": "#!/bin/sh\n"}, srcFiles: []string{"d"}, wantErr: "not an executable file"},
		{name: "invalid name", files: map[string]string{"a.sh": scriptWithHeader("name: a b")}, srcFiles: []string{"a.sh"}, wantErr: "invalid script name"},
		{
			name:     "duplicate name",
			files:    map[string]string{"a.sh": "#!/bin/sh\n", "x/a.py": "#!/bin/sh\n"},
			srcFiles: []string{"a.sh", "x/a.py"},
			wantErr:  "duplicate script name",
		},
		{name: "invalid header", files: map[string]string{"a.sh": "# ---\n# name: x\n"}, srcFiles: []string{"a.sh"}, wantErr: "not closed"},
		{
			name:     "invalid parameter",
			files:    map[string]string{"a.sh": scriptWithHeader("parameters:", "  Repo:", "    type: string")},
			srcFiles: []string{"a.sh"},
			wantErr:  `parameter "Repo": invalid name`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			mode := tt.mode
			if mode == 0 {
				mode = 0o755
			}
			for name, content := range tt.files {
				writeFile(t, filepath.Join(root, name), content, mode)
			}
			src := Source{Root: filepath.Join(root, "single.sh")}
			for _, f := range tt.srcFiles {
				src.Files = append(src.Files, filepath.Join(root, f)) // absolute, as after LoadConfig
			}

			scripts, err := LoadScripts(&Config{Sources: []Source{src}})
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, s := range scripts {
				names = append(names, s.Name)
			}
			if !reflect.DeepEqual(names, tt.wantNames) {
				t.Errorf("names = %v, want %v", names, tt.wantNames)
			}
		})
	}
}

func TestParamValidate(t *testing.T) {
	tests := []struct {
		name    string
		param   string
		p       Param
		wantErr string
	}{
		{name: "string", param: "repo_1", p: Param{Type: "string", Default: "x", Enum: []string{"x", "y"}}},
		{name: "integer", param: "n", p: Param{Type: "integer", Default: 3}},
		{name: "number from int", param: "n", p: Param{Type: "number", Default: 3}},
		{name: "number from float", param: "n", p: Param{Type: "number", Default: 0.5}},
		{name: "boolean", param: "b", p: Param{Type: "boolean", Default: true}},
		{name: "uppercase name", param: "Repo", p: Param{Type: "string"}, wantErr: "invalid name"},
		{name: "leading digit", param: "1a", p: Param{Type: "string"}, wantErr: "invalid name"},
		{name: "dash", param: "a-b", p: Param{Type: "string"}, wantErr: "invalid name"},
		{name: "missing type", param: "a", p: Param{}, wantErr: "type is required"},
		{name: "unknown type", param: "a", p: Param{Type: "array"}, wantErr: "unknown type"},
		{name: "enum on integer", param: "a", p: Param{Type: "integer", Enum: []string{"1"}}, wantErr: "only allowed for string"},
		{name: "default wrong type", param: "a", p: Param{Type: "integer", Default: "x"}, wantErr: "default must be of type integer"},
		{name: "float default for integer", param: "a", p: Param{Type: "integer", Default: 1.5}, wantErr: "must be of type integer"},
		{name: "default not in enum", param: "a", p: Param{Type: "string", Default: "z", Enum: []string{"x"}}, wantErr: "must be one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.validate(tt.param)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

var testParams = map[string]Param{
	"repo":   {Type: "string", Description: "Path", Required: true},
	"count":  {Type: "integer", Default: 10},
	"ratio":  {Type: "number"},
	"force":  {Type: "boolean"},
	"format": {Type: "string", Enum: []string{"oneline", "short"}, Default: "oneline"},
}

func TestInputSchema(t *testing.T) {
	got, err := json.Marshal(Script{Params: testParams}.InputSchema())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"additionalProperties":false,"properties":{` +
		`"count":{"default":10,"type":"integer"},` +
		`"force":{"type":"boolean"},` +
		`"format":{"default":"oneline","enum":["oneline","short"],"type":"string"},` +
		`"ratio":{"type":"number"},` +
		`"repo":{"description":"Path","type":"string"}},` +
		`"required":["repo"],"type":"object"}`
	if string(got) != want {
		t.Errorf("schema =\n%s\nwant\n%s", got, want)
	}

	got, _ = json.Marshal(Script{}.InputSchema())
	if want := `{"additionalProperties":false,"properties":{},"type":"object"}`; string(got) != want {
		t.Errorf("empty schema = %s, want %s", got, want)
	}
}

func TestEnv(t *testing.T) {
	s := Script{Params: testParams}
	tests := []struct {
		name    string
		args    string
		want    []string
		wantErr []string
	}{
		{
			name: "defaults applied, unset stays unset",
			args: `{"repo":"/r"}`,
			want: []string{"PARAM_COUNT=10", "PARAM_FORMAT=oneline", "PARAM_REPO=/r"},
		},
		{
			name: "all given",
			args: `{"repo":"a b; rm -rf /","count":3,"ratio":1e3,"force":false,"format":"short"}`,
			want: []string{"PARAM_COUNT=3", "PARAM_FORCE=false", "PARAM_FORMAT=short", "PARAM_RATIO=1000", "PARAM_REPO=a b; rm -rf /"},
		},
		{name: "integer as whole float string", args: `{"repo":"r","count":2.0}`, wantErr: []string{`"count" must be of type integer`}},
		{name: "missing required", args: `{}`, wantErr: []string{`missing required argument "repo"`}},
		{name: "no arguments", args: ``, wantErr: []string{`missing required argument "repo"`}},
		{name: "null arguments", args: `null`, wantErr: []string{`missing required argument "repo"`}},
		{name: "unknown argument", args: `{"repo":"r","extra":1}`, wantErr: []string{`unknown argument "extra"`}},
		{name: "wrong type", args: `{"repo":1}`, wantErr: []string{`"repo" must be of type string`}},
		{name: "null value", args: `{"repo":null}`, wantErr: []string{`"repo" must be of type string`}},
		{name: "number as string", args: `{"repo":"r","ratio":"1"}`, wantErr: []string{`"ratio" must be of type number`}},
		{name: "boolean as string", args: `{"repo":"r","force":"true"}`, wantErr: []string{`"force" must be of type boolean`}},
		{name: "outside enum", args: `{"repo":"r","format":"full"}`, wantErr: []string{`"format" must be one of: oneline, short`}},
		{name: "not an object", args: `[1]`, wantErr: []string{"must be a JSON object"}},
		{
			name:    "all errors reported",
			args:    `{"extra":1,"count":"x"}`,
			wantErr: []string{`unknown argument "extra"`, `"count" must be of type integer`, `missing required argument "repo"`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Env(json.RawMessage(tt.args))
			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				for _, want := range tt.wantErr {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("err = %v, want containing %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("env = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRun(t *testing.T) {
	t.Setenv("PARAM_LEAK", "leaked")
	t.Setenv("INHERITED", "yes")
	tests := []struct {
		name       string
		body       string
		params     []string
		timeout    time.Duration
		maxOutput  int // default 100
		wantStdout string
		wantStderr string
		wantErr    string
	}{
		{name: "stdout and stderr", body: "echo out; echo err >&2", wantStdout: "out\n", wantStderr: "err\n"},
		{
			name:       "environment",
			body:       `echo "$PARAM_A|${PARAM_LEAK-unset}|$INHERITED"`,
			params:     []string{"PARAM_A=x y"},
			wantStdout: "x y|unset|yes\n",
		},
		{name: "working directory is script dir", body: "ls", wantStdout: "marker\nscript.sh\n"},
		{name: "stdin is empty", body: "cat", wantStdout: ""},
		{name: "exit code", body: "echo partial; echo bad >&2; exit 3", wantStdout: "partial\n", wantStderr: "bad\n", wantErr: "exit code 3"},
		{name: "truncated", body: "printf 0123456789", maxOutput: 8, wantStdout: "01234567\n[truncated after 8 bytes]"},
		{name: "timeout kills children", body: "sleep 10 & wait", timeout: 200 * time.Millisecond, wantErr: "timed out after 200ms"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			file := filepath.Join(dir, "script.sh")
			writeFile(t, file, "#!/bin/sh\n"+tt.body+"\n", 0o755)
			writeFile(t, filepath.Join(dir, "marker"), "", 0o644)
			timeout := tt.timeout
			if timeout == 0 {
				timeout = 5 * time.Second
			}

			maxOutput := tt.maxOutput
			if maxOutput == 0 {
				maxOutput = 100
			}

			start := time.Now()
			stdout, stderr, err := Script{File: file}.Run(context.Background(), tt.params, timeout, maxOutput)
			if time.Since(start) > 3*time.Second {
				t.Errorf("run took %s", time.Since(start))
			}
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tt.wantStdout != "" || tt.wantErr == "" {
				if stdout != tt.wantStdout {
					t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
				}
			}
			if stderr != tt.wantStderr {
				t.Errorf("stderr = %q, want %q", stderr, tt.wantStderr)
			}
		})
	}
}

func TestRunStartFailure(t *testing.T) {
	file := filepath.Join(t.TempDir(), "noshebang")
	writeFile(t, file, "\x00\x01binary", 0o755)
	_, _, err := Script{File: file}.Run(context.Background(), nil, time.Second, 100)
	if err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("err = %v, want start failure", err)
	}
}

func TestRunCanceled(t *testing.T) {
	file := filepath.Join(t.TempDir(), "s.sh")
	writeFile(t, file, "#!/bin/sh\nsleep 10\n", 0o755)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, _, err := Script{File: file}.Run(ctx, nil, time.Minute, 100)
	if err == nil || err.Error() != "canceled" {
		t.Fatalf("err = %v, want canceled", err)
	}
}
