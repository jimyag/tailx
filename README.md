# tailx

Simple `tail -f` helper that watches a directory (recursively) and streams new log lines with per-file colored prefixes.

## Features
- Recursive watch: auto-adds new directories/files; handles file recreate/rotation.
- Event-aware tailing: create → read from start; write/chmod → start from end (incremental).
- Colored, configurable prefixes; optionally plain output.
- Include/exclude filters via substring match.
- Configurable EOF polling interval.

## Usage
```
go run main.go [OPTIONS] [rootPath]
```
- `rootPath` (optional): directory to watch. Defaults to current working directory.
- `-n, --lines=[+]NUM`: replay last `NUM` lines (`-n 20`) or start from line `NUM` (`-n +20`) before following new data.

## Configuration (env vars)
- `TAILX_COLOR` (bool, default: true): enable colored prefix. Accepts `true/false/1/0`.
- `TAILX_PREFIX` (string): prefix style. `full` (default), `relative`, `base`.
- `TAILX_POLL_MS` (int): EOF polling interval in milliseconds. Default `200`.
- `TAILX_INCLUDE` (csv): substrings to include; if set, only matching paths are tailed.
- `TAILX_EXCLUDE` (csv): substrings to exclude; evaluated before include.

## Output format
`[prefix] line`
- Prefix is colored if `TAILX_COLOR` is true.
- Prefix content depends on `TAILX_PREFIX`:
  - `full`: absolute path
  - `relative`: relative to `rootPath`
  - `base`: file name only

## Notes
- Create events read from file start; write/chmod events start from end.
- Remove/rename clears tracking so recreated files can be tailed again.
- No tests provided (requested).

## Release
- CI release: push a tag like `v1.2.3`, GitHub Actions runs GoReleaser and uploads archives/checksums.
- Local dry run (no publish): `goreleaser release --clean --skip=publish`
- Install GoReleaser: `brew install goreleaser` or `go install github.com/goreleaser/goreleaser/v2@latest`

## Taskfile (local build)
- Install go-task: `brew install go-task/tap/go-task` or `go install github.com/go-task/task/v3/cmd/task@latest`
- Build current platform: `task build`
- Build all platforms: `task build-all`
- Package tar.gz + checksums: `task package`
- Local release dry run via GoReleaser: `task release-local`
