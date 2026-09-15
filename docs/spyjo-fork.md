# SpyJo fork notes

SpyJo is Joey's personal fork of [Spynel](https://github.com/agent0ai/spynel) — a customized build for his preferences and homelab workflow, not a competing product. Upstream Spynel remains canonical for product behavior, protocol, and documentation. Generally useful improvements belong upstream; only personal workflow tweaks live here.

## Differences from upstream

Local changes on top of upstream (newest first; see `git log` for detail):

- Ephemeral Herdr worker tabs auto-close; orphaned tabs are swept on startup.
- Herdr worker agent names constrained to the 32-character limit.
- Obsolete deleted primaries auto-retire with immediate takeover.
- Herdr workers isolated into full-width tabs by task session and model.
- `agy` worker execution support, added to the fleet routing matrix.
- Per-task agent routing and a multi-agent review pipeline.
- Herdr worker panes isolated to the caller workspace with fleet pane immunity.
- `spyjo` rebrand: new `./cmd/spyjo` entry point, SpyJo TUI title, Herdr assistant rendering cleanup. The `spynel` entry point and install alias are retained for compatibility.

What has *not* changed: the Go module path (`github.com/agent0ai/spynel`), the `.spynel/` workspace layout and config schema, the task/goal lifecycle, and the offline `spyjo docs` topic catalog.

## Setup deltas

No published SpyJo releases exist. The upstream `install.sh` / npm paths install upstream Spynel, not this fork. Build and install from this checkout:

```sh
git clone https://github.com/dajoey/SpyJo.git
cd SpyJo
./scripts/dev.sh build        # produces .tmp-bin/spyjo (+ spynel symlink)
./scripts/install-dev.sh      # installs spyjo (and spynel alias) to ~/.local/bin
```

Run from a workspace directory, never from inside the repository checkout. `spyjo init --dir /path/to/workspace` initializes; bare `spyjo` launches the TUI.

## Usage deltas

- The command is `spyjo`; `spynel` still works as an alias where installed.
- Day-to-day usage (TUI, `/task`, `/goal`, tasks lifecycle, channels) matches upstream. Where this file and upstream disagree about fork behavior, this file wins for SpyJo checkouts.

## Canonical upstream docs

Full product documentation lives upstream (pinned to `v0.12.12`, the tag this fork's docs were deduplicated against):

- [Getting started and development](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/getting-started.md)
- [Configuration](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/configuration.md)
- [Communication integrations](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/integrations.md)
- [Harness compatibility](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/harness-compatibility.md)
- [Tasks and goals](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/tasks-and-goals.md)
- [Plain CLI and automation](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/cli.md)
- [Agent-readable documentation](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/agent-docs.md)
- [Architecture](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/architecture.md) and [provider-canary threat model](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/provider-canary-threat-model.md)
- [Extensions](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/extensions.md), [releasing](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/releasing.md), [troubleshooting](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/troubleshooting.md), [product vision](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/vision.md)
- [Programmatic integration](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/programmatic-integration.md), [TUI text editing internals](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/tui-editing.md), [persistent instructions](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/persistent-instructions.md), [configuration live matrix](https://github.com/agent0ai/spynel/blob/v0.12.12/docs/configuration-live-matrix.md)

## Attribution

SpyJo builds on [Spynel](https://github.com/agent0ai/spynel) by the Agent Zero team. Upstream docs, protocol, and product direction remain theirs; fork-local changes are Joey's.
