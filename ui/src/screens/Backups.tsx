import { useCallback, useEffect, useState } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { formatTime, plural, shortId, truncate } from "../format"
import { EmptyState, InfoRow, KeyBar, Panel, Tag } from "../components/primitives"

export function Backups() {
  const { backups, backupsLoading, refreshBackups, registerKeys, go, setRestoreTarget, notify, status } = useApp()
  const [cursor, setCursor] = useState(0)
  const selected = backups[cursor]

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (key.name === "j" || key.name === "down") {
        setCursor((index) => (backups.length ? (index + 1) % backups.length : 0))
        return true
      }
      if (key.name === "k" || key.name === "up") {
        setCursor((index) => (backups.length ? (index - 1 + backups.length) % backups.length : 0))
        return true
      }
      if (key.name === "r" || key.name === "return") {
        if (selected) {
          setRestoreTarget(selected.id)
          go("restore")
        }
        return true
      }
      if (key.name === "v") {
        if (selected) {
          setRestoreTarget(selected.id)
          go("verify")
        }
        return true
      }
      if (key.name === "u") {
        void refreshBackups()
        return true
      }
      if (key.name === "escape") {
        go("dashboard")
        return true
      }
      return false
    },
    [backups.length, go, refreshBackups, selected, setRestoreTarget],
  )

  useEffect(() => {
    registerKeys(handleKey)
    return () => registerKeys(null)
  }, [registerKeys, handleKey])

  return (
    <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
      <box flexDirection="row" gap={1} flexShrink={0}>
        <Tag tone="accent">ARCHIVE</Tag>
        <Tag tone="muted">{plural(backups.length, "snapshot")}</Tag>
        <Tag tone={backupsLoading ? "accent" : "dim"}>{backupsLoading ? "syncing" : "in sync"}</Tag>
        <text fg={theme.dim}>restic · content-addressed · encrypted</text>
      </box>
      <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
        <Panel title="SNAPSHOTS" right="newest first" flexGrow={2}>
          {backups.length === 0 ? (
            <EmptyState title="Nothing captured yet" hint="Press b from the dashboard to create the first snapshot." />
          ) : (
            <box flexDirection="column">
              <box flexDirection="row" height={1} paddingX={1}>
                <box width={3}>
                  <text fg={theme.dim}> </text>
                </box>
                <box width={24}>
                  <text fg={theme.dim}>ID</text>
                </box>
                <box width={19}>
                  <text fg={theme.dim}>CAPTURED</text>
                </box>
                <box width={15} flexShrink={0}>
                  <text fg={theme.dim}>HOST</text>
                </box>
              </box>
              {backups.map((backup, index) => {
                const active = index === cursor
                return (
                  <box
                    key={backup.id}
                    flexDirection="row"
                    height={1}
                    paddingX={1}
                    backgroundColor={active ? theme.surfaceAlt : theme.surface}
                    onMouseDown={() => setCursor(index)}
                  >
                    <box width={3}>
                      <text fg={active ? theme.accent : theme.dim}>{active ? "▌" : " "}</text>
                    </box>
                    <box width={24}>
                      <text fg={active ? theme.text : theme.muted} wrapMode="none">
                        {shortId(backup.id)}
                      </text>
                    </box>
                    <box width={19}>
                      <text fg={theme.muted}>{formatTime(backup.time)}</text>
                    </box>
                    <box width={15} flexShrink={0}>
                      <text fg={theme.dim} wrapMode="none">{truncate(backup.hostname, 14)}</text>
                    </box>
                  </box>
                )
              })}
            </box>
          )}
          <box flexGrow={1} />
          <text fg={theme.dim}>j/k move · enter preview restore · r restore · v verify · u refresh</text>
        </Panel>

        <box width={46} flexShrink={0} flexDirection="column" gap={1} minHeight={0}>
          <Panel title="SNAPSHOT DETAIL" flexGrow={1}>
            {selected ? (
              <box flexDirection="column" gap={0}>
                <InfoRow label="BACKUP ID" value={shortId(selected.id)} tone="accent" />
                <InfoRow label="FULL ID" value={selected.id} />
                <InfoRow label="CAPTURED" value={formatTime(selected.time)} />
                <InfoRow label="HOSTNAME" value={selected.hostname} />
                <InfoRow label="REPOSITORY" value={status?.repoExists ? "available" : "missing"} tone={status?.repoExists ? "green" : "yellow"} />
                <box height={1} />
                <text fg={theme.muted} wrapMode="word">
                  Inspect the snapshot before rebuilding. The restore preview resolves modules, file counts and
                  conflicts without writing anything.
                </text>
                <box height={1} />
                <box
                  flexDirection="row"
                  gap={1}
                  onMouseDown={() => {
                    setRestoreTarget(selected.id)
                    go("restore")
                  }}
                >
                  <Tag tone="accent">enter · restore</Tag>
                  <Tag tone="mint">v · verify</Tag>
                </box>
              </box>
            ) : (
              <EmptyState title="Select a snapshot" />
            )}
          </Panel>
          <Panel title="INTEGRITY" flexGrow={0} height={8}>
            <box flexDirection="column" gap={0}>
              <InfoRow label="REPO STATUS" value={status?.repoExists ? "reachable" : "unknown"} tone={status?.repoExists ? "green" : "yellow"} />
              <InfoRow label="ENCRYPTION" value={status?.encrypted ? "enabled" : "off"} tone={status?.encrypted ? "mint" : "yellow"} />
              <box height={1} />
              <text
                fg={theme.accent}
                onMouseDown={() => {
                  if (selected) {
                    setRestoreTarget(selected.id)
                    go("verify")
                  } else {
                    notify("Select a snapshot first", "yellow")
                  }
                }}
              >
                run verification →
              </text>
            </box>
          </Panel>
        </box>
      </box>
      <KeyBar
        hints={[
          ["j/k", "move"],
          ["enter", "restore"],
          ["v", "verify"],
          ["u", "refresh"],
          ["esc", "dashboard"],
        ]}
      />
    </box>
  )
}
