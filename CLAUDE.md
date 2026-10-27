# CLAUDE.md

`unclog` is a free, open-source Go CLI that finds the RAM, disk and process waste left behind by AI coding tools (Claude Code, Cursor, Copilot, Windsurf, Ollama, LM Studio, Hugging Face, MCP servers) and cleans it safely on request. Mac, Windows, Linux. Full spec: `docs/spec.md`. Milestone specs: `docs/milestones/`. Work one milestone per session.

## Goals for v0.1

- One command shows how much RAM and disk AI tooling is using, grouped by tool
- Find orphaned processes (MCP servers, language servers, dev servers, node/python workers) still running after their parent tool closed
- Clean selected items with confirmation, moving files to trash where possible
- Single binary, installs in under a minute, uses under 20 MB RAM itself

## Non-goals for v0.1

- No background daemon, scheduling or auto-cleanup (reserved for the paid app)
- No generic "RAM booster" that frees OS file cache
- No GUI
- No telemetry by default

## Conventions

- Go 1.23+. Libraries: cobra (commands), gopsutil/v4 (processes, memory, disk), bubbletea + bubbles + lipgloss (UI), viper (YAML config), wastebasket (trash).
- `cmd/` is thin: flag parsing and output only, no logic.
- All scanning and cleaning logic lives in `internal/engine` so the paid app can import it. Detectors in `internal/detectors`, one file per tool, each implementing `Detector` (`Name()`, `Scan(ctx, env)`).
- OS-specific paths live in `internal/paths`; always honour env overrides (`HF_HOME`, `OLLAMA_MODELS`, `XDG_CACHE_HOME`, `npm config get cache`, `go env GOCACHE`) before defaults.
- Exit codes: 0 ok, 1 error, 2 nothing found, 3 user cancelled.
- Colors off when output is piped or `NO_COLOR` is set.
- `--json` on `scan`, `ps` and `doctor`.
- Before finishing any milestone: write tests, run `make test lint`.

## Safety rules (non-negotiable)

1. Never delete anything without an explicit user selection and confirmation, except with `--yes`.
2. `clean` moves files to the OS trash by default; permanent delete only with `--permanent`.
3. Only delete paths that a detector produced. Never accept arbitrary paths from the command line.
4. Hard denylist: `/`, home dir root, `~/Documents`, `~/Desktop`, `~/Downloads`, any `.git` folder, `.env` files, SSH/GPG keys, browser profiles, system folders. Refuse even if a detector returns them.
5. Resolve symlinks and refuse any path that ends up outside the detector's expected root.
6. Skip files that are open or locked by a running process, and skip caches of a tool that is currently running unless the user confirms.
7. Never touch config or auth files (`~/.claude.json`, `settings.json`, credentials, tokens).
8. `kill` never touches processes owned by other users, PID < 100, or anything not matched by a detector; send SIGTERM first.
9. `--dry-run` must produce exactly the same list as a real run, with nothing changed.
10. Write a log of every clean/kill action to `~/.config/unclog/history.log` (path, size, time).
11. No network calls except an opt-in update check. No telemetry by default.
12. Every detector ships with tests against a fake home dir in `testdata/`.
