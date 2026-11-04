import { useCallback, useEffect, useState } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { InfoRow, KeyBar, Panel, Spinner, Tag } from "../components/primitives"
import type { InitResult } from "../types"

export function Onboarding() {
  const { client, status, registerKeys, reloadStatus, notify, quit } = useApp()
  const [repo, setRepo] = useState("")
  const [saving, setSaving] = useState(false)
  const [result, setResult] = useState<InitResult | null>(null)

  const save = useCallback(
    async (value: string) => {
      const trimmed = value.trim()
      if (!trimmed || saving) {
        notify("Enter a repository path first.", "yellow")
        return
      }
      setSaving(true)
      try {
        const data = await client.request<InitResult>("init", { repo: trimmed })
        setResult(data)
        await reloadStatus()
      } catch (error) {
        notify(error instanceof Error ? error.message : String(error), "red")
      } finally {
        setSaving(false)
      }
    },
    [client, notify, reloadStatus, saving],
  )

  useEffect(() => {
    registerKeys(() => true)
    return () => registerKeys(null)
  }, [registerKeys])

  if (result) {
    return (
      <box flexGrow={1} flexDirection="column" justifyContent="center" alignItems="center" gap={1}>
        <Panel title="REPOSITORY SAVED" width={76} flexGrow={0} borderColor={theme.green}>
          <box flexDirection="column" gap={1}>
            <Tag tone="green" filled>
              CONFIG WRITTEN
            </Tag>
            <text fg={theme.text}>{result.message}</text>
            <InfoRow label="REPOSITORY" value={result.repo} tone="accent" />
            <InfoRow label="CONFIG" value={status?.configPath || "—"} />
            <box height={1} />
            <text fg={theme.muted} wrapMode="word">
              Quit and relaunch `ditch-windows tui` so the engine can unlock the repository with your passphrase.
              Restic initializes the repository on the first backup.
            </text>
          </box>
        </Panel>
        <KeyBar hints={[["q", "quit and relaunch"]]} />
      </box>
    )
  }

  return (
    <box flexGrow={1} flexDirection="column" justifyContent="center" alignItems="center" gap={1}>
      <box flexDirection="column" width={78} gap={1}>
        <ascii-font text="DITCH" font="tiny" color={theme.accent} />
        <text fg={theme.text} attributes={1}>
          FIRST RUN · CHOOSE A REPOSITORY
        </text>
        <text fg={theme.muted} wrapMode="word">
          Ditch needs a restic repository before it can capture or rebuild anything. Point it at a local folder, a
          network share, or an S3-compatible URL.
        </text>
        <box height={1} />
        <Panel title="REPOSITORY URL" borderColor={theme.accent}>
          <box flexDirection="column" gap={1}>
            <box flexDirection="row" gap={1} alignItems="center">
              <text fg={theme.accent}>›</text>
              <input
                width={64}
                value={repo}
                placeholder="D:\ditch-backups  or  s3:https://bucket/path"
                focused
                onInput={setRepo}
                onSubmit={(value) => void save(typeof value === "string" ? value : repo)}
              />
            </box>
            <text fg={theme.dim}>Local paths are created if missing. Network and object storage use restic's URL syntax.</text>
          </box>
        </Panel>
        <box flexDirection="row" gap={1}>
          <Tag tone="muted">passphrase comes from RESTIC_PASSWORD</Tag>
          <Tag tone="muted">encrypted by restic</Tag>
        </box>
        <box height={1} />
        <text fg={saving ? theme.accent : theme.dim}>{saving ? "saving configuration…" : "enter to save · q to quit"}</text>
      </box>
    </box>
  )
}
