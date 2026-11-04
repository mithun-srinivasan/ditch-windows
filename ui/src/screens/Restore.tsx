import { useCallback, useEffect, useState } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { conflictLabel, formatBytes, formatDuration, formatTime, plural, shortId, truncate } from "../format"
import {
  EmptyState,
  HLine,
  InfoRow,
  KeyBar,
  Modal,
  Panel,
  Spinner,
  Tag,
  statusGlyph,
  statusTone,
} from "../components/primitives"
import type { ApplyResult, PreviewResult } from "../types"

type Stage = "pick" | "preview" | "confirm" | "running" | "done"

export function Restore() {
  const {
    client,
    status,
    backups,
    backupsLoading,
    restoreTarget,
    setRestoreTarget,
    registerKeys,
    startProgress,
    patchModule,
    patchProgress,
    progress,
    setBusy,
    setProgress,
    refreshBackups,
    notify,
    go,
  } = useApp()

  const [stage, setStage] = useState<Stage>("pick")
  const [cursor, setCursor] = useState(0)
  const [preview, setPreview] = useState<PreviewResult | null>(null)
  const [result, setResult] = useState<ApplyResult | null>(null)

  useEffect(() => {
    if (!restoreTarget) return
    const index = backups.findIndex((backup) => backup.id === restoreTarget)
    if (index >= 0) {
      setCursor(index)
      setRestoreTarget(null)
    }
  }, [backups, restoreTarget, setRestoreTarget])

  const openPreview = useCallback(
    async (backupId: string) => {
      setStage("preview")
      setPreview(null)
      try {
        const data = await client.request<PreviewResult>("preview", { backupId })
        setPreview(data)
      } catch (error) {
        setStage("pick")
        notify(error instanceof Error ? error.message : String(error), "red")
      }
    },
    [client, notify],
  )

  const apply = useCallback(async () => {
    if (!preview) return
    setStage("running")
    startProgress(
      "restore",
      "Rebuilding from snapshot",
      preview.modules.filter((module) => module.available).map((module) => module.name),
    )
    try {
      const data = await client.request<ApplyResult>("apply", {
        backupId: preview.backupId,
        conflictMode: status?.conflictMode || "overwrite_keep_bak",
      })
      setResult(data)
      for (const module of data.modules) {
        patchModule(module.name, {
          phase: module.success ? "ok" : module.skipped ? "skipped" : "failed",
          error: module.error,
        })
      }
      patchProgress({ finished: true, message: `Restore of ${shortId(data.backupId)} complete` })
      setBusy(false)
      setStage("done")
      await refreshBackups()
    } catch (error) {
      setBusy(false)
      setProgress(null)
      setStage("preview")
      notify(error instanceof Error ? error.message : String(error), "red")
    }
  }, [client, patchModule, patchProgress, preview, refreshBackups, setBusy, setProgress, startProgress, status])

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (stage === "running") return true
      if (stage === "confirm") {
        if (key.name === "return" || key.name === "y") {
          void apply()
          return true
        }
        if (key.name === "escape" || key.name === "n") {
          setStage("preview")
          return true
        }
        return true
      }
      if (stage === "done") {
        if (key.name === "return" || key.name === "l") {
          go("backups")
          return true
        }
        if (key.name === "escape") {
          setStage("pick")
          setResult(null)
          setPreview(null)
          setProgress(null)
          return true
        }
        return false
      }
      if (stage === "preview") {
        if (key.name === "return") {
          setStage("confirm")
          return true
        }
        if (key.name === "escape") {
          setStage("pick")
          setPreview(null)
          return true
        }
        return false
      }
      if (key.name === "j" || key.name === "down") {
        setCursor((index) => (backups.length ? (index + 1) % backups.length : 0))
        return true
      }
      if (key.name === "k" || key.name === "up") {
        setCursor((index) => (backups.length ? (index - 1 + backups.length) % backups.length : 0))
        return true
      }
      if (key.name === "return") {
        const backup = backups[cursor]
        if (backup) void openPreview(backup.id)
        return true
      }
      if (key.name === "escape") {
        go("dashboard")
        return true
      }
      return false
    },
    [apply, backups, cursor, go, openPreview, setProgress, stage],
  )

  useEffect(() => {
    registerKeys(handleKey)
    return () => registerKeys(null)
  }, [registerKeys, handleKey])

  if (!status) return null

  if (stage === "done" && result) {
    const modules = result.modules ?? []
    const baks = result.baks ?? []
    const failed = modules.filter((module) => !module.success && !module.skipped).length
    return (
      <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
        <box flexDirection="row" gap={1} flexShrink={0}>
          <Tag tone={failed ? "yellow" : "green"} filled>
            {failed ? "RESTORE COMPLETED WITH ISSUES" : "MACHINE REBUILT"}
          </Tag>
          <Tag tone="muted">{formatDuration(result.durationMs)}</Tag>
          <Tag tone="mint">{plural(baks.length, "backup copy", "backup copies")}</Tag>
        </box>
        <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
          <Panel title="RESTORE REPORT" flexGrow={2}>
            <InfoRow label="SNAPSHOT" value={shortId(result.backupId)} tone="accent" />
            <InfoRow label="CONFLICTS" value={conflictLabel(status.conflictMode)} />
            <InfoRow label="FILES BYPASSED" value={result.dryRun ? "dry run" : "none"} />
            <box height={1} />
            {modules.map((module) => (
              <box key={module.name} flexDirection="row" height={2} paddingX={1} backgroundColor={theme.surfaceAlt}>
                <box width={3}>
                  <text fg={statusTone(module.success ? "ok" : module.skipped ? "skipped" : "failed")}>
                    {statusGlyph(module.success ? "ok" : module.skipped ? "skipped" : "failed")}
                  </text>
                </box>
                <box width={18}>
                  <text fg={theme.text}>{module.name}</text>
                </box>
                <box flexGrow={1}>
                  <text fg={theme.dim}>{module.error || (module.skipped ? "skipped" : "restored")}</text>
                </box>
                <box width={8}>
                  <text fg={theme.dim}>{module.durationMs ? `${module.durationMs}ms` : "—"}</text>
                </box>
              </box>
            ))}
          </Panel>
          <Panel title=".BAK COPIES" width={42} flexShrink={0}>
            {baks.length ? (
              <box flexDirection="column" gap={0}>
                <text fg={theme.dim}>Existing files were preserved before overwrite.</text>
                <box height={1} />
                {baks.slice(0, 16).map((path) => (
                  <text key={path} fg={theme.muted} wrapMode="none">
                    {path}
                  </text>
                ))}
                {baks.length > 16 ? <text fg={theme.dim}>+ {baks.length - 16} more</text> : null}
              </box>
            ) : (
              <EmptyState title="No conflicts encountered" hint="Nothing needed a .bak copy." />
            )}
          </Panel>
        </box>
        <KeyBar
          hints={[
            ["enter", "browse archive"],
            ["esc", "choose another snapshot"],
          ]}
        />
      </box>
    )
  }

  if (stage === "running") {
    return (
      <Panel title="RESTORE IN PROGRESS" right={progress?.message} flexGrow={1}>
        <box flexDirection="row" gap={1} alignItems="center" height={2}>
          <Spinner color={theme.mint} />
          <text fg={theme.text}>Applying snapshot to {status.home}</text>
        </box>
        <box height={1} />
        {(progress?.modules || []).map((module) => (
          <box key={module.name} flexDirection="row" height={2} paddingX={1}>
            <box width={4}>
              <text fg={statusTone(module.phase)}>{statusGlyph(module.phase)}</text>
            </box>
            <box width={18}>
              <text fg={module.phase === "queued" ? theme.dim : theme.text}>{module.name}</text>
            </box>
            <box width={12}>
              <text fg={statusTone(module.phase)}>{module.phase}</text>
            </box>
            <box flexGrow={1}>
              <text fg={theme.dim}>{module.error || module.details || ""}</text>
            </box>
          </box>
        ))}
        <box flexGrow={1} />
        <text fg={theme.dim}>Files that already exist are handled by the configured conflict policy.</text>
      </Panel>
    )
  }

  if (stage === "pick") {
    return (
      <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
        <Panel title="CHOOSE A SNAPSHOT" right={backupsLoading ? "loading…" : plural(backups.length, "snapshot")} flexGrow={2}>
          {backups.length === 0 ? (
            <EmptyState title="The archive is empty" hint="Capture a backup before rebuilding." />
          ) : (
            <box flexDirection="column">
              {backups.map((backup, index) => {
                const active = index === cursor
                return (
                  <box
                    key={backup.id}
                    flexDirection="row"
                    height={2}
                    paddingX={1}
                    backgroundColor={active ? theme.surfaceAlt : theme.surface}
                    onMouseDown={() => setCursor(index)}
                  >
                    <box width={2}>
                      <text fg={active ? theme.accent : theme.dim}>{active ? "▌" : " "}</text>
                    </box>
                    <box flexDirection="column" flexGrow={1}>
                      <text fg={active ? theme.text : theme.muted}>{shortId(backup.id)}</text>
                      <text fg={theme.dim}>
                        {formatTime(backup.time)} · {backup.hostname}
                      </text>
                    </box>
                    <text fg={active ? theme.accent : theme.dim}>→</text>
                  </box>
                )
              })}
            </box>
          )}
          <text fg={theme.dim}>enter preview · j/k move · esc dashboard</text>
        </Panel>
        <Panel title="BEFORE YOU RESTORE" width={44} flexShrink={0}>
          <box flexDirection="column" gap={1}>
            <text fg={theme.text} attributes={1}>
              Read-only until you confirm.
            </text>
            <text fg={theme.muted} wrapMode="word">
              The preview inspects the snapshot and reports what would be written, how many files conflict, and where
              elevation is required. Nothing is modified during preview.
            </text>
            <HLine />
            <InfoRow label="POLICY" value={conflictLabel(status.conflictMode)} tone="accent" />
            <InfoRow label="TARGET HOME" value={status.demo ? "demo sandbox" : status.home} />
            <InfoRow label="PLATFORM" value={status.platform} />
            <InfoRow label="ELEVATED" value={status.elevated ? "yes" : "no"} tone={status.elevated ? "blue" : "yellow"} />
            <box height={1} />
            <text fg={theme.dim} wrapMode="word">
              {status.demo
                ? "Demo mode restores into the sandbox home, never the real system."
                : "Run ditch-windows from an elevated terminal when restoring machine-level modules such as winget or the registry."}
            </text>
          </box>
        </Panel>
      </box>
    )
  }

  if (!preview) {
    return (
      <Panel title="PREVIEWING SNAPSHOT" flexGrow={1}>
        <box flexGrow={1} justifyContent="center" alignItems="center" gap={1} flexDirection="column">
          <Spinner />
          <text fg={theme.muted}>inspecting manifest and conflicts…</text>
        </box>
      </Panel>
    )
  }

  const available = preview.modules.filter((module) => module.available)

  return (
    <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
      <box flexDirection="row" gap={1} flexShrink={0}>
        <Tag tone="accent">PREVIEW</Tag>
        <Tag tone="muted">{formatTime(preview.time)}</Tag>
        <Tag tone="mint">{plural(preview.files, "file")}</Tag>
        <Tag tone="muted">{formatBytes(preview.bytes)}</Tag>
        <Tag tone={preview.conflicts ? "yellow" : "green"}>{plural(preview.conflicts, "conflict")}</Tag>
      </box>
      <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
        <Panel title="RESTORE PLAN" right={shortId(preview.backupId)} flexGrow={2}>
          <box flexDirection="row" height={1} paddingX={1}>
            <box width={3}>
              <text fg={theme.dim}> </text>
            </box>
            <box width={18}>
              <text fg={theme.dim}>MODULE</text>
            </box>
            <box width={10}>
              <text fg={theme.dim}>STATE</text>
            </box>
            <box flexGrow={1}>
              <text fg={theme.dim}>SCOPE</text>
            </box>
            <box width={10}>
              <text fg={theme.dim}>FILES</text>
            </box>
            <box width={9}>
              <text fg={theme.dim}>CONFLICT</text>
            </box>
          </box>
          {preview.modules.map((module) => (
            <box key={module.name} flexDirection="row" height={2} paddingX={1} backgroundColor={theme.surfaceAlt}>
              <box width={3}>
                <text fg={module.available ? theme.green : theme.dim}>{module.available ? "●" : "○"}</text>
              </box>
              <box width={18}>
                <text fg={module.available ? theme.text : theme.dim}>{module.name}</text>
              </box>
              <box width={10}>
                <text fg={module.available ? theme.mint : theme.dim}>{module.available ? "ready" : "missing"}</text>
              </box>
              <box flexGrow={1}>
                <text fg={theme.dim} wrapMode="none">
                  {truncate(module.error || module.details || `${module.items} items`, 18)}
                </text>
              </box>
              <box width={10}>
                <text fg={theme.muted}>{module.files || "—"}</text>
              </box>
              <box width={9}>
                <text fg={module.conflicts ? theme.yellow : theme.dim}>{module.conflicts || "—"}</text>
              </box>
            </box>
          ))}
        </Panel>
        <Panel title="DECISION" width={42} flexShrink={0}>
          <box flexDirection="column" gap={1} flexGrow={1}>
            <InfoRow label="MODULES" value={`${available.length} ready`} tone="green" />
            <InfoRow label="CONFLICTS" value={String(preview.conflicts)} tone={preview.conflicts ? "yellow" : "green"} />
            <InfoRow label="POLICY" value={conflictLabel(status.conflictMode)} tone="accent" />
            <InfoRow label="ELEVATION" value={available.some((m) => m.needsAdmin && !m.elevated) ? "required" : "ok"} />
            <HLine />
            <text fg={theme.muted} wrapMode="word">
              {preview.conflicts > 0
                ? `Existing files will follow "${conflictLabel(status.conflictMode)}". With keep .bak, originals are renamed before the snapshot is written.`
                : "No existing files collide with this snapshot. The restore writes cleanly."}
            </text>
            <box flexGrow={1} />
            <text fg={theme.accent}>enter to confirm · esc to go back</text>
          </box>
        </Panel>
      </box>
      {stage === "confirm" ? (
        <Modal
          title="CONFIRM RESTORE"
          borderColor={theme.yellow}
          footer={
            <box flexDirection="row" gap={2}>
              <text>
                <span fg={theme.green} attributes={1}>
                  enter / y
                </span>
                <span fg={theme.muted}> rebuild</span>
              </text>
              <text>
                <span fg={theme.red} attributes={1}>
                  esc / n
                </span>
                <span fg={theme.muted}> cancel</span>
              </text>
            </box>
          }
        >
          <text fg={theme.text} attributes={1}>
            Write {plural(preview.files, "file")} to {status.home}?
          </text>
          <text fg={theme.muted} wrapMode="word">
            Snapshot {shortId(preview.backupId)} from {formatTime(preview.time)}. {preview.conflicts} existing file
            {preview.conflicts === 1 ? "" : "s"} will be handled by the "{conflictLabel(status.conflictMode)}" policy.
          </text>
          <text fg={theme.dim} wrapMode="word">
            This is the point of no return for the target files. A .bak copy is kept for overwritten files, but the
            restore itself is not rolled back automatically.
          </text>
        </Modal>
      ) : null}
    </box>
  )
}
