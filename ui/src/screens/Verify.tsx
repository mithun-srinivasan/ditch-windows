import { useCallback, useEffect, useState } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { formatTime, plural, shortId } from "../format"
import { EmptyState, InfoRow, KeyBar, Panel, Spinner, Tag, statusGlyph, statusTone } from "../components/primitives"
import type { VerifyResult } from "../types"

type Stage = "pick" | "running" | "done"

export function Verify() {
  const {
    client,
    status,
    backups,
    restoreTarget,
    setRestoreTarget,
    registerKeys,
    startProgress,
    patchProgress,
    setBusy,
    setProgress,
    notify,
    go,
  } = useApp()

  const [stage, setStage] = useState<Stage>("pick")
  const [cursor, setCursor] = useState(0)
  const [readData, setReadData] = useState(false)
  const [result, setResult] = useState<VerifyResult | null>(null)

  useEffect(() => {
    if (!restoreTarget) return
    const index = backups.findIndex((backup) => backup.id === restoreTarget)
    if (index >= 0) {
      setCursor(index)
      setRestoreTarget(null)
    }
  }, [backups, restoreTarget, setRestoreTarget])

  const run = useCallback(async () => {
    const backup = backups[cursor]
    if (!backup) {
      notify("Select a snapshot to verify.", "yellow")
      return
    }
    setStage("running")
    setResult(null)
    startProgress("verify", `Verifying ${shortId(backup.id)}`, [])
    try {
      const data = await client.request<VerifyResult>("verify", { backupId: backup.id, readData })
      setResult(data)
      patchProgress({
        finished: true,
        message: data.repositoryOk && data.hashOk && data.modules.every((module) => module.verified)
          ? "Snapshot is intact"
          : "Verification found problems",
      })
      setBusy(false)
      setStage("done")
    } catch (error) {
      setBusy(false)
      setProgress(null)
      setStage("pick")
      notify(error instanceof Error ? error.message : String(error), "red")
    }
  }, [backups, client, cursor, notify, patchProgress, readData, setBusy, setProgress, startProgress])

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (stage === "running") return true
      if (stage === "done") {
        if (key.name === "escape" || key.name === "return") {
          setStage("pick")
          setResult(null)
          setProgress(null)
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
      if (key.name === "d") {
        setReadData((value) => !value)
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
    [backups.length, go, run, setProgress, stage],
  )

  useEffect(() => {
    registerKeys(handleKey)
    return () => registerKeys(null)
  }, [registerKeys, handleKey])

  if (!status) return null

  if (stage === "running") {
    return (
      <Panel title="VERIFYING SNAPSHOT" flexGrow={1}>
        <box flexGrow={1} justifyContent="center" alignItems="center" flexDirection="column" gap={1}>
          <Spinner color={theme.mint} />
          <text fg={theme.text}>Checking repository integrity and manifest hashes…</text>
          <text fg={theme.dim}>
            {readData ? "Reading data blobs from the repository." : "Metadata check only. Enable read-data for a full check."}
          </text>
        </box>
      </Panel>
    )
  }

  if (stage === "done" && result) {
    const modules = result.modules ?? []
    const healthy = result.repositoryOk && result.hashOk && modules.every((module) => module.verified)
    return (
      <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
        <box flexDirection="row" gap={1} flexShrink={0}>
          <Tag tone={healthy ? "green" : "red"} filled>
            {healthy ? "VERIFIED" : "PROBLEMS FOUND"}
          </Tag>
          <Tag tone="muted">{shortId(result.backupId)}</Tag>
          <Tag tone="mint">{plural(modules.filter((module) => module.verified).length, "module")} ok</Tag>
        </box>
        <Panel title="INTEGRITY REPORT" flexGrow={1}>
          <CheckRow label="Repository reachable" ok={result.repositoryOk} error={result.repositoryError} />
          <CheckRow label="Manifest hash matches" ok={result.hashOk} error={result.hashError} />
          <box height={1} />
          <text fg={theme.dim} paddingX={1}>
            MODULE CHECKS
          </text>
          {modules.map((module) => (
            <box key={module.name} flexDirection="row" height={2} paddingX={1} backgroundColor={theme.surfaceAlt}>
              <box width={3}>
                <text fg={module.verified ? theme.green : theme.red}>{module.verified ? "●" : "×"}</text>
              </box>
              <box width={18}>
                <text fg={theme.text}>{module.name}</text>
              </box>
              <box flexGrow={1}>
                <text fg={module.error ? theme.red : theme.dim}>{module.error || (module.verified ? "content and metadata match" : "not verified")}</text>
              </box>
            </box>
          ))}
        </Panel>
        <KeyBar hints={[["esc", "verify another"], ["enter", "back"]]} />
      </box>
    )
  }

  return (
    <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
      <Panel title="VERIFY A SNAPSHOT" right={plural(backups.length, "snapshot")} flexGrow={2}>
        {backups.length === 0 ? (
          <EmptyState title="Nothing to verify" hint="Capture a backup first." />
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
                </box>
              )
            })}
          </box>
        )}
        <text fg={theme.dim}>j/k move · enter verify · esc dashboard</text>
      </Panel>
      <Panel title="CHECK OPTIONS" width={42} flexShrink={0}>
        <box flexDirection="column" gap={1} flexGrow={1}>
          <box flexDirection="row" gap={1} alignItems="center">
            <text fg={readData ? theme.green : theme.dim}>{readData ? "◼" : "◻"}</text>
            <text fg={theme.text}>Read repository data</text>
            <Tag tone={readData ? "green" : "muted"}>{readData ? "on" : "off"}</Tag>
          </box>
          <text fg={theme.dim}>Press d to toggle. Full checks read every blob and take longer.</text>
          <box height={1} />
          <InfoRow label="TARGET" value={backups[cursor] ? shortId(backups[cursor].id) : "none"} tone="accent" />
          <InfoRow label="REPOSITORY" value={status.repoExists ? "reachable" : "missing"} tone={status.repoExists ? "green" : "yellow"} />
          <box height={1} />
          <text fg={theme.muted} wrapMode="word">
            Verification checks the restic repository, the manifest hash, and each module's recorded content. Problems
            are reported without modifying the snapshot.
          </text>
          <box flexGrow={1} />
          <text fg={statusTone("muted")}>enter runs the selected check</text>
        </box>
      </Panel>
    </box>
  )
}

function CheckRow({ label, ok, error }: { label: string; ok: boolean; error?: string }) {
  return (
    <box flexDirection="row" height={2} paddingX={1}>
      <box width={3}>
        <text fg={ok ? theme.green : theme.red}>{statusGlyph(ok ? "ok" : "failed")}</text>
      </box>
      <box width={26}>
        <text fg={theme.text}>{label}</text>
      </box>
      <box flexGrow={1}>
        <text fg={ok ? theme.green : theme.red}>{error || (ok ? "pass" : "fail")}</text>
      </box>
    </box>
  )
}
