import { useCallback, useEffect, useState } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { formatBytes, formatDuration, plural, shortId } from "../format"
import { EmptyState, InfoRow, KeyBar, Panel, ProgressBar, Spinner, Tag, statusGlyph, statusTone } from "../components/primitives"
import type { BackupResult } from "../types"

type Stage = "pick" | "running" | "done"

export function Backup() {
  const {
    client,
    status,
    registerKeys,
    startProgress,
    patchProgress,
    patchModule,
    progress,
    busy,
    setBusy,
    setProgress,
    refreshBackups,
    notify,
    go,
  } = useApp()

  const [stage, setStage] = useState<Stage>("pick")
  const [selected, setSelected] = useState<string[]>(() => {
    if (!status) return []
    return [...(status.profiles[status.defaultProfile]?.modules || [])]
  })
  const [cursor, setCursor] = useState(0)
  const [result, setResult] = useState<BackupResult | null>(null)

  const modules = status?.modules || []

  const toggle = useCallback(
    (name: string) => {
      setSelected((current) =>
        current.includes(name) ? current.filter((item) => item !== name) : [...current, name],
      )
    },
    [],
  )

  const run = useCallback(async () => {
    if (!status) return
    if (selected.length === 0) {
      notify("Select at least one module to capture.", "yellow")
      return
    }
    const ordered = modules.filter((module) => selected.includes(module.name)).map((module) => module.name)
    setStage("running")
    setResult(null)
    startProgress("backup", "Capturing modules", ordered)
    try {
      const data = await client.request<BackupResult>("backup", { modules: ordered })
      setResult(data)
      for (const module of data.modules) {
        patchModule(module.name, {
          phase: module.status === "ok" ? "ok" : module.status === "skipped" ? "skipped" : "failed",
          items: module.items,
          details: module.details,
          error: module.error,
        })
      }
      patchProgress({
        finished: true,
        message: data.partial ? "Backup finished with warnings" : `Snapshot ${shortId(data.backupId)} captured`,
      })
      setBusy(false)
      setStage("done")
      await refreshBackups()
    } catch (error) {
      setBusy(false)
      setProgress(null)
      setStage("pick")
      notify(error instanceof Error ? error.message : String(error), "red")
    }
  }, [
    client,
    modules,
    notify,
    patchModule,
    patchProgress,
    refreshBackups,
    selected,
    setBusy,
    setProgress,
    startProgress,
    status,
  ])

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (stage === "running") return true
      if (stage === "done") {
        if (key.name === "return" || key.name === "l") {
          go("backups")
          return true
        }
        if (key.name === "escape") {
          setStage("pick")
          setResult(null)
          setProgress(null)
          return true
        }
        return false
      }
      if (key.name === "j" || key.name === "down") {
        setCursor((index) => (modules.length ? (index + 1) % modules.length : 0))
        return true
      }
      if (key.name === "k" || key.name === "up") {
        setCursor((index) => (modules.length ? (index - 1 + modules.length) % modules.length : 0))
        return true
      }
      if (key.name === "space") {
        const module = modules[cursor]
        if (module) toggle(module.name)
        return true
      }
      if (key.name === "a") {
        setSelected(modules.map((module) => module.name))
        return true
      }
      if (key.name === "n") {
        setSelected([])
        return true
      }
      if (key.name === "return") {
        void run()
        return true
      }
      if (key.name === "escape") {
        go("dashboard")
        return true
      }
      return false
    },
    [cursor, go, modules, run, setProgress, stage, toggle],
  )

  useEffect(() => {
    registerKeys(handleKey)
    return () => registerKeys(null)
  }, [registerKeys, handleKey])

  if (!status) return null

  if (stage === "done" && result) {
    const modules = result.modules ?? []
    return (
      <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
        <box flexDirection="row" gap={1} flexShrink={0}>
          <Tag tone={result.partial ? "yellow" : "green"} filled>
            {result.partial ? "CAPTURED WITH WARNINGS" : "SNAPSHOT CAPTURED"}
          </Tag>
          <Tag tone="muted">{formatDuration(result.durationMs)}</Tag>
          <Tag tone="mint">{plural(result.snapshots, "snapshot")}</Tag>
        </box>
        <Panel title="BACKUP RECORD" right={shortId(result.backupId)} flexGrow={1}>
          <box flexDirection="column" gap={0}>
            <InfoRow label="ID" value={result.backupId} tone="accent" />
            <InfoRow label="MODULES" value={`${modules.filter((m) => m.status === "ok").length}/${modules.length} captured`} />
            <InfoRow label="DURATION" value={formatDuration(result.durationMs)} />
            {result.error ? <InfoRow label="WARNING" value={result.error} tone="yellow" /> : null}
            <box height={1} />
            <box flexDirection="row" height={1} paddingX={1}>
              <box width={18}>
                <text fg={theme.dim}>MODULE</text>
              </box>
              <box flexGrow={1}>
                <text fg={theme.dim}>RESULT</text>
              </box>
              <box width={14}>
                <text fg={theme.dim}>ITEMS</text>
              </box>
              <box width={10}>
                <text fg={theme.dim}>TIME</text>
              </box>
            </box>
            {modules.map((module) => (
              <box key={module.name} flexDirection="row" height={2} paddingX={1} backgroundColor={theme.surfaceAlt}>
                <box width={18}>
                  <text fg={theme.text}>{module.name}</text>
                </box>
                <box flexGrow={1}>
                  <text fg={statusTone(module.status)}>
                    {statusGlyph(module.status)} {module.status}
                    {module.error ? ` · ${module.error}` : module.details ? ` · ${module.details}` : ""}
                  </text>
                </box>
                <box width={14}>
                  <text fg={theme.muted}>{module.items}</text>
                </box>
                <box width={10}>
                  <text fg={theme.dim}>{module.durationMs ? `${module.durationMs}ms` : "—"}</text>
                </box>
              </box>
            ))}
          </box>
        </Panel>
        <KeyBar
          hints={[
            ["enter", "browse archive"],
            ["esc", "back to picker"],
            ["l", "archive"],
          ]}
        />
      </box>
    )
  }

  if (stage === "running") {
    return (
      <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
        <Panel title="CAPTURE IN PROGRESS" right={progress?.message} flexGrow={1}>
          <box flexDirection="row" gap={1} alignItems="center" height={2}>
            <Spinner />
            <text fg={theme.text}>{progress?.label || "Capturing modules"}</text>
            <text fg={theme.dim}>·</text>
            <text fg={theme.muted}>{status.repo}</text>
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
              <box width={14}>
                <text fg={statusTone(module.phase)}>{module.phase}</text>
              </box>
              <box flexGrow={1}>
                <text fg={theme.dim}>{module.error || module.details || module.items || ""}</text>
              </box>
            </box>
          ))}
          <box flexGrow={1} />
          <text fg={theme.dim}>Restic is writing content-addressed chunks. Keep this window open.</text>
        </Panel>
        <box flexDirection="row" gap={1}>
          <Tag tone="accent">DO NOT INTERRUPT</Tag>
          <text fg={theme.dim}>encrypted transport · deduplicated storage</text>
        </box>
      </box>
    )
  }

  return (
    <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
      <Panel title="SELECT MODULES" right={`${selected.length} selected`} flexGrow={2}>
        <box flexDirection="column" flexGrow={1}>
          {modules.map((module, index) => {
            const on = selected.includes(module.name)
            const active = index === cursor
            return (
              <box
                key={module.name}
                flexDirection="row"
                height={2}
                paddingX={1}
                backgroundColor={active ? theme.surfaceAlt : theme.surface}
                onMouseDown={() => {
                  setCursor(index)
                  toggle(module.name)
                }}
              >
                <box width={2}>
                  <text fg={active ? theme.accent : theme.dim}>{active ? "▌" : " "}</text>
                </box>
                <box width={3}>
                  <text fg={on ? theme.green : theme.dim}>{on ? "◼" : "◻"}</text>
                </box>
                <box width={18}>
                  <text fg={on ? theme.text : theme.muted}>{module.name}</text>
                </box>
                <box flexGrow={1}>
                  <text fg={theme.dim} wrapMode="none">
                    {module.description}
                  </text>
                </box>
                {module.needsAdmin ? <Tag tone="yellow">admin</Tag> : null}
              </box>
            )
          })}
        </box>
        <text fg={theme.dim}>j/k move · space toggle · a all · n none · enter capture</text>
      </Panel>

      <Panel title="PLAN" width={40} flexShrink={0}>
        <box flexDirection="column" gap={1} flexGrow={1}>
          <InfoRow label="PROFILE" value={status.defaultProfile} tone="accent" />
          <InfoRow label="MODULES" value={`${selected.length} of ${modules.length}`} />
          <InfoRow label="REPOSITORY" value={status.repoExists ? "ready" : "will initialize"} tone={status.repoExists ? "green" : "yellow"} />
          <InfoRow label="POLICY" value={status.conflictMode} />
          <box height={1} />
          <ProgressBar value={selected.length} total={modules.length} width={22} color={theme.green} />
          <box height={1} />
          <text fg={theme.muted} wrapMode="word">
            A backup reads module state and hands it to restic. Nothing on the system is modified during capture.
          </text>
          <box flexGrow={1} />
          {selected.length === 0 ? (
            <text fg={theme.yellow}>No modules selected — nothing would be captured.</text>
          ) : (
            <EmptyState title="" hint="" />
          )}
          <box height={1} />
          <box flexDirection="row" gap={1}>
            <Tag tone="mint">{formatBytes(0)} preview</Tag>
            <Tag tone={busy ? "accent" : "muted"}>{busy ? "working" : "idle"}</Tag>
          </box>
        </box>
      </Panel>
    </box>
  )
}
