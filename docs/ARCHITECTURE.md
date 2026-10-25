# Architecture

ditch-windows is a Go command-line application that orchestrates Windows' own tools and
[restic](https://restic.net/). It intentionally does not implement backup storage,
deduplication, encryption, or Windows inventory APIs itself.

## Design goals

1. **Rebuild, not just back up files.** Capture applications, Wi-Fi, fonts, theme, default
   apps, and selected personal folders in one workflow.
2. **Safe by default.** Restore is a preview unless `--apply` is explicit. Existing files
   are preserved as `.bak` by default.
3. **Inspectable and portable.** A versioned JSON manifest plus a standard restic
   repository. No proprietary server or database.
4. **Use the platform.** `winget`, `netsh`, `dism`, the registry, VSS, and UAC are driven
   as subprocesses or narrow allowlisted APIs, not reimplemented.
5. **Test without Windows.** Command execution and storage sit behind interfaces, so unit
   tests run on Linux and in CI.

## Component map

```text
                         +-----------------------+
                         |   cmd/ditch-windows   |
                         |  CLI, config, flags   |
                         +-----------+-----------+
                                     |
             +-----------------------+-----------------------+
             |                                               |
   +---------v----------+                          +---------v---------+
   |    internal/tui    |                          | internal/storage  |
   |  Docker demo TUI   |                          | repo URL parsing  |
   +---------+----------+                          +---------+---------+
             |                                               |
             +-----------------------+-----------------------+
                                     |
                         +-----------v-----------+
                         |     internal/core     |
                         | Runner, manifest,     |
                         | config, conflicts,    |
                         | elevation, hashing    |
                         +-----+-----------+-----+
                               |           |
              +----------------+           +------------------+
              |                                                 |
   +----------v-----------+                          +----------v---------+
   |  internal/module     |                          |  internal/restic   |
   | winget, default-apps |                          |  restic subprocess |
   | wifi, fonts, theme,  |                          |  wrapper           |
   | files                |                          +----------+---------+
   +----------+-----------+                                     |
              |                                                 |
   +----------v-----------+                          +----------v---------+
   |  internal/platform   |                          |  restic repository |
   | registry, fonts,     |                          |  local/USB/S3/     |
   | theme providers      |                          |  rclone            |
   +----------------------+                          +--------------------+
```

### `cmd/ditch-windows`

Parses commands (`backup`, `list`, `restore`, `verify`, `config`, `demo`, `version`),
loads config, resolves the repository, opens the logger, and constructs the runner. The
interactive menu is currently plain text; the full-screen bubbletea TUI is implemented
for the isolated Docker demo and is being extended to the Windows flow.

### `internal/core`

The orchestration layer:

- `Runner` selects modules and executes backup, preview, restore, and verify.
- `Manifest` is the versioned JSON record written per backup.
- Config and profile validation.
- Conflict policy (`.bak`, skip, per-item prompt).
- SHA-256 hashing of module metadata.
- Elevation and host information.
- Filesystem paths, logging, and atomic writes.

### `internal/module`

One adapter per capture domain. All modules implement the same contract:

```go
type Module interface {
    Name() string
    Description() string
    NeedsAdmin() bool
    Scan(ctx context.Context) (Summary, error)
    Backup(ctx context.Context, dir string) error
    Restore(ctx context.Context, dir string, opts RestoreOpts) error
    Verify(ctx context.Context, dir string) error
}
```

- `Scan` must be cheap and side-effect free.
- `Backup` writes metadata into the staging directory.
- `Restore` applies metadata from a verified metadata snapshot.
- `Verify` checks the module's staged metadata.

A module may also implement optional interfaces:

- `SnapshotProvider` — returns snapshot IDs created directly against the repository, used
  by the files and Wi-Fi modules for payloads that should not sit in unencrypted staging.
- `Previewer` — adds item/file/byte/conflict counts to the restore preview.

### `internal/restic`

A thin subprocess wrapper around restic. It builds arguments, runs commands with context
cancellation, parses JSON output, and exposes a small `Repository` interface:

```go
Init, Exists, Backup, Restore, List, Ls, Check
```

The wrapper is replaceable with an in-memory fake in tests. It is not a restic
reimplementation.

### `internal/platform`

Windows-specific providers for the registry, fonts, and theme. Registry access is
allowlisted to the values needed for wallpaper, theme, accent, dark mode, and font
registration. Whole hives are never exported or restored.

### `internal/tui`

The bubbletea/lipgloss full-screen demo UI. It talks to the demo through a small action
interface in `internal/tui/demo.go`; the adapter in `cmd/ditch-windows/demo_tui.go` maps
those actions to the real runner. The same visual language can host the Windows screens.

### `internal/demo`

Synthetic modules and fixtures for the Docker image. `apps`, `wifi`, and `theme` are
clearly labeled fakes. The files module and restic are real.

## Backup flow

```text
1. Resolve config, profile, repository, and passphrase.
2. Create a private staging directory in %APPDATA%\ditch-windows\tmp
   (or the configured TempDir).
3. For each selected module:
     a. Check elevation if the module requires it.
     b. Scan to report item counts without side effects.
     c. Backup metadata into staging\modules\<module>\.
     d. Hash module metadata with SHA-256.
     e. If the module owns a payload snapshot, record its snapshot IDs.
4. Fill in host, Windows build, timestamps, durations, and statuses.
5. Write manifest.json into staging.
6. restic snapshots staging as the metadata snapshot, tagged:
     kind=metadata, tool=ditch-windows, backup=<backup-id>
7. Append the metadata snapshot ID to the manifest, write a local copy under
   %APPDATA%\ditch-windows\backups\<backup-id>\manifest.json, and remove staging.
```

A module failure does not abort the whole backup by default. The manifest records the
failure, the backup is marked partial, and the command returns an error after the
successful modules have been stored.

## Restore flow

```text
1. Select the newest metadata snapshot, or the requested backup ID.
2. Restore metadata into a private staging directory.
3. Find and parse manifest.json.
4. Build a preview: module availability, admin requirements, item counts,
   file counts, bytes, and conflicts.
5. If dry run (the default), stop and print the preview.
6. If applying:
     a. Ask for confirmation in the CLI.
     b. Relaunch elevated if a selected module needs it.
     c. Call each module's Restore with the conflict policy.
     d. Preserve existing files as <name>.bak or <name>.bak.N.
7. Delete the metadata staging directory.
```

The GUI/TUI path uses the same runner and the same preview-then-apply rule. The Docker
TUI additionally refuses to apply unless a preview for the current backup exists.

## Data and integrity model

- **Repository:** the source of truth. Encrypted by restic, deduplicated, and portable.
- **Metadata snapshot:** one per backup, contains `manifest.json` and module metadata.
- **Module snapshots:** optional payload snapshots owned by modules (files, Wi-Fi).
- **Local manifest:** plaintext convenience copy for inspection. Not required to restore.
- **Hashes:** SHA-256 over module metadata. `verify` checks repo integrity, manifest
  hashes, and module verification.
- **Backup IDs:** UTC timestamp plus random suffix, for example
  `20261025T102320Z-cc9464`.

See [STORAGE.md](STORAGE.md) for exact paths and lifecycle details.

## Safety invariants

- A restore is a preview unless apply is explicit.
- Default conflict behavior renames existing files to a unique `.bak` name.
- Paths are resolved against the user's home and validated; config include paths cannot
  be absolute or traverse upwards.
- Wi-Fi XML with plaintext keys never enters unencrypted staging or logs; it is snapshotted
  directly into restic and then deleted.
- Registry access is allowlisted; whole hives are never restored.
- Admin-only modules are skipped or require explicit elevation; they are never silently
  run elevated.
- The Docker demo does not mount the host profile and uses synthetic data.
- The restic passphrase is never written to config.

## Cross-platform strategy

The application is Windows-first for capture and restore, but the orchestration and
storage layers compile and test on Linux. Platform-specific behavior is injected through
interfaces:

- command execution (`Exec`),
- repository operations (`Repository`),
- registry/font/theme providers (`internal/platform`),
- demo modules (`internal/demo`).

This is what makes the Docker demo possible without pretending Linux is Windows.

## Extending with a module

1. Implement `core.Module` in `internal/module`.
2. Register it in the module registry used by `cmd/ditch-windows`.
3. Add its name to `core.ModuleNames` and to the default profiles if appropriate.
4. Implement `Previewer` if restore conflicts can be counted.
5. Implement `SnapshotProvider` instead of writing secrets into staging if the payload
   must go directly to restic.
6. Add tests with a fake `Exec` and fake `Repository`. Do not require real `winget`,
   `netsh`, `dism`, or registry access in unit tests.
7. Document its admin requirements and what it does not capture.

## Why restic

restic provides authenticated encryption, content-defined chunking, deduplication,
compression, snapshot pruning, and multiple backends. Reimplementing those correctly is a
large security project on its own. ditch-windows focuses on the Windows capture/restore
semantics and treats restic as the storage engine.

## Related docs

- [Storage and data lifecycle](STORAGE.md)
- [Docker demo](DOCKER.md)
- [Main README](../README.md)
