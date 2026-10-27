# Chistore-go

A small, fast package manager for user terminal utilities. This is a Go rewrite of
[Chistore](https://gitgud.io/name-cho/Chistore): one static binary instead of a Bash script,
the same package repository format, and a much easier path for adding packages.

- Single binary, no runtime dependencies for the manager itself
- Multiple Git repositories with priorities, configured in `repos.json`
- Reads `index.json` first and falls back to the legacy `INDEX` + `<package>/info.md`
- Verifies SHA-256 payload hashes when the index provides them
- Installs to `$XDG_BIN_HOME` (`~/.local/bin` by default), no root required
- Migrates the old `~/.local/share/chistore/manifest` automatically
- Self-updates from GitHub Releases, with SHA-256 verification

Chistore started as a Bash script by [name-cho](https://github.com/name-cho); this Go
implementation is maintained by the same author.

## Mirrors

- GitHub: <https://github.com/name-cho/Chistore-go>
- GitGud: <https://gitgud.io/name-cho/Chistore-go>

The package repository stays separate, and GitGud is the primary source:

- GitGud: <https://gitgud.io/name-cho/Chistore>
- GitHub: <https://github.com/name-cho/Chistore>

## Install

Linux, macOS and WSL:

```bash
curl -fsSL https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.sh -o /tmp/chistore-install.sh
less /tmp/chistore-install.sh   # optional, but a good habit
bash /tmp/chistore-install.sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.ps1 -OutFile "$env:TEMP\chistore-install.ps1"
Get-Content "$env:TEMP\chistore-install.ps1"
powershell -ExecutionPolicy Bypass -File "$env:TEMP\chistore-install.ps1"
```

The installers put `chistore` in `~/.local/bin` (`%LOCALAPPDATA%\Programs\chistore\bin`
on Windows). Make sure that directory is in `PATH`.

You can also download a release binary directly:

```bash
curl -fsSL https://github.com/name-cho/Chistore-go/releases/latest/download/chistore_linux_amd64 \
  -o ~/.local/bin/chistore
chmod +x ~/.local/bin/chistore
```

## Usage

The old flags work exactly as before:

| Flag | Action |
| --- | --- |
| `-h`, `--help` | Show help |
| `-Ss [query]` | Search repositories |
| `-S <pkg...>` | Install package(s) |
| `-Syu` | Upgrade installed packages |
| `-Rn <pkg...>` | Remove (keep configs and data) |
| `-Rns <pkg...>` | Remove completely (purge) |
| `-Qet` | List installed packages |
| `-Qs [query]` | Search installed packages |
| `-Qi <pkg>` | Show package information |

Readable subcommands are available too:

```bash
chistore search env
chistore install envsnap
chistore upgrade
chistore remove envsnap
chistore purge envsnap
chistore list
chistore info envsnap
chistore self-update
```

Use `pkg@repo` to pick a repository when several provide the same package:

```bash
chistore install envsnap@gitgud
chistore info envsnap@github
```

Useful globals:

```bash
chistore --yes install demo     # no prompts
chistore --no-color list       # plain output
chistore --repo URL search     # use only this repository
chistore --no-self-update ...  # skip the daily update check
```

## Repositories

Repositories are listed in `repos.json`:

- Linux and macOS: `~/.config/chistore/repos.json`
- Windows: `%APPDATA%\chistore\repos.json`

The default configuration contains one repository:

```json
{
  "schema": 1,
  "repos": [
    {
      "name": "gitgud",
      "url": "https://gitgud.io/name-cho/Chistore/-/raw/main",
      "priority": 10,
      "enabled": true
    }
  ]
}
```

Lower `priority` wins when two repositories provide the same package. Disabled
repositories are skipped without being deleted.

Manage repositories without editing the file:

```bash
chistore repo add github https://raw.githubusercontent.com/name-cho/Chistore/main --priority 20
chistore repo list
chistore repo remove github
chistore repo refresh          # contact every repository and report package counts
```

Installed packages remember the repository they came from. `upgrade` takes new
versions from that repository even if another one has a higher priority. Packages
installed by older chistore versions have no recorded repository and are resolved
by priority.

`--repo URL` and `CHISTORE_REPO` override the whole list with a single repository
for one command, which is handy for testing.

## Package format

A repository is a Git repository with an `index.json` and one directory per package:

```text
Chistore/
├── index.json
├── INDEX
├── envsnap/
│   ├── info.md
│   └── envsnap
└── pystat/
    ├── info.md
    └── pystat.py
```

`index.json` is the primary index:

```json
{
  "schema": 1,
  "name": "Chistore",
  "generated": "2026-10-27T08:00:00Z",
  "packages": [
    {
      "id": "envsnap",
      "name": "EnvSnap",
      "description": "Snapshot system & environment state with diff support",
      "version": "1.0.1",
      "size": "3.8K",
      "type": "single",
      "main": "envsnap",
      "sha256": "44d1544afee88e196e6140a0e07cc32de814619392f4ec3641b5138fa93c7442",
      "author": "name-cho",
      "homepage": "https://gitgud.io/name-cho",
      "source": "https://gitgud.io/name-cho/Chistore/-/raw/main/envsnap",
      "dependencies": [{ "manager": "sys", "packages": ["bash"] }]
    }
  ]
}
```

The legacy `INDEX` file (one package id per line) is still written and read, so
older chistore versions keep working. If `index.json` is missing, the manager
falls back to `INDEX` + `<package>/info.md`.

## Adding a package

Go to a checkout of the package repository (`Chistore`) and run:

```bash
chistore pkg init hello
```

That creates the package directory, `info.md`, an executable template and rebuilds
`index.json` and `INDEX`. Fill in `info.md`, replace the executable, then:

```bash
chistore pkg validate   # checks metadata, payloads and both indexes
chistore pkg index      # rebuilds index.json and INDEX from package directories
```

`pkg index` also computes the SHA-256 of every payload, so installs are verified.

`info.md` is intentionally simple:

```markdown
## Name: Hello
## Description: Example utility
## Version: 1.0.0
## Size: 1K
## Type: single
## MainFile: hello
## Author: your-name
## Homepage: https://example.com
## Dependencies: sys:bash,curl pip:rich
```

Plain `Name: Hello` lines are accepted as well. Unknown fields are ignored, so new
metadata can be added without breaking older clients.

Supported package types:

- `single` — one executable file (`<package>/<MainFile>`)
- `python` — a Python file installed into the shared chistore virtualenv
- `archive` — a `<package>.tar.gz` extracted into the package data directory

Supported dependency managers:

- `sys:` (or `pacman:`, `apt:`, `dnf:`) — system packages
- `pip:` (or `python:`) — installed into `~/.local/share/chistore/venv`
- `npm:` (or `node:`) — installed globally with `npm install -g`

## Windows notes

The manager runs natively on Windows (install, list, info, upgrade, remove and
`pkg` tooling all work). Package payloads are POSIX shell or Python tools, so
running them requires Git Bash, WSL, or another `/bin/sh` provider.

## Configuration

| Variable | Meaning |
| --- | --- |
| `CHISTORE_REPO` | Use only this repository, ignoring `repos.json` |
| `CHISTORE_UPDATE_URL` | GitHub Releases API URL used by self-update |
| `CHISTORE_NO_AUTO_UPDATE=1` | Disable the daily self-update check |
| `XDG_DATA_HOME` | Data root. Default: `~/.local/share` |
| `XDG_CONFIG_HOME` | Config root. Default: `~/.config` (holds `chistore/repos.json`) |
| `XDG_BIN_HOME` | Binary install root. Default: `~/.local/bin` |

The install scripts additionally honour `CHISTORE_BIN_DIR` (target directory) and
`CHISTORE_RELEASE_REPO` (GitHub `owner/repo` for release downloads).

To switch the only repository to the GitHub mirror of the package repository:

```bash
export CHISTORE_REPO="https://raw.githubusercontent.com/name-cho/Chistore/main"
```

## Self-update

`chistore` checks GitHub Releases at most once per day and replaces itself atomically.
SHA-256 checksums from `checksums.txt` are verified before the binary is replaced.
The running process keeps using the old code until you start `chistore` again.

To update explicitly:

```bash
chistore self-update
```

## Compatibility

- Existing packages keep working: `INDEX`, `info.md`, single-file, Python and archive
  layouts are unchanged, and `INDEX` is still generated next to `index.json`.
- Repositories without `index.json` are read through the legacy `INDEX` + `info.md`
  path automatically.
- The old pipe-separated `manifest` is imported into `state.json` on the first run.
  The original file is renamed to `manifest.migrated`.
- Installed package binaries are tracked and removed only from the chistore bin
  directory; purge refuses paths outside the expected config/data directories.
- Archive extraction rejects absolute paths, `..` traversal, Windows drive paths and
  archive links.

## Build and test

```bash
go test ./...
go vet ./...
make build          # or: go build -trimpath -ldflags "-s -w -X main.version=$(cat VERSION)" .
```

The release workflow builds:

- `linux/amd64`, `linux/arm64`
- `darwin/amd64`, `darwin/arm64`
- `windows/amd64`, `windows/arm64`

## Uninstall

```bash
rm -f ~/.local/bin/chistore
rm -rf ~/.local/share/chistore ~/.config/chistore
```

Remove installed tools first if you want a clean sweep:

```bash
chistore list
chistore purge <package>
```
