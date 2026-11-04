import { useCallback, useEffect, useMemo } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { formatTime, plural, shortId, truncate } from "../format"
import { InfoRow, KeyBar, Panel, Tag } from "../components/primitives"

const actions: Array<{ key: string; label: string; detail: string; screen: "backup" | "restore" | "backups" | "verify" }> = [
  { key: "b", label: "Capture a backup", detail: "pack the selected modules", screen: "backup" },
  { key: "r", label: "Rebuild this machine", detail: "preview, then restore", screen: "restore" },
  { key: "l", label: "Browse the archive", detail: "inspect every snapshot", screen: "backups" },
  { key: "v", label: "Verify integrity", detail: "check repo and hashes", screen: "verify" },
]

export function Dashboard() {
  const { status, backups, go, registerKeys, notify } = useApp()

  const enabled = useMemo(() => {
    if (!status) return new Set<string>()
    const profile = status.profiles[status.defaultProfile]
    return new Set(profile?.modules || [])
  }, [status])

  const last = backups[0]

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (key.name === "b") {
        go("backup")
        return true
      }
      if (key.name === "r") {
        go("restore")
        return true
      }
      if (key.name === "l") {
        go("backups")
        return true
      }
      if (key.name === "v") {
        go("verify")
        return true
      }
      return false
    },
    [go],
  )

  useEffect(() => {
    registerKeys(handleKey)
    return () => registerKeys(null)
  }, [registerKeys, handleKey])

  if (!status) return null

  return (
    <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
      <box flexDirection="row" gap={1} height={10} flexShrink={0}>
        <Panel title="IDENTITY" flexGrow={2} backgroundColor={theme.surface}>
          <box flexDirection="row" flexGrow={1} alignItems="center" gap={3}>
            <ascii-font text="DITCH" font="block" color={theme.accent} />
            <box flexDirection="column" gap={1} flexGrow={1}>
              <text fg={theme.text} attributes={1}>
                WINDOWS 11 REBUILD KIT
              </text>
              <text fg={theme.muted} wrapMode="word">
                Declare the machine you want. Capture the pieces that make it yours.
                Rebuild from a verified snapshot whenever Windows has other plans.
              </text>
              <box flexDirection="row" gap={1}>
                <Tag tone="accent">{status.modules.length} PACKS</Tag>
                <Tag tone="mint">RESTIC</Tag>
                <Tag tone="blue">{status.platform.toUpperCase()}</Tag>
              </box>
            </box>
          </box>
        </Panel>
        <Panel title="TARGET" width={44} flexShrink={0}>
          <box flexDirection="column" gap={0}>
            <InfoRow label="MACHINE" value={status.hostname} />
            <InfoRow label="PROFILE" value={status.defaultProfile} tone="accent" />
            <InfoRow label="REPOSITORY" value={status.repoExists ? "available" : "missing"} tone={status.repoExists ? "green" : "yellow"} />
            <InfoRow label="ENCRYPTION" value={status.encrypted ? "restic encrypted" : "not encrypted"} tone={status.encrypted ? "mint" : "yellow"} />
            <InfoRow label="LAST SNAPSHOT" value={last ? formatTime(last.time) : "never"} tone={last ? "text" : "dim"} />
            <InfoRow label="ARCHIVE" value={plural(backups.length, "snapshot")} />
            <BoxGap />
            <text fg={theme.dim} wrapMode="word">
              {status.demo ? "Demo workspace. Every module is simulated; no system state is touched." : status.home}
            </text>
          </box>
        </Panel>
      </box>

      <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
        <Panel title="MODULE PACKS" right={`profile · ${status.defaultProfile}`} flexGrow={2}>
          <box flexDirection="row" height={1} flexShrink={0} paddingX={1}>
            <box width={3}>
              <text fg={theme.dim}> </text>
            </box>
            <box width={18}>
              <text fg={theme.dim}>NAME</text>
            </box>
            <box flexGrow={1}>
              <text fg={theme.dim}>CAPTURES</text>
            </box>
            <box width={9}>
              <text fg={theme.dim}>STATE</text>
            </box>
          </box>
          {status.modules.map((module) => {
            const on = enabled.has(module.name)
            return (
              <box key={module.name} flexDirection="row" height={2} paddingX={1} backgroundColor={on ? theme.surfaceAlt : theme.surface}>
                <box width={3}>
                  <text fg={on ? theme.green : theme.dim}>{on ? "●" : "○"}</text>
                </box>
                <box width={18}>
                  <text fg={on ? theme.text : theme.muted}>{module.name}</text>
                </box>
                <box flexGrow={1}>
                  <text fg={theme.dim} wrapMode="none">
                    {truncate(module.description, 30)}
                  </text>
                </box>
                <box width={9}>
                  <text fg={module.needsAdmin ? theme.yellow : theme.dim}>{module.needsAdmin ? "admin" : "user"}</text>
                </box>
              </box>
            )
          })}
          <box flexGrow={1} />
          <text fg={theme.dim}>space toggles modules on the Backup screen · profiles live in Settings</text>
        </Panel>

        <box width={44} flexShrink={0} flexDirection="column" gap={1} minHeight={0}>
          <Panel title="QUICK ACTIONS" flexGrow={0} height={10}>
            <box flexDirection="column" gap={0}>
              {actions.map((action) => (
                <box
                  key={action.key}
                  flexDirection="row"
                  height={2}
                  paddingX={1}
                  onMouseDown={() => {
                    go(action.screen)
                    notify(`${action.label} opened`, "muted")
                  }}
                >
                  <box width={3}>
                    <text fg={theme.accent} attributes={1}>
                      {action.key}
                    </text>
                  </box>
                  <box flexDirection="column" flexGrow={1}>
                    <text fg={theme.text}>{action.label}</text>
                    <text fg={theme.dim}>{action.detail}</text>
                  </box>
                  <text fg={theme.dim}>→</text>
                </box>
              ))}
            </box>
          </Panel>
          <Panel title="LATEST SNAPSHOT" flexGrow={1}>
            {last ? (
              <box flexDirection="column" gap={0}>
                <InfoRow label="BACKUP" value={shortId(last.id)} tone="accent" />
                <InfoRow label="CREATED" value={formatTime(last.time)} />
                <InfoRow label="HOST" value={last.hostname} />
                <box height={1} />
                <text fg={theme.muted} wrapMode="word">
                  Open Backups to inspect modules, or Restore to preview this snapshot before touching disk.
                </text>
                <box height={1} />
                <box flexDirection="row" gap={2}>
                  <text fg={theme.accent} onMouseDown={() => go("backups")}>
                    [ inspect ]
                  </text>
                  <text fg={theme.mint} onMouseDown={() => go("restore")}>
                    [ restore ]
                  </text>
                </box>
              </box>
            ) : (
              <box flexGrow={1} justifyContent="center" alignItems="center" flexDirection="column" gap={1}>
                <text fg={theme.dim}>NO SNAPSHOTS YET</text>
                <text fg={theme.muted}>Press b to capture the first one.</text>
              </box>
            )}
          </Panel>
        </box>
      </box>
      <KeyBar
        hints={[
          ["b", "capture"],
          ["r", "rebuild"],
          ["l", "archive"],
          ["v", "verify"],
          ["?", "help"],
        ]}
      />
    </box>
  )
}

function BoxGap() {
  return <box height={1} flexShrink={0} />
}
