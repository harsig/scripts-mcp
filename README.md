# scripts-mcp

A small MCP server that serves **shell scripts** as tools. Each script becomes one tool. Its name, description and parameters come from a metadata header in the script. Calling the tool runs the script and returns its output.

- stdio transport, built on the official Go MCP SDK
- scripts come from local paths or git repositories
- a single YAML file configures everything
- Linux and other Unix systems

## Build

Requires Go (see [src/go.mod](src/go.mod) for the version). Shared code comes from [harness-kit](https://github.com/harsig/harness-kit). Until it has tagged releases, it is tracked at its `main` branch; update it with `go get github.com/harsig/harness-kit@main` in `src/`.

```sh
./build.sh                # go vet + static binary at bin/scripts-mcp
cd src && go test ./...   # tests
```

## Writing a script

A script is an executable file (`chmod +x`) with a shebang. The metadata header is optional. It is a YAML block placed directly after the shebang, between two `# ---` lines:

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

- `name` is the tool name. It defaults to the file name without its extension and must match `^[a-zA-Z0-9_-]+$` and be unique.
- `description` is the tool description. It defaults to empty.
- `parameters` maps names (`^[a-z][a-z0-9_]*$`) to definitions:
  - `type`: `string`, `integer`, `number` or `boolean` (required)
  - `description`, `required` (default `false`), `default`
  - `enum`: allowed values, only for `string` parameters
- Unknown keys are an error.

Parameters arrive as environment variables named `PARAM_<NAME>`. They are never passed as command line arguments, so no quoting is needed. Booleans are `true` or `false`. A parameter that was not given and has no default is not set, so use `${PARAM_X-}` or `${PARAM_X:-fallback}` with `set -u`.

The server validates every call before starting the script. Unknown or missing arguments, wrong types and values outside the `enum` come back as a tool error.

## Configuration

```yaml
timeout_seconds: 60             # optional, per call, default 60
max_output_bytes: 32768         # optional, default 32768

sources:                        # each source has exactly one of path | git
  - path: ~/my-scripts          # local directory
    files:                      # each an executable script, relative to the source root or absolute
      - backup.sh
      - tools/disk-usage.sh

  - path: ./single-script.sh    # no files: the path itself is the script

  - git:                        # cloned into ./.scripts-mcp/repos/ by --setup
      url: git@github.com:acme/scripts.git
      ref: v1.2.0               # optional tag, branch or commit; default is the remote's default branch
    files: [ops/restart.sh, ops/logs.sh]   # required for git sources
```

Relative `path` values are resolved from the directory that contains the config file, relative `files` entries from the source root. Absolute paths are used as they are, and `~` is expanded. The server checks the configuration and all scripts at startup. If anything is wrong, it exits with code 1 and a message.

## Usage

```text
scripts-mcp <config_file>          run the MCP server (stdio)
scripts-mcp --setup <config_file>  clone or update git sources and exit
scripts-mcp --version              print the version
```

Options go before the config file. A usage error exits with code 2, any other error with code 1.

### 1. Fetch git sources (only needed if you use them)

```sh
scripts-mcp --setup ~/.config/scripts-mcp/config.yaml
```

Missing repositories are cloned into `.scripts-mcp/repos/` next to the config file, existing ones are fetched, and the configured `ref` is checked out. The clone folder depends on URL and `ref`, so after changing a `ref` the server refuses to start until you run `--setup` again. Authentication is whatever your local `git` provides (SSH for private repositories). A normal run never uses the network.

### 2. Register the server with your MCP client

**Claude Code:**

```sh
claude mcp add scripts -- /path/to/scripts-mcp/bin/scripts-mcp ~/.config/scripts-mcp/config.yaml
```

**Clients configured with JSON** (Claude Desktop, Cursor, and others):

```json
{
  "mcpServers": {
    "scripts": {
      "command": "/path/to/scripts-mcp/bin/scripts-mcp",
      "args": ["/home/me/.config/scripts-mcp/config.yaml"],
      "env": { "API_TOKEN": "..." }
    }
  }
}
```

Use absolute paths, because clients may start the server from any working directory. Scripts inherit the server's environment, so put secrets in the client's `env` block.

## Behavior notes

- **Execution:** the script is run directly (no shell wrapper) with its own directory as the working directory. stdin is `/dev/null`.
- **Result:** exit code 0 returns stdout as text (`(no output)` if it is empty), and stderr goes to the server's stderr. Any other exit code, a timeout or a start failure returns a tool error with the reason, stderr and stdout. Output longer than `max_output_bytes` is cut and marked as truncated.
- **Timeout:** when `timeout_seconds` expires, the script's whole process group is killed. Calls can run concurrently.
- **Reloading:** the tool list is built at startup, so restart the server after changing a header or the config. Script bodies are read on every call, so changes to them apply immediately.
- **Shutdown:** the server stops on stdin close, SIGINT or SIGTERM and kills running scripts. stdout carries only the MCP protocol. Logs go to stderr.

## Out of scope

Config hot reload, `${VAR}` expansion, glob patterns in `files`, sidecar metadata files, array or object parameters, streaming output, MCP resources and prompts, authentication settings and HTTP transport.
