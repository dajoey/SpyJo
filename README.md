# SpyJo

**SpyJo is Joey's personal fork of [Spynel](https://github.com/agent0ai/spynel), customized for his own preferences and homelab workflow. It is not a competing product — Spynel remains the canonical upstream.**

## What this is

- A fork of Spynel: the classic, non-AI orchestration program that coordinates external AI coding harnesses through one assistant-facing relationship.
- Customized for one user: Joey's harness mix, machines, and habits.
- Not intended as a competitor or a general-purpose distribution. Generally useful improvements belong upstream; personal workflow tweaks live here.

## What differs from upstream

- Command and TUI rebranded to `spyjo` (built from `./cmd/spyjo`; the `spynel` entry point is kept for compatibility).
- Herdr harness adapter: warm and dynamic agent panes, worker isolation per task session, fleet pane immunity.
- Per-task agent routing and a multi-agent review pipeline.
- `agy` worker execution support.
- Homelab-oriented cleanups: ephemeral worker tab auto-close, orphan sweeps, agent-name limits, obsolete-primary retirement.
- See [docs/spyjo-fork.md](docs/spyjo-fork.md) for the current delta list and setup notes.

## What stays canonical upstream

All product documentation — installation, configuration, harnesses, tasks and goals, architecture, security — lives upstream. This repo keeps only fork-specific notes and pointers; see the [documentation index](docs/README.md).

## Quick start (from source)

No published SpyJo releases exist; build from this checkout:

```sh
git clone https://github.com/dajoey/SpyJo.git
cd SpyJo
./scripts/dev.sh build
```

Run the resulting binary from a workspace directory (not from inside the repository):

```sh
mkdir -p /tmp/spyjo-playground && cd /tmp/spyjo-playground
"$OLDPWD/.tmp-bin/spyjo"
```

Or install the development binary to `~/.local/bin` (also keeps a `spynel` alias):

```sh
./scripts/install-dev.sh
spyjo version
```

Then `spyjo init --dir /path/to/workspace` (or launch `spyjo` and initialize interactively). Day-to-day usage matches upstream; `spyjo docs` prints the offline reference.

## Attribution

SpyJo builds on [Spynel](https://github.com/agent0ai/spynel) by the Agent Zero team. Upstream docs, protocol, and product direction remain theirs; fork-local changes are Joey's.
