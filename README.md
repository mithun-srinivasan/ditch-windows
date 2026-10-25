# ditch-windows

**One TUI that rebuilds a fresh Windows 11 machine: apps, settings, Wi-Fi, look and feel, files, in an open, inspectable format.**

ditch-windows is an open-source (MIT) Go tool for capturing the state of a Windows 11
PC into an encrypted, deduplicated [restic](https://restic.net/) repository, then
restoring it after a clean install. It drives Windows' own tools (`winget`, `netsh`,
`dism`) and restic as subprocesses. It does not reimplement backup, deduplication, or
cryptography.

> **Status:** v0.1 development. The CLI is functional; the full-screen TUI and some
> documentation are still being built. Test restores on a non-critical machine first.

## Features

- **Apps** — `winget export`/`import`, plus detection of Store, Scoop, and Chocolatey
  apps and a generated `manual-reinstall.md` for software winget cannot restore.
- **Wi-Fi** — exports profiles (keys included) into the encrypted repository only;
  plaintext exports live in a restricted temporary directory and are wiped immediately.
- **Fonts** — user fonts and non-default system fonts, with per-user or system-wide
  re-registration.
- **Theme** — wallpaper, `.theme` file, accent color, and light/dark mode using a small
  curated allowlist of HKCU values. Registry hives are never restored wholesale.
- **Default apps** — DISM default-association export/import (administrator required).
- **Files** — selected folders (Documents, Desktop, Pictures, Downloads by default)
  through restic, with VSS/`--use-fs-snapshot` for locked files.
- **Safe restores** — dry-run preview by default; conflicts default to
  `overwrite_keep_bak`, which preserves the existing file as `.bak`; `skip` and
  `ask_per_item` are available.
- **Portable storage** — local folder, USB drive, S3-compatible object storage, or any
  rclone remote, all through restic repository URLs.
- **Inspectable** — a `manifest.json` records tool version, Windows build, timestamps,
  per-module status, snapshot IDs, and SHA-256 hashes.
- **UAC when needed** — starts unelevated and relaunches elevated only when a selected
  module requires administrator rights.

## Requirements

- Windows 10/11 for capture and restore modules. The code cross-compiles and unit tests
  run on Linux.
- Go 1.22+ to build from source.
- [restic](https://restic.net/) on `PATH`, or set `restic_path` in config or
  `DITCH_RESTIC_BINARY` in the environment:

  ```powershell
  winget install restic.restic
  ```

- For S3 or rclone, configure credentials/remotes using restic's standard mechanisms.

## Install

Download a Windows release from
[GitHub Releases](https://github.com/mithun-srinivasan/ditch-windows/releases), or build:

```powershell
git clone https://github.com/mithun-srinivasan/ditch-windows.git
cd ditch-windows
go build -trimpath -o ditch-windows.exe ./cmd/ditch-windows
```

## Quick start

1. Install restic and initialize the config:

   ```powershell
   winget install restic.restic
   .\ditch-windows.exe config init --repo "$env:USERPROFILE\ditch-windows-repo"
   ```

2. Run the interactive menu:

   ```powershell
   .\ditch-windows.exe
   ```

3. Or run a headless backup for automation:

   ```powershell
   $env:RESTIC_PASSWORD = "a long unique passphrase"
   .\ditch-windows.exe backup --profile daily --headless
   ```

4. Restore. The default is a preview only:

   ```powershell
   .\ditch-windows.exe restore
   .\ditch-windows.exe restore --apply
   ```

On the first backup, ditch-windows initializes the restic repository and asks for the
passphrase twice. **A forgotten passphrase means the backup is unrecoverable.** Store
it in a password manager and keep an offline recovery copy.

Config lives at `%APPDATA%\ditch-windows\config.yaml`; logs and local manifests live
under the same directory.

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
ditch-windows config show             Show config and profiles
ditch-windows config init --repo URL  Create the initial config
ditch-windows version                 Print build information
```

Use `--repo <url>` with `backup`, `list`, `restore`, or `verify` to override the saved
repository for one run. Supported repository spellings include a local path,
`usb:E:`, `s3:s3.amazonaws.com/bucket/prefix`, `s3://bucket/prefix`, and
`rclone:remote:path`.

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

## Security notes

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

## What cannot be restored

ditch-windows deliberately does **not** back up or restore:

- Windows activation, digital license, or product keys.
- DPAPI secrets, Credential Manager entries, browser passwords, cookies, or sessions.
- Store-only apps and their per-app data (some can be reinstalled through winget/Store,
  but their state is not captured).
- Microsoft account state, OneDrive placeholders, or cloud-only files.
- Drivers, WSL distributions, SSH keys, and application configs (roadmap items).
- Whole registry hives or arbitrary `HKCU`/`HKLM` keys.
- Windows 11 default-app associations may be rejected by DISM; a manual fallback is
  documented in the generated report.

## Roadmap

- Full-screen bubbletea TUI with setup wizard, module picker, progress, and restore preview.
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

Tests use an injected `Exec` interface, so no real `winget`, `netsh`, or `dism` runs
are required. Contributions are welcome under the MIT license.

## Layout

```text
cmd/ditch-windows/  CLI entry point
internal/core/      module registry, runner, config, manifest, conflicts, elevation
internal/module/    winget, default-apps, wifi, fonts, theme, files
internal/platform/  Windows registry, fonts, theme providers
internal/restic/    restic subprocess wrapper
internal/storage/   repository URL parsing (local/USB/S3/rclone)
internal/tui/       interactive terminal UI (in progress)
```

## License

MIT. See [LICENSE](LICENSE).
