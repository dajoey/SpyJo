# Roster DOX

## Purpose

- Own the optional workspace staffing file `.spynel/roster.yaml` and its private per-day usage count.

## Local Contracts

- A missing roster means legacy routing; an invalid roster returns an error the caller logs before using legacy routing. Roster problems never fail a dispatch.
- Staff names and runner kinds are validated identifiers; launch arguments are single-line strings passed to the process without shell expansion.
- A daily cap requires a fallback. A session key keeps the staff it was first given that day, so retries and recoveries count once. The last fallback link is used even when capped, so work always has a runner.
- The usage count under `runtime/` is bookkeeping only and never workflow authority.

## Child DOX Index

No child DOX files.
