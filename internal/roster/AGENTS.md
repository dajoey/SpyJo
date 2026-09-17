# Roster DOX

## Purpose

- Own the optional workspace staffing file `.spynel/roster.yaml` and its private per-day usage count.

## Local Contracts

- A missing roster means legacy routing; an invalid roster returns an error the caller logs before using legacy routing. Roster problems never fail a dispatch.
- Staff names and runner kinds are validated identifiers; launch arguments are single-line strings passed to the process without shell expansion.
- A daily cap requires a fallback. A session key keeps the staff it was first given that day, so retries and recoveries count once. The last fallback link is used even when capped, so work always has a runner.
- Workflow document phases are routed by the orchestrator; `chat`, `notification`, and `heartbeat` roles are routed by the Herdr harness from the session key, only when the turn carries the configured default model (an explicit per-conversation model wins).
- The usage count under `runtime/` is bookkeeping only and never workflow authority.

## Child DOX Index

No child DOX files.
