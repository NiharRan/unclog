# unclog

Find and clean the RAM, disk and process waste that AI coding tools leave behind.

`unclog` is a free, open-source command-line tool for macOS, Linux and Windows. It looks at what Claude Code, Cursor, VS Code, Windsurf, Ollama, LM Studio, Hugging Face, Docker, MCP servers and your package managers are using, shows it grouped by tool, and cleans what you pick. It is one small binary, makes no network calls and sends no telemetry.

![unclog scan finds caches, models and orphaned processes, then clean --dry-run shows what it would move to the Trash](assets/demo.gif)

Full tutorial: [AI coding tools hid 15 GB on my Mac. unclog found it](https://www.nihardaily.com/posts/ai-coding-tools-hid-15-gb-on-my-mac-unclog-found-it).

## Install

macOS and Linux (Homebrew):

```
brew install --cask NiharRan/tap/unclog
```

Windows (Scoop):

```
scoop bucket add NiharRan https://github.com/NiharRan/scoop-bucket
scoop install unclog
```

With Go 1.26 or newer:

```
go install github.com/NiharRan/unclog@latest
```

Or download an archive for your platform from the [releases page](https://github.com/NiharRan/unclog/releases) and put `unclog` on your `PATH`.

## Commands

| Command | What it does |
| --- | --- |
| `unclog scan` | Disk used by AI caches, logs and models, and RAM used by AI processes, grouped by tool, with a total reclaimable figure. Flags: `--json`, `--only disk\|ram`, `--min-size 100MB`. |
| `unclog clean` | Interactive checklist of findings. Only `safe` items start checked. Files go to the Trash. Flags: `--dry-run`, `--yes`, `--category`, `--older-than 30d`, `--permanent`. |
| `unclog ps` | Table of AI-related processes with RAM, CPU, uptime, parent and an orphan flag. Flags: `--watch`, `--orphans`, `--json`. |
| `unclog kill` | Stops orphaned AI processes: SIGTERM, then SIGKILL after 5 seconds. Flags: `--orphans`, `--pid`, `--dry-run`, `--yes`. |
| `unclog doctor` | The top 5 problems with a one-line fix each. Flag: `--json`. |
| `unclog config` | Shows the effective settings. Subcommands: `init`, `edit`, `path`. |
| `unclog version` | Version, commit and platform. |

Exit codes: `0` ok, `1` error, `2` nothing found, `3` cancelled by you. Colors turn off when output is piped, with `--no-color`, or when `NO_COLOR` is set.

Every finding has a category (`model`, `cache`, `tmp`, `logs`), a risk level and a last-used date:

- `safe`: caches you can regenerate. Pre-selected in `clean`.
- `review`: session logs, local history, Docker images. Listed, unchecked.
- `careful`: downloaded models. Listed, unchecked.

`clean --yes` only ever acts on `safe` items, and skips items whose tool is currently running.

## What it looks at

- **Disk:** Claude Code session logs, Cursor / VS Code / Windsurf caches and workspace storage, Ollama, Hugging Face and LM Studio models, npm / pnpm / yarn / pip / uv / Go build caches, Docker (images, stopped containers, build cache, via the `docker` CLI), temp files older than 24 hours.
- **Processes:** MCP servers, language servers, dev servers (vite, next, webpack), Claude Code and its plugin workers, Ollama, and Cursor / VS Code / Windsurf helper processes.
- An **orphan** is a matched process whose parent chain ends at init or a dead parent, and that has used almost no CPU for 10 minutes. `ps` and `scan` show "likely" when the process has not been watched for the full window; `ps --watch` confirms it.

Paths follow the tool's own environment overrides (`HF_HOME`, `OLLAMA_MODELS`, `XDG_CACHE_HOME`, `CLAUDE_CONFIG_DIR`, `GOCACHE`, `npm config get cache`, and so on). Tools move their folders between versions, so please open an issue if a path is wrong on your machine.

## Safety

One bad deletion kills trust, so these rules are enforced in code and covered by tests:

1. Nothing is deleted without your selection and confirmation, except with `--yes`.
2. `clean` moves files to the OS Trash. Permanent deletion needs `--permanent` (and typing `delete`, unless `--yes`).
3. Only paths a detector produced can be deleted. There is no way to pass a path on the command line.
4. A hard denylist is always refused: `/`, your home folder, Documents, Desktop, Downloads, any `.git` folder, `.env` files, SSH and GPG keys, browser profiles and system folders.
5. Symlinks are resolved, and anything that ends up outside the expected folder is refused.
6. Files that are open in a running process are skipped, and so are caches of a tool that is running unless you confirm.
7. Config and auth files (`~/.claude.json`, `settings.json`, credentials, tokens) are never touched.
8. `kill` never touches other users' processes, PID below 100, this program or its parent shell, or anything no detector matches.
9. `--dry-run` shows exactly what a real run would do and changes nothing.
10. Every clean and kill action is logged to `~/.config/unclog/history.log`.
11. No network calls and no telemetry.

Trashing a file does not free disk space until you empty the Trash.

## Configuration

`unclog config init` writes a commented `~/.config/unclog/config.yaml` (`%APPDATA%\unclog` on Windows).

```yaml
exclude: []            # paths or globs that are never listed or removed
custom_paths: []       # extra directories to report (risk: review)
min_size: 0            # hide findings smaller than this, e.g. 100MB
orphan_idle_minutes: 10
quiet: false           # hide the footer
update_check: false    # reserved; unclog makes no network calls
```

## Not in this tool

No background daemon, scheduling, auto-cleanup, GUI or "RAM booster" that frees the OS file cache. unclog runs when you run it.

## Development

```
make build   # bin/unclog
make test
make lint    # needs golangci-lint
```

The scanning and cleaning logic lives in `internal/engine`, separate from the thin command layer in `cmd`. See `CLAUDE.md` for the conventions and safety rules.

## License

MIT, see [LICENSE](LICENSE).

## Support

unclog is free and open source. If it saved you some disk space, you can [buy me a coffee](https://buymeacoffee.com/niharranjan).
