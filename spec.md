# scripts-mcp

**scripts-mcp** is a small Go MCP server (stdio transport, official Go SDK) that serves shell scripts as tools. A script is an executable file with an optional metadata header; calling its tool runs the script and returns its output. The available scripts are defined by one YAML file passed as the only argument.

## Features

- **One tool per script.** The tool name is the script name, the tool description is the script description, and the tool parameters are the script parameters, all from the metadata header of the script (see below). The name falls back to the file name without extension, the description to an empty string, the parameters to none.
- **Metadata header:** a YAML block embedded in the first comment block of the script, directly after an optional shebang. The block starts and ends with a `# ---` line; inside, one leading `#` and one optional space are stripped from every line before the YAML is parsed. A script without a header is valid. Unknown keys fail. The script body is never parsed.
- **Parameters:** each parameter has a `type` (`string`, `integer`, `number`, `boolean`), an optional `description`, an optional `required` (default false), an optional `default` and an optional `enum` (strings only). The server builds the tool's JSON `InputSchema` from them (`additionalProperties: false`) and validates every call itself: unknown arguments, missing required arguments, wrong types and values outside the `enum` return an MCP tool error and the script is not started. Parameter names match `^[a-z][a-z0-9_]*$`.
- **Execution:** the script is started directly (no shell wrapper, so it needs the executable bit and a shebang), with the script's directory as working directory. Each parameter is passed as the environment variable `PARAM_<NAME>` (name uppercased), never as a command line argument, so values need no quoting and cannot inject options. Booleans become `true` or `false`; a parameter that is neither given nor has a default is not set. Everything else in the environment is inherited from the server process, so secrets are set by the MCP client in its server configuration. stdin is `/dev/null`, because the server's own stdin carries the MCP protocol.
- **Result:** exit code 0 returns stdout as a single text block, or `(no output)` if stdout is empty. Any other exit code, a timeout or a start failure returns an MCP tool error with the exit code and stderr (and stdout if any). On success, stderr goes to the server's stderr. Output beyond `max_output_bytes` is cut and marked as truncated.
- **Timeout:** the call is limited by `timeout_seconds`; on expiry the whole process group is killed. Each call runs its own process, concurrent calls are allowed.
- **Sources:** a list of local paths or git repositories. A source lists `files`, each a script, either relative to the source root or absolute. Without `files`, the `path` itself must be a script file (not allowed for git sources, which need `files`).
- **Git sources:** `scripts-mcp --setup` clones each repository into `.scripts-mcp/repos/<name>-<hash>/` next to the configuration file (the hash covers URL and `ref`), or fetches it if it is already there, and checks out the `ref` (tag, branch or commit; default: the remote's default branch). A normal run uses these clones and never touches the network; it fails with a hint to run `--setup` if a clone is missing, which includes after a `ref` change. Requires the `git` binary; authentication is whatever your git setup provides (SSH for private repositories).
- **Dynamic content:** the tool list, including descriptions and parameters, is built once at startup. The script is executed from disk on every call, so edits to the script body show up without a restart; changed metadata needs a restart.
- **Config checks at startup:** unknown keys fail; a source needs exactly one of `path` or `git`; a missing file, a file that is not executable, an invalid script name (`^[a-zA-Z0-9_-]+$`), a duplicate script name, an invalid header or an invalid parameter definition (name, type, `default` or `enum` not matching the type) fails. Relative `path` values are resolved from the config file's directory, relative `files` entries from the source root; `~` is expanded.
- **Errors:** usage errors print the usage and exit with code 2; configuration, setup and startup problems exit with code 1 and a clear message. A failing tool call returns an MCP tool error and never crashes the server. Logs and errors go to stderr, prefixed with `scripts-mcp: `.
- **CLI:** `scripts-mcp <config_file>` runs the server; `scripts-mcp --setup <config_file>` clones or updates git sources and exits; `scripts-mcp --version` prints the version, which `build.sh` takes from `git describe`. Options come before the config file; any other usage is a usage error. In server mode stdout carries only the MCP protocol. The server stops on stdin close, SIGINT or SIGTERM and kills running scripts.
- **Platform:** Linux and other Unix systems.
- **Code layout:** a single `main` package: `main.go` (wiring), `config.go`, `git.go`, `script.go` (header parsing, schema building, argument validation, execution), `server.go`, with table-driven tests per file. Git sources, strict YAML loading, path resolution and output truncation come from the shared module `github.com/harsig/harness-kit`. Tools are registered with the SDK's low-level `Server.AddTool`. No interfaces or abstractions beyond what the tests need.
- **Out of scope:** config hot reload, `${VAR}` expansion, glob patterns in `files`, sidecar metadata files, running scripts to discover their metadata, interpreters other than the script's shebang, array or object parameters, streaming output, MCP resources or prompts, authentication settings, HTTP transport.

## Configuration Schema

```yaml
timeout_seconds: 60             # optional, per call, default 60
max_output_bytes: 32768         # optional, default 32768

sources:                        # list of sources, each has exactly one of path | git
  - path: ~/my-scripts          # local directory
    files:                      # optional, relative to the source root or absolute, each an executable script
      - backup.sh
      - tools/disk-usage.sh

  - path: ./single-script.sh    # no files: the path itself is the script

  - git:                        # cloned into ./.scripts-mcp/repos/ by --setup
      url: git@github.com:acme/scripts.git
      ref: v1.2.0               # optional: tag | branch | commit (default: remote default branch)
    files: [ops/restart.sh, ops/logs.sh]   # required for git sources
```

## Script Header Example

```bash
#!/usr/bin/env bash
# ---
# name: git_log
# description: Show the most recent commits of a local repository.
# parameters:
#   repo:
#     type: string
#     description: Path to the repository
#     required: true
#   count:
#     type: integer
#     description: Number of commits to show
#     default: 10
#   format:
#     type: string
#     enum: [oneline, short, full]
#     default: oneline
# ---
set -euo pipefail
git -C "$PARAM_REPO" log -n "$PARAM_COUNT" --format="$PARAM_FORMAT"
```
