# Where ditch-windows stores data

The short version:

- **The encrypted restic repository is the backup.** Its location is whatever
  `repo_url` points to — often `%USERPROFILE%\ditch-windows-repo`, but it can be a USB
  drive, S3 bucket, or rclone remote.
- **`%APPDATA%\ditch-windows` holds local state only:** config, logs, temporary staging,
  and a plaintext copy of each backup's manifest.
- **The restic passphrase is never written to config.** Without it, the repository is
  unrecoverable.
- **The Docker demo stores everything under `/demo`** and never mounts your Windows
  profile.

This page explains every path, what is inside it, and what is safe to delete.

## Windows storage map

`%APPDATA%` normally expands to `C:\Users\<you>\AppData\Roaming`. `%USERPROFILE%`
normally expands to `C:\Users\<you>`.

| Data | Default location | Encrypted? | Needed for restore? |
| --- | --- | --- | --- |
| Restic repository (the actual backup) | `%USERPROFILE%\ditch-windows-repo` *(suggested; see below)* | **Yes** | **Yes** |
| Configuration | `%APPDATA%\ditch-windows\config.yaml` | No | No — recreate it |
| Local manifest copy | `%APPDATA%\ditch-windows\backups\<backup-id>\manifest.json` | No | No — a copy is in the repository |
| Logs | `%APPDATA%\ditch-windows\logs\` | No | No |
| Temporary staging | `%APPDATA%\ditch-windows\tmp\` | Mostly no; see Wi-Fi below | No |
| Restic cache | `%LOCALAPPDATA%\restic\` *(restic default)* | No | No — performance cache only |
| Docker demo | `/demo/{home,repo,state,tmp}` | Synthetic only | No |

The default repository path is a **suggestion**, not a hidden second copy. The path that
matters is the one you pass to:

```powershell
ditch-windows config init --repo "$env:USERPROFILE\ditch-windows-repo"
```

or override per command with `--repo <path-or-url>`. `config show` prints the configured
repository:

```powershell
ditch-windows config show
```

If you choose S3 or rclone, the repository is remote and there is no local backup copy.

## What is inside the repository

A restic repository is an encrypted, deduplicated object store. ditch-windows writes:

1. **One metadata snapshot per backup**, tagged
   `kind=metadata,tool=ditch-windows,backup=<backup-id>`. It contains `manifest.json`
   and the module metadata staged by the runner.
2. **Module-owned snapshots** for modules that back up payloads directly. For example,
   the files module snapshots your selected folders, and the Wi-Fi module snapshots its
   exported XML so plaintext keys never land in unencrypted staging.
3. **Manifest references** to every snapshot ID created for that backup.

Restic encrypts and authenticates all of it. `ditch-windows verify` checks the
repository, the manifest hashes, and the modules that can be verified locally after
restoring metadata to a temporary directory.

## What is inside the local app directory

### `config.yaml`

Created by `ditch-windows config init --repo ...`. It contains:

- `repo_url` — the restic repository path or URL.
- `restic_path` — optional pinned restic executable.
- `default_profile` — normally `daily`.
- `profiles` — module lists and include/exclude globs.
- `conflict_mode` — restore behavior; defaults to `overwrite_keep_bak`.
- `ask_per_item` and `verify_after_backup` — optional behavior flags.

It does **not** contain the restic passphrase.

### `backups\<backup-id>\manifest.json`

A human-readable convenience copy of the manifest from the encrypted metadata snapshot.
It includes the tool version, host/user, Windows build, timestamps, module statuses,
SHA-256 hashes of module metadata, and snapshot IDs. It does **not** contain file contents
or Wi-Fi keys.

Losing this folder does not lose the backup. The authoritative manifest is in restic.
The local copy exists so you can inspect the last run without unlocking the repository.

### `logs\`

Daily log files. They are useful for troubleshooting. Command output is sanitized for
newlines and obvious secrets, but logs are not encrypted. Review them before sharing.

### `tmp\`

Private staging area created with restrictive permissions. During a backup, modules
write metadata here, the runner hashes it, and restic snapshots it. During restore and
verify, metadata is staged here and then removed. The directory is cleaned on a
best-effort basis.

Wi-Fi exports are the exception: their XML contains plaintext keys, so the Wi-Fi module
uses a mode-0700 temporary directory, snapshots it directly into restic, and deletes it
immediately. It never writes Wi-Fi XML into the unencrypted metadata staging tree or
logs.

### Restic cache

restic keeps a local cache for performance. On Windows its default is normally
`%LOCALAPPDATA%\restic`. It is not a backup and can be deleted; restic rebuilds it from
the repository when needed.

## Backup and restore lifecycle

### Backup

```text
Modules scan the machine
        |
        v
Metadata + module payloads staged in %APPDATA%\ditch-windows\tmp\
        |
        v
Hashes recorded in manifest.json
        |
        v
restic snapshots staging directory (encrypted, deduplicated)
        |
        v
Manifest copy saved under %APPDATA%\ditch-windows\backups\<id>\
        |
        v
Staging directory deleted
```

Each run has a sortable backup ID such as `20261025T102320Z-cc9464`.

### Restore

```text
Select the newest backup, or pass a backup ID as the first argument
        |
        v
Download metadata snapshot into the private temp directory
        |
        v
Build preview: modules, item counts, file counts, conflicts
        |
        +--> dry run (default): stop here
        |
        v
Apply: each module restores using the configured conflict policy
        |
        v
Existing files kept as <name>.bak or <name>.bak.N
        |
        v
Temporary metadata deleted
```

`restore` is preview-only by default. `restore --apply` writes. Under the default
`overwrite_keep_bak` policy, an existing file is renamed to a unique `.bak` name before
the restored file is written.

## Choosing and moving the repository

The repository can be any restic-supported destination:

```text
Local folder        D:\backups\ditch-windows
USB drive           usb:E:
S3                  s3:s3.amazonaws.com/my-bucket/ditch-windows
S3 URL form         s3://my-bucket/ditch-windows
rclone remote       rclone:gdrive:ditch-windows
```

To move a local repository, copy the **entire** repository directory while no backup is
running. Keep the passphrase separately. Do not copy only `%APPDATA%\ditch-windows`; that
is local state, not the backup.

## Deleting data

| What you delete | Result |
| --- | --- |
| `%APPDATA%\ditch-windows\logs` | Logs only; backups unaffected |
| `%APPDATA%\ditch-windows\backups` | Local manifest convenience copies; backups unaffected |
| `%APPDATA%\ditch-windows\tmp` | Temporary files; safe when no operation is running |
| `%APPDATA%\ditch-windows\config.yaml` | Configuration; recreate with `config init` |
| The restic repository | **The backup is gone** |
| The restic passphrase | **The backup is unrecoverable** |

Use restic's normal retention tools to prune old snapshots:

```powershell
restic -r "$env:USERPROFILE\ditch-windows-repo" forget --keep-last 10 --prune
```

Set `RESTIC_PASSWORD` in the environment (or use restic's password file support) when
running restic directly.

## Docker demo storage

The demo is deliberately self-contained:

| Path inside container | Contents |
| --- | --- |
| `/demo/home` | Synthetic Documents, Desktop, Pictures, and Downloads |
| `/demo/repo` | Real restic repository containing synthetic data |
| `/demo/state` | Demo config, logs, and local manifests (`DITCH_WINDOWS_HOME`) |
| `/demo/tmp` | Demo staging and secure Wi-Fi temp files |
| `/home/demo/.cache/restic` | Restic cache (disposable, not a backup) |

`docker run --rm` deletes all five when the container exits. The documented commands do
not mount a host directory. The published passphrase `ditch-demo-only` protects synthetic
data only and must never be reused.

See [DOCKER.md](DOCKER.md) for the demo safety model and commands.

## Portable state and tests

`DITCH_WINDOWS_HOME` overrides `%APPDATA%\ditch-windows` for the config, logs, manifests,
and temp root. This is useful for portable installs, isolated tests, and the Docker demo:

```powershell
$env:DITCH_WINDOWS_HOME = "D:\portable\ditch-windows-state"
```

The restic repository is still controlled separately by `repo_url` / `--repo`.

## Quick reference

```text
Encrypted backup       repo_url (default suggestion: %USERPROFILE%\ditch-windows-repo)
Config                 %APPDATA%\ditch-windows\config.yaml
Logs                   %APPDATA%\ditch-windows\logs\
Local manifests        %APPDATA%\ditch-windows\backups\<backup-id>\manifest.json
Temp/staging           %APPDATA%\ditch-windows\tmp\
Passphrase             not stored; RESTIC_PASSWORD or interactive prompt
Docker demo            /demo, disposable
```

If you remember one thing: **the restic repository and its passphrase are the backup.
Everything under `%APPDATA%\ditch-windows` is convenience state that can be recreated.**
