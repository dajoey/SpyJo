# Print Settings Meta DOX

- `main.go` emits `config.Settings()` as JSON on stdout for the settings console
  when the installed binary does not yet serve `GET /v1/settings` (pre-restart).
- It is a read-only developer helper, not part of build or release flows; remove
  it once every installation runs a binary that serves the endpoint.
