# Docker demo

The Docker image is a **safe, disposable demonstration** of ditch-windows. It exists so
you can try the backup and restore workflow without installing anything on your Windows
machine and without giving the app access to your real profile.

## Safety model

- The container is Linux. It cannot see `C:\Users\...` unless you explicitly mount it.
  The documented commands do not mount any host directory.
- The demo home is generated at `/demo/home`. It contains only clearly synthetic text
  files.
- The restic repository is created at `/demo/repo` inside the container. `docker run
  --rm` deletes the container filesystem afterwards.
- The demo passphrase is published (`ditch-demo-only`) on purpose. It protects synthetic
  data only. **Never reuse it for a real repository.**
- A demo restore is either a dry run (default) or writes only beneath `/demo/home`.
  `.bak` handling is exercised there, not on the host.
- No UAC prompt, Windows registry write, `winget`, `netsh`, or `dism` command is
  executed.

If you add a bind mount, you are responsible for what you expose. The demo does not need
one. Use a named volume only if you want the synthetic repository to survive between
containers:

```bash
docker volume create ditch-demo-data
docker run --rm -v ditch-demo-data:/demo ditch-windows-demo
```

## What is real and what is simulated

| Part | In the container |
| --- | --- |
| restic repository, encryption, snapshots, checks | Real restic 0.14 |
| files module: scan, restic backup, preview, staged restore, conflicts | Real production module |
| metadata manifest, SHA-256 hashes, verification | Real production runner |
| `apps` module | Synthetic fixture standing in for winget export/import |
| `wifi` module | Synthetic XML with a fake password; demonstrates encrypted-only secret handling |
| `theme` module | Synthetic wallpaper/theme fixture; no registry access |
| UAC, VSS, Windows registry, `winget`, `netsh`, `dism` | Not available in Linux |

The demo therefore validates the storage and orchestration path, not Windows API
correctness. Real captures and restores must still be tested on Windows 11.

## Build and run

```bash
docker build -t ditch-windows-demo .

# Full-screen TUI; -it is required.
docker run --rm -it ditch-windows-demo

# Automated tour, useful for CI and non-interactive shells.
docker run --rm ditch-windows-demo
```

The image entrypoint picks the TUI when both stdin and stdout are terminals, and the
automated tour otherwise. You can always select one explicitly:

```bash
docker run --rm -it ditch-windows-demo demo --interactive
docker run --rm ditch-windows-demo demo
```

The default tour:

1. Seeds the synthetic home.
2. Backs up four demo modules through restic.
3. Lists the backup.
4. Edits one demo file locally.
5. Shows a restore preview with conflicts.
6. Verifies the repository and manifest hashes.

To demonstrate conflict preservation (`.bak` files):

```bash
docker run --rm ditch-windows-demo demo --apply
```

To explore interactively:

```bash
docker run --rm -it ditch-windows-demo
```

The TUI has a dashboard, module status table, restore preview, a confirmation step
before writing, backup list, verification result, and a file browser. Navigate with
`j`/`k` or the arrow keys, select with `enter`, or use the shortcuts:

| Key | Action |
| --- | --- |
| `b` | Run a backup |
| `p` | Preview the newest restore (dry run) |
| `a` | Apply the previewed restore (asks `y` to confirm) |
| `v` | Verify repository integrity and manifest hashes |
| `l` | List backups |
| `f` | Show synthetic demo files |
| `q` | Quit |

The TUI needs at least an 80x24 terminal. If the window is smaller, resize it; the
automated tour remains available without `-it`.

## Layout inside the container

```text
/demo/home     synthetic user profile (Documents, Desktop, Pictures, Downloads)
/demo/repo     encrypted restic repository
/demo/state    config, logs, local manifests
/demo/tmp      staging and secure Wi-Fi temp directories
```

The image runs as the unprivileged `demo` user (UID 10001). The restic repository and
all generated files are owned by that user.

## Cleanup

With `--rm`, removing the last container is enough. To remove the image:

```bash
docker rmi ditch-windows-demo
```

If you used a named volume:

```bash
docker volume rm ditch-demo-data
```

## Troubleshooting

**Docker Desktop says the WSL engine cannot start.** Restart Docker Desktop and run
`wsl --shutdown` from PowerShell, then retry. This is a Docker Desktop/WSL issue, not an
application error.

**The TUI does not open or exits immediately.** Use `docker run --rm -it`, from a real
interactive terminal rather than a pipe. The TUI also needs at least an 80x24 window.
Without a TTY the entrypoint deliberately falls back to the automated tour.

**The image builds but the demo fails with a restic error.** Rebuild without cache
(`docker build --no-cache -t ditch-windows-demo .`) to pick up the current code.

**You want a different demo directory.** Pass `--base /path/inside/container` and, if
needed, mount a volume there. Do not point `--base` at a host directory containing real
data.

**Docker is unavailable.** The normal Windows build does not need Docker; install restic
and follow the main README. Unit tests use fake command runners and never touch real
winget/netsh/dism.

## Why not Windows containers?

`winget`, `dism`, desktop UAC, VSS, and the interactive desktop are not available in
Windows Server Core containers. A Windows container would be larger, harder to run on
developer laptops, and still would not exercise the real GUI/desktop paths. The Linux
container is intentionally a **workflow demo**, not a Windows emulator.
