# Demo guide

This guide gets anyone — a teammate, a conference audience, a reviewer, or you — from
zero to a working ditch-windows backup and restore in about five minutes.

The demo is **safe by construction**. It builds a synthetic home directory in a
temporary folder, backs it up with a real encrypted restic repository, previews and
optionally applies a restore, and verifies the result. It never reads your real
profile, never installs applications, and never prompts for administrator rights.

- [Pick a path](#pick-a-path)
- [What the demo proves](#what-the-demo-proves)
- [Option 1: one command from source](#option-1-one-command-from-source)
- [Option 2: prebuilt Windows binary](#option-2-prebuilt-windows-binary)
- [Option 3: Docker (no Windows, no Go)](#option-3-docker-no-windows-no-go)
- [The interactive TUI](#the-interactive-tui)
- [Apply mode and `.bak` files](#apply-mode-and-bak-files)
- [Five-minute live demo script](#five-minute-live-demo-script)
- [Options and environment variables](#options-and-environment-variables)
- [Where the demo writes](#where-the-demo-writes)
- [What is real and what is simulated](#what-is-real-and-what-is-simulated)
- [Safety and privacy](#safety-and-privacy)
- [Troubleshooting](#troubleshooting)
- [Cleanup](#cleanup)
- [Sharing, recording, and CI](#sharing-recording-and-ci)
- [FAQ](#faq)

## Pick a path

| You have | Use | Command |
| --- | --- | --- |
| Go 1.22+ (Windows, Linux, or macOS) | Source checkout | `go run ./cmd/ditch-windows demo` |
| Windows, no Go | Release archive | `.\ditch-windows.exe demo` |
| Docker, no Windows and no Go | Container image | `docker run --rm ditch-windows-demo` |
| A live audience and a terminal | Interactive TUI | `ditch-windows demo --interactive` |
| A script, CI job, or screen recording | Automated tour | `ditch-windows demo` |

All four paths run the same demo code and produce the same result.

## What the demo proves

The demo exercises the production pipeline, not a slideshow:

1. **Real restic repository** — encryption, snapshots, deduplication, `check`, and
   restore all run through the real restic binary.
2. **Real files module** — scanning, globs, exclusions, staged restore, and conflict
   handling are the same code used on a real Windows machine.
3. **Real runner and manifest** — module status, counts, durations, SHA-256 hashes, and
   the local manifest copy are produced by the production runner.
4. **Real verification** — `repository_ok=true` and `manifest_hashes_ok=true` come from
   an actual repository check and manifest hash comparison.
5. **Honest Windows boundaries** — `winget`, `netsh`, `dism`, VSS, UAC, and the registry
   are replaced with clearly labelled fixtures, so the demo never pretends to test
   Windows API behaviour it cannot reach.

## Option 1: one command from source

Requirements: [Go 1.22+](https://go.dev/dl/) and network access on the first run (to
download restic). No Node.js, Bun, Docker, or preinstalled restic is required.

```powershell
git clone https://github.com/mithun-srinivasan/ditch-windows.git
cd ditch-windows
go run ./cmd/ditch-windows demo
```

On Linux or macOS the command is identical:

```bash
git clone https://github.com/mithun-srinivasan/ditch-windows.git
cd ditch-windows
go run ./cmd/ditch-windows demo
```

### What happens on the first run

1. A demo root is created at `%TEMP%\ditch-windows-demo` (Windows) or
   `$TMPDIR/ditch-windows-demo` (Linux/macOS).
2. If restic is not already available, a pinned, checksum-verified release is
   downloaded into `<demo-root>\state\bin`. Nothing is written outside the demo root.
3. A synthetic home is seeded with documents, a desktop note, a picture note, a
   download, and two intentionally excluded files.
4. Four demo modules run: `apps`, `wifi`, `files`, and `theme`.
5. The backup is listed, one file is edited locally, and a restore is previewed.
6. The repository and manifest hashes are verified.

The demo passphrase is `ditch-demo-only`. It is published on purpose because the data
is synthetic. Never reuse it for a real repository.

### Expected output

The exact paths and counts vary by machine, but the shape is stable:

```text
  downloading restic v0.19.1 (restic_0.19.1_windows_amd64.zip)
  checksum verified; installing to C:\Users\<you>\AppData\Local\Temp\ditch-demo-doc\state\bin
ditch-windows — isolated synthetic demo
  demo root:      C:\Users\<you>\AppData\Local\Temp\ditch-demo-doc
  synthetic home: C:\Users\<you>\AppData\Local\Temp\ditch-demo-doc\home
  restic repo:    C:\Users\<you>\AppData\Local\Temp\ditch-demo-doc\repo
  demo passphrase: ditch-demo-only (published; synthetic data only)
  Your real files are not mounted or read. Everything above is disposable.

Backing up demo modules: apps, wifi, files, theme

  apps     ok         3 winget, 2 store/scoop, 1 manual reinstall
  files    ok         5 file(s), 330 B across 4 folder(s); VSS unavailable (not elevated)
  theme    ok         1 wallpaper, dark mode, blue accent
  wifi     ok         2 synthetic Wi-Fi profiles; XML stays encrypted

Backup ID: 20261107T140609Z-dd23e0
Snapshots: 3 (metadata + module payloads)

Backups in the demo repository:
  20261107T140609Z-dd23e0  2026-11-07T19:36:14+05:30  <hostname>

Simulating a local edit made after the backup...
Restore preview: backup 20261107T140609Z-dd23e0 from 2026-11-07T19:36:09+05:30 (<hostname>)
  apps           ready           6 items — 3 winget, 2 store/scoop, 1 manual reinstall
  files          ready           5 items — 4 folder(s)
  theme          ready           1 items — 1 wallpaper, dark mode, blue accent
  wifi           ready           2 items — 2 synthetic Wi-Fi profiles; XML stays encrypted
Files: 5, bytes: 330, conflicts: 5
Dry run only; the synthetic home was not changed.

Restore was a dry run. Re-run with --apply to see .bak conflict handling inside the demo.

Verifying repository and manifest hashes...
Verified backup 20261107T140609Z-dd23e0: repository_ok=true manifest_hashes_ok=true modules=4

Demo complete. The demo directory is disposable and can be deleted at any time.
Interactive demo: ditch-windows demo --interactive
```

The `VSS unavailable (not elevated)` note is expected and correct: the demo does not
need a volume snapshot because its files are synthetic and not locked.

## Option 2: prebuilt Windows binary

Download `ditch-windows_<version>_windows_amd64.zip` from
[GitHub Releases](https://github.com/mithun-srinivasan/ditch-windows/releases), unzip it,
and run:

```powershell
.\ditch-windows.exe demo
```

The archive contains `ditch-windows.exe` and a self-contained `ditch-windows-tui.exe`.
You do not need Go, Node.js, Bun, or a preinstalled restic. If Windows SmartScreen warns
about an unsigned binary, choose **More info → Run anyway**, or build from source.

On Windows 11 on ARM, the x64 binary runs under the built-in x64 emulation.

## Option 3: Docker (no Windows, no Go)

The container is a disposable Linux sandbox. It does not mount your host profile.

```bash
docker build -t ditch-windows-demo .

# Automated tour (default when stdin/stdout are not a terminal).
docker run --rm ditch-windows-demo

# Full-screen TUI (-it is required).
docker run --rm -it ditch-windows-demo
```

The image already contains restic and the built UI, so it needs no network at run time.
Everything is written under `/demo` inside the container and disappears with `--rm`.

See [DOCKER.md](DOCKER.md) for the container safety model, layout, and Docker-specific
troubleshooting.

## The interactive TUI

Use the interactive mode for a live audience:

```powershell
.\ditch-windows.exe demo --interactive
```

In Docker:

```bash
docker run --rm -it ditch-windows-demo
```

The TUI opens on a dashboard with the safety model, module status, restore preview,
backup list, and verification results. It needs a real terminal of at least 80x24.
Without a TTY, `demo --interactive` deliberately falls back to the numbered menu.

| Key | Action |
| --- | --- |
| `j` / `k` or arrows | Move |
| `enter` | Select |
| `b` | Run a backup |
| `p` | Preview the newest restore (dry run) |
| `a` | Apply the previewed restore (asks `y` to confirm) |
| `v` | Verify repository integrity and manifest hashes |
| `l` | List backups |
| `f` | Show synthetic demo files |
| `q` | Quit |

The numbered fallback menu offers the same operations as numbers 1–7.

## Apply mode and `.bak` files

The automated tour is a dry run by default. To show what happens when a restore meets a
file you edited after the backup, add `--apply`:

```powershell
.\ditch-windows.exe demo --apply
```

```bash
docker run --rm ditch-windows-demo demo --apply
```

The restore writes only inside the synthetic demo home. Existing files are preserved
with a `.bak` suffix:

```text
Applying the restore to the synthetic demo home (never the host)...
    files: restoring files snapshot (this may take a while)
    files: merging Documents
    files: Documents: 2 file(s), 2 conflict(s), 2 .bak file(s)
    files: merging Desktop
    files: Desktop: 1 file(s), 1 conflict(s), 1 .bak file(s)
    files: merging Pictures
    files: Pictures: 1 file(s), 1 conflict(s), 1 .bak file(s)
    files: merging Downloads
    files: Downloads: 1 file(s), 1 conflict(s), 1 .bak file(s)

Restore results:
  apps     ok
  files    ok
  theme    ok
  wifi     ok

Conflict handling: existing files were preserved:
  Desktop/readme.txt.bak
  Documents/notes/todo.md.bak
  Documents/welcome.txt.bak
  Downloads/sample-download.txt.bak
  Pictures/photo-notes.txt.bak

Verifying repository and manifest hashes...
Verified backup 20261107T140609Z-dd23e0: repository_ok=true manifest_hashes_ok=true modules=4
```

This is the core promise of the product in one screen: **nothing is lost silently**.

## Five-minute live demo script

A suggested narration. Times are cumulative and assume the source checkout is already
cloned and dependencies are cached.

| Time | Do | Say |
| --- | --- | --- |
| 0:00 | Show the README tagline | "Rebuilding Windows is easy. Rebuilding *your* Windows is the slow part." |
| 0:20 | `go run ./cmd/ditch-windows demo` | "This creates a fake home, a real encrypted restic repo, and backs it up. No admin, no real files." |
| 1:10 | Point at the module table | "Apps, Wi-Fi, files, and theme. On real Windows these come from winget, netsh, the registry, and VSS." |
| 1:40 | Point at `Backup ID` and `Snapshots` | "One metadata snapshot plus payload snapshots, all content-addressed and deduplicated." |
| 2:00 | Point at `Simulating a local edit` | "I changed a file after the backup. This is the conflict a restore has to handle." |
| 2:30 | Point at `conflicts: 5` | "The restore is a dry run. It tells you exactly what would change before anything is written." |
| 3:00 | Re-run with `--apply` | "Now I opt in. Existing files become `.bak` copies — nothing is overwritten silently." |
| 3:40 | Point at `repository_ok=true` | "The repository and manifest hashes both verify. This is the integrity story." |
| 4:10 | `.\ditch-windows.exe setup` | "For a real machine, one command installs restic, writes the config, and builds the UI." |
| 4:30 | Open `.\ditch-windows.exe` | "Then the full-screen UI drives the same engine." |

Useful answers for questions:

- **"Does it touch my machine?"** No. The demo root is a temp directory; the repository
  and even the downloaded restic live inside it.
- **"Is the encryption real?"** Yes. It is a real restic repository with a real
  passphrase. The passphrase is published only because the data is synthetic.
- **"Does it reinstall apps?"** Not in the demo. The `apps` module is a fixture. On real
  Windows it exports and imports a winget manifest.
- **"What about locked files?"** Real backups use VSS when elevated. The demo does not
  need it and says so in the module status.
- **"Can I restore to a new PC?"** That is the intended workflow: install ditch-windows,
  run `setup`, point it at the repository, and `restore --apply`.

## Options and environment variables

| Flag | Effect |
| --- | --- |
| `--base <dir>` | Use a specific demo directory instead of the temp default |
| `--interactive` | Open the full-screen TUI (numbered menu without a TTY) |
| `--apply` | Apply the restore inside the demo home and show `.bak` handling |

| Variable | Effect |
| --- | --- |
| `DITCH_DEMO_DIR` | Default demo directory when `--base` is not passed |
| `RESTIC_PASSWORD` | Override the demo passphrase (the demo data is still synthetic) |
| `DITCH_RESTIC_BINARY` | Use an existing restic binary instead of downloading one |
| `DITCH_RESTIC_VERSION` | Pin a different restic version (default `0.19.1`) |
| `DITCH_RESTIC_DOWNLOAD_BASE` | Use a mirror for restic release downloads |
| `DITCH_TUI_BIN` | Point the launcher at a specific TUI entry point |
| `DITCH_UI_DIR` | Point the launcher at a specific UI root |

The demo refuses to use a filesystem root as `--base`. Use a normal subdirectory.

## Where the demo writes

Everything lives under one demo root, which defaults to a temporary directory:

```text
<demo-root>/
  home/      synthetic user profile (Documents, Desktop, Pictures, Downloads)
  repo/      encrypted restic repository
  state/     config, logs, local manifests, and the downloaded restic binary
  tmp/       staging and secure temporary directories
```

| Platform | Default demo root |
| --- | --- |
| Windows | `%TEMP%\ditch-windows-demo` |
| Linux / macOS | `$TMPDIR/ditch-windows-demo` (usually `/tmp/ditch-windows-demo`) |
| Docker | `/demo` |

The demo overrides `DITCH_WINDOWS_HOME` to `<demo-root>/state`, so it does not read or
write `%APPDATA%\ditch-windows`. A restic cache may also appear in the normal restic
cache location; it is a cache, not a backup, and can be deleted at any time.

## What is real and what is simulated

| Part | In the demo |
| --- | --- |
| restic repository, encryption, snapshots, checks, restore | Real |
| files module: scan, globs, backup, preview, staged restore, conflicts | Real production module |
| runner, module status, manifest, SHA-256 hashes, verification | Real production code |
| `apps` module | Synthetic fixture standing in for winget export/import |
| `wifi` module | Synthetic profiles with fake passwords; shows encrypted-only secret handling |
| `theme` module | Synthetic wallpaper/theme fixture; no registry access |
| UAC, VSS, Windows registry, `winget`, `netsh`, `dism` | Not exercised |

The demo validates storage, orchestration, restore safety, and verification. Real
captures and restores must still be validated on Windows 11.

## Safety and privacy

- The demo never mounts, scans, or reads your real user profile.
- It never runs `winget`, `netsh`, `dism`, or a registry write.
- It never requests administrator rights.
- A restore is a dry run unless you pass `--apply`, and `--apply` writes only inside the
  synthetic demo home.
- The repository is real restic encryption; the published passphrase protects synthetic
  data only. **Never reuse `ditch-demo-only` for real backups.**
- Deleting the demo root deletes the synthetic home, the repository, and the demo's
  restic binary. Nothing else is affected.

## Troubleshooting

**`restic was not found` or the download fails.** The demo needs network access the
first time. If GitHub is blocked, install restic yourself and point the demo at it:

```powershell
winget install restic.restic
$env:DITCH_RESTIC_BINARY = (Get-Command restic).Source
```

or set `DITCH_RESTIC_DOWNLOAD_BASE` to an internal mirror that serves the same
`v<version>/restic_<version>_<os>_<arch>.zip` and `SHA256SUMS` layout. A checksum
mismatch is always rejected; nothing is installed before verification.

**The TUI does not open, or exits immediately.** It needs a real interactive terminal of
at least 80x24. Run it from Windows Terminal or a normal shell, not from a pipe. Without
a TTY, `--interactive` falls back to the numbered menu on purpose.

**The TUI shows an old interface.** Rebuild the frontend with `npm install` and
`npm run build` in `ui/`, or run `ditch-windows setup` to do it for you. The launcher
also requires `ui/node_modules` next to `ui/dist/index.js` because the bundle keeps
packages external.

**`go` is not installed.** Use the prebuilt release binary or Docker. Only the source
path needs Go.

**SmartScreen blocks the release binary.** The build is unsigned. Choose **More info →
Run anyway**, or build from source.

**Docker says the engine cannot start.** Restart Docker Desktop, run `wsl --shutdown`
from PowerShell, and retry. This is a Docker Desktop/WSL issue, not ditch-windows.

**The demo directory is locked or contains old state.** Pass a fresh directory:
`demo --base "$env:TEMP\ditch-demo-2"`. To start completely clean, delete the old demo
root first.

**Windows long-path errors.** Use a short demo root such as `C:\demo` or enable long
paths in Windows settings.

**The output counts differ from this guide.** File counts and byte totals depend on
platform line endings and on whether the demo directory already contained files. The
module states and verification results are the meaningful part.

## Cleanup

```powershell
Remove-Item -Recurse -Force "$env:TEMP\ditch-windows-demo"
```

```bash
rm -rf "${TMPDIR:-/tmp}/ditch-windows-demo"
```

```bash
docker rmi ditch-windows-demo
```

Nothing outside the demo root is required for cleanup. If you set `RESTIC_PASSWORD` or
`DITCH_RESTIC_BINARY` only for the demo, close the shell or unset them.

## Sharing, recording, and CI

The automated tour is deterministic and non-interactive, which makes it suitable for
recordings and CI:

```bash
ditch-windows demo
```

- **Record a terminal demo:** use [asciinema](https://asciinema.org/) or your terminal's
  recording feature. The tour finishes on its own and exits.
- **Run in CI:** `go run ./cmd/ditch-windows demo` needs network only for the first
  restic download. Preinstall restic or set `DITCH_RESTIC_BINARY` for offline runners.
- **Share without a Windows machine:** point people at the Docker image, which bundles
  the binary, restic, and the UI.
- **Embed in a README or slide:** the `demo --apply` output above is a good screenshot
  candidate; it shows the dry-run preview, the applied restore, `.bak` preservation, and
  verification in one place.

Exit code `0` means every demo step succeeded. A failed module or verification returns a
non-zero code, so the tour is safe to use as a smoke test.

## FAQ

**Is this the same code that backs up a real PC?** Yes for restic, the files module, the
runner, the manifest, and verification. Windows-only integrations are fixtures.

**Do I need administrator rights?** No. The demo is explicitly non-elevated and reports
VSS as unavailable.

**Why download restic instead of bundling it?** Restic is a separate, independently
versioned project. The demo pins a version, verifies its SHA-256 checksum, and installs
it into the demo root. Releases of ditch-windows also keep restic separate, but `setup`
automates the same download for real use.

**Can I reuse the demo repository?** Yes. Pass the same `--base` again; each run adds a
backup. This is useful for showing deduplication or a second restore. Use a fresh base
when you want a clean slate.

**Can I run this on Linux or macOS?** Yes. The orchestration, restic pipeline, files
module, and UI are cross-platform. Only the Windows capture modules are
platform-specific.

**Where do I go next?** Read the [main README](../README.md) for real setup and restore
safety, [STORAGE.md](STORAGE.md) for every path and the repository lifecycle, and
[ARCHITECTURE.md](ARCHITECTURE.md) for the module contract.
