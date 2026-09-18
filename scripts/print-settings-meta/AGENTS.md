# Print Settings Meta DOX

## Purpose

- Emit the typed `config.Settings()` catalog as JSON on stdout for the settings
  console when the installed binary does not yet serve `GET /v1/settings`
  (pre-restart installs).

## Local Contracts

- Read-only developer helper: it never reads workspace or runtime state and is
  not part of build, release, or smoke flows.
- Remove it once every installation runs a binary that serves the endpoint.

## Child DOX Index

No child DOX files.
