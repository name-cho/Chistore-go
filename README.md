# Chistore-go

Package manager for small terminal utilities. Written in Go, ships as one binary,
installs into the home directory and does not need root.

The command is `chistore`. This repository contains the manager only. Packages are
stored in separate repositories.

[Русская версия](README.ru.md)

## Names

Three names look similar, so a short explanation:

- Chistore-go: this project, the manager.
- chistore-repo: default package repository. It is connected after installation.
- Chistore: the old package repository and the old Bash manager. Kept for people
  who still use it.

The manager has no packages inside. It downloads `index.json` from a repository,
finds the package there and downloads its files from the same repository.

GitHub mirrors: [Chistore-go](https://github.com/name-cho/Chistore-go),
[chistore-repo](https://github.com/name-cho/chistore-repo),
[Chistore](https://github.com/name-cho/Chistore).

## Features

- one static binary, no dependencies for the manager itself
- several package repositories with priorities in `repos.json`
- reads `index.json`, falls back to the old `INDEX` and `<package>/info.md`
- checks SHA-256 of package files when the index has hashes
- installs into `$XDG_BIN_HOME` (`~/.local/bin` by default), no root needed
- imports the old `~/.local/share/chistore/manifest`
- can update itself from GitHub Releases and checks SHA-256 before replacing

## Install

Linux, macOS and WSL:

```bash
curl -fsSL https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.sh -o /tmp/chistore-install.sh
sed -n '1,120p' /tmp/chistore-install.sh
bash /tmp/chistore-install.sh
```

`less` is often missing in a minimal Arch WSL image. `sed` or `cat` works there.

If curl fails with `TLS connect error: error:00000000:lib(0)::reason(0)`:

```bash
sudo pacman -S --needed ca-certificates curl
curl -4 -fsSL https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.sh -o /tmp/chistore-install.sh
```

The `-4` option forces IPv4 and helps when IPv6 is broken. If that does not help,
check `date -u` and try the GitGud mirror:

```bash
curl -4 -fsSL https://gitgud.io/name-cho/Chistore-go/-/raw/main/scripts/install.sh -o /tmp/chistore-install.sh
bash /tmp/chistore-install.sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/name-cho/Chistore-go/main/scripts/install.ps1 -OutFile "$env:TEMP\chistore-install.ps1"
Get-Content "$env:TEMP\chistore-install.ps1"
powershell -ExecutionPolicy Bypass -File "$env:TEMP\chistore-install.ps1"
```

You can also download a release binary:

```bash
curl -fsSL https://github.com/name-cho/Chistore-go/releases/latest/download/chistore_linux_amd64 \
  -o ~/.local/bin/chistore
chmod +x ~/.local/bin/chistore
```

Other assets are `linux_arm64`, `darwin_amd64`, `darwin_arm64`,
`windows_amd64.exe` and `windows_arm64.exe`.

The installers put `chistore` into `~/.local/bin`
(`%LOCALAPPDATA%\Programs\chistore\bin` on Windows). If the directory is not in
`PATH`:

```bash
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.bashrc
. ~/.bashrc
```

## Usage

```bash
chistore search
chistore install envsnap
chistore list
chistore info envsnap
chistore upgrade
chistore remove envsnap
chistore purge envsnap
chistore self-update
```

Old flags still work:

| Flag | Action |
| --- | --- |
| `-h`, `--help` | show help |
| `-Ss [query]` | search repositories |
| `-S <pkg...>` | install packages |
| `-Syu` | upgrade installed packages |
| `-Rn <pkg...>` | remove, keep configs and data |
| `-Rns <pkg...>` | remove completely |
| `-Qet` | list installed packages |
| `-Qs [query]` | search installed packages |
| `-Qi <pkg>` | show package information |

If a package exists in several repositories, add `@repo`:

```bash
chistore install envsnap@gitgud
chistore info envsnap@github
```

Global options:

```bash
chistore --yes install envsnap
chistore --no-color list
chistore --repo URL search
chistore --no-self-update install envsnap
```

## Repositories

A fresh install has one repository, `gitgud`, which points to `chistore-repo`:

```json
{
  "schema": 1,
  "repos": [
    {
      "name": "gitgud",
      "url": "https://gitgud.io/name-cho/chistore-repo/-/raw/main",
      "priority": 10,
      "enabled": true
    }
  ]
}
```

Config file location:

- Linux, macOS, WSL: `~/.config/chistore/repos.json`
- Windows: `%APPDATA%\chistore\repos.json`

Lower priority wins when two repositories have the same package. A disabled
repository stays in the file but is skipped.

The list can be edited with commands:

```bash
chistore repo add github https://raw.githubusercontent.com/name-cho/chistore-repo/main --priority 20
chistore repo list
chistore repo remove github
chistore repo refresh
```

The example adds the GitHub mirror. It has priority 20, so the GitGud copy still
wins for duplicate packages.

Installed packages remember where they came from. `upgrade` uses that repository
even if another one has a lower priority number. Packages installed by older
versions have no origin, so the priority decides.

`--repo` and `CHISTORE_REPO` replace the configured list for one command:

```bash
chistore --repo https://gitgud.io/name-cho/chistore-repo/-/raw/main search
```

### Why the raw URL returns 404

The base raw address is not a file:

```text
https://gitgud.io/name-cho/chistore-repo/-/raw/main             -> 404
https://gitgud.io/name-cho/chistore-repo/-/raw/main/index.json -> 200
```

GitLab and GitHub raw endpoints serve files and do not show directory listings.
Opening the base URL in a browser gives 404, but the repository itself is fine.
`chistore` never requests the base URL. With the default config it requests:

1. `<base>/index.json`
2. `<base>/<package>/<payload>` for the packages it needs

If `index.json` is missing, it tries `<base>/INDEX` and then
`<base>/<package>/info.md`. A 404 on the base address does not affect installs.

To browse the repository, open its web page:

- <https://gitgud.io/name-cho/chistore-repo>
- <https://github.com/name-cho/chistore-repo>

## Package repository

A package repository is a Git repository with `index.json` and one directory per
package:

```text
chistore-repo/
├── index.json
├── INDEX
├── envsnap/
│   ├── info.md
│   └── envsnap
└── pystat/
    ├── info.md
    └── pystat.py
```

Example `index.json`:

```json
{
  "schema": 1,
  "name": "chistore-repo",
  "generated": "2026-10-27T08:58:42Z",
  "packages": [
    {
      "id": "envsnap",
      "name": "EnvSnap",
      "description": "Snapshot system & environment state with diff support",
      "version": "1.0.1",
      "size": "3.8K",
      "type": "single",
      "main": "envsnap",
      "sha256": "e6107bc55160ee4b328cbb5d28dce0d8fcb9d336fd2bb245fee24651db93cd00",
      "author": "name-cho",
      "homepage": "https://gitgud.io/name-cho",
      "source": "https://gitgud.io/name-cho/chistore-repo/-/raw/main/envsnap",
      "dependencies": [{ "manager": "sys", "packages": ["bash"] }]
    }
  ]
}
```

`INDEX` contains one package id per line. It is still written and read for older
clients and for the Bash manager. If `index.json` is absent, Chistore uses `INDEX`
and `<package>/info.md`.

## Adding a package

Work in a checkout of a package repository:

```bash
chistore pkg init hello
```

This creates `hello/info.md`, an executable template and rebuilds both indexes.
Fill in `info.md`, replace the executable, then run:

```bash
chistore pkg validate
chistore pkg index
```

`pkg index` also calculates SHA-256 for every package file.

Format of `info.md`:

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

Lines without `##` also work. Unknown fields are ignored, so new metadata does not
break old clients.

Package types:

- `single`: one executable file, `<package>/<MainFile>`
- `python`: a Python file installed into the shared chistore virtualenv
- `archive`: `<package>.tar.gz` unpacked into the package data directory

Dependency managers:

- `sys:` (also `pacman:`, `apt:`, `dnf:`) for system packages
- `pip:` (also `python:`) for `~/.local/share/chistore/venv`
- `npm:` (also `node:`) for `npm install -g`

On Windows, keep `.gitattributes` with `* text=auto eol=lf`. Git must serve LF
bytes. `pkg index` hashes the file as Git serves it, and a CRLF checkout produces
hashes that fail during install.

## Windows

The manager runs on Windows: install, list, info, upgrade, remove and `pkg`
commands work. Packages themselves are shell or Python programs, so running them
needs Git Bash, WSL or another `/bin/sh`.

## Environment variables

| Variable | Meaning |
| --- | --- |
| `CHISTORE_REPO` | use only this repository, ignore `repos.json` |
| `CHISTORE_UPDATE_URL` | GitHub Releases API URL for self-update |
| `CHISTORE_NO_AUTO_UPDATE=1` | disable the daily update check |
| `XDG_DATA_HOME` | data root, default `~/.local/share` |
| `XDG_CONFIG_HOME` | config root, default `~/.config` |
| `XDG_BIN_HOME` | binary install root, default `~/.local/bin` |

The install scripts also read `CHISTORE_BIN_DIR` and `CHISTORE_RELEASE_REPO`.

## Self-update

`chistore` checks GitHub Releases once a day and replaces itself. It checks the
SHA-256 from `checksums.txt` before replacing the binary. The current process keeps
running the old code until the next start.

Manual update:

```bash
chistore self-update
```

## Compatibility

- `INDEX`, `info.md`, single-file, Python and archive packages still work. `INDEX`
  is still generated next to `index.json`.
- Repositories without `index.json` use `INDEX` and `info.md`.
- A `repos.json` that still points to the old default repository
  (`gitgud.io/name-cho/Chistore/-/raw/main`) is moved to `chistore-repo` when
  loaded. Configs with extra repositories or a custom URL are left alone.
- The old pipe-separated `manifest` is imported into `state.json` on first run and
  renamed to `manifest.migrated`.
- Installed binaries are tracked and removed only from the chistore bin directory.
  Purge refuses paths outside the expected config and data directories.
- Archive extraction rejects absolute paths, `..`, Windows drive paths and links.

## Build and test

```bash
go test ./...
go vet ./...
make build
```

The release workflow builds `linux/amd64`, `linux/arm64`, `darwin/amd64`,
`darwin/arm64`, `windows/amd64` and `windows/arm64`.

## Uninstall

```bash
rm -f ~/.local/bin/chistore
rm -rf ~/.local/share/chistore ~/.config/chistore
```

Remove installed tools first if needed:

```bash
chistore list
chistore purge <package>
```
