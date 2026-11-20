# AGENTS.md

## UI & Host Alignment

- Follow `egoist/mygo` (`v0.10.0`) and `egoist/mygo-music` native visual and motion conventions (`AGPL-3.0`, keep upstream copyright and attribution notices intact).
- Keep scope strictly to the standalone Mini Player companion plugin for MyGo Music (`windows/amd64`, `windows/arm64`, `linux/amd64`, `linux/arm64`; macOS explicitly unsupported with clear user-facing explanation). Do not add unrequested features, controls, or settings.
- Keep `.scratch/` out of version control so the open-source repository stays clean.

## Checks

```sh
gofmt -l . ; go vet ./... ; go test ./... -count=1
GOOS=windows GOARCH=amd64 go build ./... ; GOOS=windows GOARCH=arm64 go build ./...
GOOS=linux GOARCH=amd64 go build ./... ; GOOS=linux GOARCH=arm64 go build ./...
```

## Agent skills

### Issue tracker

Issues and specs are tracked as local markdown files under `.scratch/`. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-label triage vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout (`GLOSSARY.md` + `docs/adr/`). See `docs/agents/domain.md`.
