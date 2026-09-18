# SpyJo CLI DOX

## Purpose

- `main.go` is the `spyjo` launcher: a thin alias over the shared `internal/cli`
  entry point (same behavior, binary name, and error prefixes as `cmd/spynel`).
- It holds no logic of its own; all command parsing and lifecycle live in
  `internal/cli`.

## Local Contracts

- Keep this launcher a thin alias: never add argument parsing, feature
  detection, or output formatting here.

## Child DOX Index

No child DOX files.
