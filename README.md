```text
  ____  _ _       _
 |  _ \(_) |_ ___| |__
 | | | | | __/ __| '_ \
 | |_| | | || (__| | | |
 |____/|_|\__\___|_| |_|

  ditch-windows  ::  pack up Windows. Put it back.
```

# ditch-windows

**Pack up Windows. Put it back.**

Rebuild a Windows 11 machine from one encrypted, inspectable backup: applications,
Wi-Fi, fonts, theme, default apps, and selected personal folders.

ditch-windows is an open-source (MIT) Go CLI/TUI that drives Windows' own tools
(`winget`, `netsh`, `dism`, the registry) and [restic](https://restic.net/) as
subprocesses. It does not reimplement backup, deduplication, encryption, or Windows
inventory APIs.

[![CI](https://github.com/mithun-srinivasan/ditch-windows/actions/workflows/ci.yml/badge.svg)](https://github.com/mithun-srinivasan/ditch-windows/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/Go-1.22%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/)
[![Windows 11](https://img.shields.io/badge/Windows-11-0078D4?logo=windows11&logoColor=white)](https://www.microsoft.com/windows/windows-11)
[![MIT](https://img.shields.io/badge/license-MIT-22c55e)](LICENSE)

> **Status: v0.1 development.** The CLI, backup/restore engine, modules, and isolated
> Docker demo are functional. The Docker demo has a full-screen terminal UI; the Windows
> screens are still being moved to the same interface. Test restores on a non-critical
> machine before relying on them.
>
> **Safety:** restores are previews unless you pass `--apply`. Existing files are kept as
> `.bak` copies by default. The restic passphrase is never written to config.

## Contents

- [Why ditch-windows?](#why-ditch-windows)
- [What it captures](#what-it-captures)
- [How it works](#how-it-works)
- [Where your data lives](#where-your-data-lives)
- [Requirements](#requirements)
- [Install](#install)
- [Quick start](#quick-start)
- [Restore safety](#restore-safety)
- [Docker demo](#docker-demo-safe-no-windows-required)
- [Commands](#commands)
- [Modules](#modules)
- [Security and privacy](#security-and-privacy)
- [What cannot be restored](#what-cannot-be-restored)
- [Documentation](#documentation)
- [Roadmap](#roadmap)
- [Development](#development)
- [Project layout](#project-layout)
- [License](#license)

## Why ditch-windows?

A clean Windows install is quick. Rebuilding *your* machine — finding the apps, joining
the right Wi-Fi, restoring fonts and wallpaper, putting the files back — is the slow part.
ditch-windows captures that working state in a form you can inspect and restore:

- **Applications** — a winget import file, installed-app inventory, and a manual
  reinstall list for software winget cannot handle.
- **Machine identity** — Wi-Fi profiles, user and non-default fonts, wallpaper, accent
  color, light/dark mode, and default-app associations.
- **Personal files** — selected folders through restic, with VSS where available for
  locked files.
- **One encrypted repository** — restic handles encryption, deduplication, compression,
  and local/USB/S3/rclone storage. The manifest stays small, versioned, and readable.

It is deliberately **not** a password manager, disk imager, or whole-registry backup.
Those boundaries are documented below.

## What it captures

| Area | Capture | Restore | Admin |
| --- | --- | --- | --- |
| Applications | `winget export` JSON, installed-app inventory, manual reinstall report | `winget import` with package/source agreements | No |
| Default apps | DISM default-association XML | DISM import (Windows 11 support varies) | Yes |
| Wi-Fi | `netsh wlan export` XML, keys included, encrypted repository only | `netsh wlan add profile` | No |
| Fonts | User fonts plus non-default system fonts | Copy and register per-user or system-wide | No* |
| Theme | Wallpaper, `.theme` file, accent, light/dark mode via curated HKCU values | Copy assets and write only curated values | No |
| Files | Selected folders via restic, VSS for locked files | Staged restore with conflict policy | Yes** |

\* System-wide font registration needs elevation; without it, fonts are restored per user.

\*\* The files module is marked administrator-only so restic can use VSS for locked files.

The default `daily` profile captures winget, Wi-Fi, fonts, theme, and files. The `full`
profile adds default-app associations. Profiles are editable in
`%APPDATA%\ditch-windows\config.yaml`.

## How it works

```text
Windows tools / APIs                    ditch-windows                     restic
-------------------                    --------------                     ------
winget, netsh, dism  --scan/backup-->  module adapters  --metadata-->  encrypted
registry allowlist                     runner + manifest  --payloads-->  repository
user folders                           SHA-256 hashes                     |
                                                                          v
restore preview  <--  manifest  <--  metadata snapshot  <--  restic restore
      |
      +-- dry run (default)
      |
      +-- apply (explicit)  -->  modules restore; existing files kept as .bak
```

Each backup produces:

1. One **metadata snapshot** tagged `kind=metadata,tool=ditch-windows,backup=<id>`.
2. Optional **module payload snapshots** for files and Wi-Fi secrets.
3. A versioned `manifest.json` with host/build information, module status, item counts,
   SHA-256 hashes, durations, and snapshot IDs.

The local manifest is a convenience copy. The encrypted repository is the source of
truth. See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for the component contract and
full backup/restore flows.

## Where your data lives

The short answer:

| Data | Default location | Encrypted | Required to restore |
| --- | --- | --- | --- |
| **Restic repository (the backup)** | `%USERPROFILE%\ditch-windows-repo` *suggested* | **Yes** | **Yes** |
| Config | `%APPDATA%\ditch-windows\config.yaml` | No | No |
| Local manifest copy | `%APPDATA%\ditch-windows\backups\<backup-id>\manifest.json` | No | No |
| Logs | `%APPDATA%\ditch-windows\logs\` | No | No |
| Temporary staging | `%APPDATA%\ditch-windows\tmp\` | Mostly no | No |
| Restic cache | `%LOCALAPPDATA%\restic\` (restic default) | No | No |
| Docker demo | `/demo/{home,repo,state,tmp}` | Synthetic only | No |

`%APPDATA%` normally means `C:\Users\<you>\AppData\Roaming`. The repository location is
whatever you pass to `config init --repo` or `--repo`: a local folder, USB drive,
S3-compatible bucket, or rclone remote.

**The restic passphrase is not stored in config.** A forgotten passphrase means the
backup is unrecoverable. Keep it in a password manager and an offline recovery copy.

Everything under `%APPDATA%\ditch-windows` is convenience state. The repository and its
passphrase are the backup. For exact paths, the repository layout, deletion guidance, and
the Docker map, read [docs/STORAGE.md](docs/STORAGE.md).

## Requirements

- Windows 10/11 for capture and restore modules. The orchestration layer cross-compiles
  and unit tests run on Linux.
- Go 1.22+ to build from source.
- [restic](https://restic.net/) on `PATH`, or set `restic_path` in config or
  `DITCH_RESTIC_BINARY`:

  ```powershell
  winget install restic.restic
  ```

- For S3 or rclone, configure credentials/remotes using restic's standard mechanisms.
- Docker Desktop only if you want the isolated demo.

## Install

Download a Windows release from
[GitHub Releases](https://github.com/mithun-srinivasan/ditch-windows/releases), or build:

```powershell
git clone https://github.com/mithun-srinivasan/ditch-windows.git
cd ditch-windows
go build -trimpath -o ditch-windows.exe ./cmd/ditch-windows
```

## Quick start

1. Install restic and initialize the config with your repository path:

   ```powershell
   winget install restic.restic
   .\ditch-windows.exe config init --repo "$env:USERPROFILE\ditch-windows-repo"
   .\ditch-windows.exe config show
   ```

2. Run the interactive menu, or start with a headless backup:

   ```powershell
   .\ditch-windows.exe
   .\ditch-windows.exe backup --profile daily --headless
   ```

   The first backup asks for a new restic passphrase twice and initializes the
   repository. For automation, supply it through `RESTIC_PASSWORD`:

   ```powershell
   $env:RESTIC_PASSWORD = "a long unique passphrase"
   ```

3. See what was captured:

   ```powershell
   .\ditch-windows.exe list
   ```

4. Preview a restore. Nothing is written in this step:

   ```powershell
   .\ditch-windows.exe restore
   .\ditch-windows.exe restore <backup-id>
   ```

5. Apply the restore. The CLI previews again, asks for confirmation, and keeps existing
   files as `.bak`:

   ```powershell
   .\ditch-windows.exe restore --apply
   ```

6. Verify repository integrity and manifest hashes:

   ```powershell
   .\ditch-windows.exe verify
   .\ditch-windows.exe verify --read-data
   ```

Back up to a different repository for one run with `--repo`, for example
`--repo "usb:E:"` or `--repo "s3:s3.amazonaws.com/my-bucket/ditch-windows"`.

## Restore safety

- `restore` is a **dry run** by default. `restore --apply` is required to write.
- The CLI always builds and prints a preview before asking for confirmation.
- The default conflict policy is `overwrite_keep_bak`: the existing file becomes
  `<name>.bak` (or `.bak.1`, `.bak.2`, ...) before the restored file is written.
- `skip` and `ask_per_item` conflict policies are available in config.
- Modules that need administrator rights trigger UAC only when selected and only when
  applying. They are never silently elevated.
- The Docker TUI refuses to apply until you have previewed the current backup, then asks
  for `y` confirmation.
- A partial backup still stores the successful modules and records the failures in the
  manifest.

## Docker demo (safe, no Windows required)

The demo runs in a disposable Linux container. It does **not** mount your home directory
or read any real files. It uses a synthetic home, real restic encryption, the real files
module, and clearly labelled fixtures for the Windows-only modules.

```bash
docker build -t ditch-windows-demo .

# Full-screen TUI. -it is required so the TUI can read keys and draw.
docker run --rm -it ditch-windows-demo

# Automated tour for scripts, CI, or a non-interactive shell.
docker run --rm ditch-windows-demo

# Explicitly select the TUI or the automated tour.
docker run --rm -it ditch-windows-demo demo --interactive
docker run --rm ditch-windows-demo demo

# Demonstrate .bak conflict preservation.
docker run --rm ditch-windows-demo demo --apply
```

Inside the TUI, use `j`/`k` or the arrow keys to move and `enter` to select. Shortcuts:
`b` backup, `p` preview restore, `a` apply restore, `v` verify, `l` backups,
`f` demo files, `q` quit. Applying asks for confirmation.

All demo application data lives under `/demo` inside the container. Restic also keeps a
cache in the disposable container home (`/home/demo/.cache/restic`). `--rm` throws both
away. The demo passphrase is published (`ditch-demo-only`) because the data is synthetic.
Never reuse it for real backups. See [docs/DOCKER.md](docs/DOCKER.md) for the safety model,
architecture, volume options, and troubleshooting.

The container is a product demo, not a Windows substitute: it cannot exercise `winget`,
`netsh`, `dism`, VSS, UAC, or the registry. Validate real captures and restores on a
Windows 11 machine.

## Commands

```text
ditch-windows                         Open the interactive menu
ditch-windows backup                  Back up the default profile
ditch-windows backup --profile full   Back up the full profile
ditch-windows backup --headless       Run without the interactive menu
ditch-windows list                    List available backups
ditch-windows restore                 Preview the newest backup
ditch-windows restore <backup-id>     Preview a selected backup
ditch-windows restore --apply         Preview, confirm, and apply
ditch-windows verify                  Check repository and newest backup
ditch-windows verify --read-data      Also read and verify data blobs
ditch-windows demo                    Safe automated synthetic Docker demo
ditch-windows demo --interactive      Full-screen demo TUI (requires a terminal)
ditch-windows demo --apply            Demo restore + .bak handling in the container
ditch-windows config show             Show config, repository, and profiles
ditch-windows config init --repo URL  Create the initial config
ditch-windows version                 Print build information
```

Use `--repo <url>` with `backup`, `list`, `restore`, or `verify` to override the saved
repository for one run. Supported repository spellings include a local path, `usb:E:`,
`s3:s3.amazonaws.com/bucket/prefix`, `s3://bucket/prefix`, and `rclone:remote:path`.

## Modules

| Module | What is captured | What is restored | Admin |
| --- | --- | --- | --- |
| `winget` | `winget export` JSON, installed-app inventory, manual reinstall list | `winget import` with package/source agreements | No |
| `default-apps` | DISM `Export-DefaultAppAssociations` XML | DISM import (Windows 11 support varies) | Yes |
| `wifi` | `netsh wlan export profile key=clear` XML, inside the encrypted repo | `netsh wlan add profile` | No |
| `fonts` | User fonts plus non-default system fonts | Copy to user/system font directory and register | No* |
| `theme` | Wallpaper, `.theme`, accent, dark/light mode (curated keys) | Copy assets and write only curated HKCU values | No |
| `files` | Selected folders via restic + VSS | Staged restore with conflict policy | Yes** |

\* System-wide font registration needs elevation; without it, fonts are restored per user.

\*\* The module is marked administrator-only so restic can use VSS for locked files.

Profiles are defined in `config.yaml`. `daily` captures winget, Wi-Fi, fonts, theme, and
files. `full` adds default-app associations.

## Security and privacy

- Restic encrypts and authenticates repository contents. ditch-windows never stores the
  passphrase in config; use `RESTIC_PASSWORD`, the interactive prompt, or a protected
  passphrase file in automation.
- Wi-Fi exports contain plaintext PSKs. They are written only to a mode-0700 temporary
  directory, backed up directly into restic, and overwritten plus deleted immediately.
  They never enter the unencrypted staging directory or logs.
- Registry access is allowlisted: wallpaper, theme, accent, dark-mode, and font
  registration values. Full registry hives are **never** exported or restored.
- Configuration and local manifests are restricted to the current user. Logs redact
  obvious secret text, but review them before sharing.
- The manifest contains hashes of module metadata, not file contents.
- Treat the restic repository as sensitive: it contains personal files and Wi-Fi keys.
- The Docker demo never mounts the host profile. The published demo passphrase protects
  synthetic data only.

## What cannot be restored

ditch-windows deliberately does **not** back up or restore:

- Windows activation, digital license, or product keys.
- DPAPI secrets, Credential Manager entries, browser passwords, cookies, or sessions.
- Store-only apps and their per-app data (some can be reinstalled through winget/Store,
  but their state is not captured).
- Microsoft account state, OneDrive placeholders, or cloud-only files.
- Drivers, WSL distributions, SSH keys, and application configs.
- Whole registry hives or arbitrary `HKCU`/`HKLM` keys.
- Windows 11 default-app associations may be rejected by DISM; a manual fallback is
  documented in the generated report.

These are good boundaries. A backup tool should not quietly copy credentials you did not
ask it to copy.

## Documentation

| Document | What it covers |
| --- | --- |
| [docs/DOCKER.md](docs/DOCKER.md) | Isolated demo safety model, TUI keys, container layout, troubleshooting |
| [docs/STORAGE.md](docs/STORAGE.md) | Every path, repository layout, backup/restore lifecycle, deletion, Docker map |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Components, module contract, flows, integrity model, extension guide |
| [assets/ascii-logo.txt](assets/ascii-logo.txt) | The wordmark used in this README and the demo TUI |

## Roadmap

- Move the Windows screens to the full-screen bubbletea TUI (the Docker demo TUI is done).
- Setup wizard, module picker, and richer restore preview.
- Curated application configs (VS Code, Git, Windows Terminal).
- Curated registry key backup.
- WSL distribution export/import.
- Driver export via `pnputil`.
- SSH keys and dotfiles.
- Scheduled backups through Task Scheduler.

## Development

```powershell
go test ./...
go vet ./...
$env:GOOS = "windows"
go build ./...
```

Tests use an injected `Exec` interface and an in-memory repository, so no real `winget`,
`netsh`, `dism`, or restic calls are required. Contributions are welcome under the MIT
license.

The Docker build and smoke test also run in CI. To test the demo locally:

```bash
docker build -t ditch-windows-demo .
docker run --rm ditch-windows-demo
```

## Project layout

```text
cmd/ditch-windows/  CLI entry point, demo session, TUI adapter
internal/core/      runner, config, manifest, conflicts, hashing, elevation, paths
internal/module/    winget, default-apps, wifi, fonts, theme, files
internal/platform/  Windows registry, fonts, theme providers
internal/restic/    restic subprocess wrapper and JSON parsing
internal/storage/   repository URL parsing (local/USB/S3/rclone)
internal/tui/       bubbletea/lipgloss full-screen demo interface
internal/demo/      synthetic fixtures and modules for the Docker demo
assets/             ASCII wordmark
docs/               Docker, storage, and architecture documentation
Dockerfile          safe Linux demo image (restic + synthetic fixtures)
```

## License

MIT. See [LICENSE](LICENSE).
