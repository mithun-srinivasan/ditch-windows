import { useCallback, useEffect } from "react"
import { useApp } from "../context"
import { theme } from "../theme"
import { conflictLabel, middleEllipsis, plural, truncate } from "../format"
import { HLine, InfoRow, KeyBar, Panel, Tag } from "../components/primitives"

function WrappedField({ label, value, tone = theme.text }: { label: string; value: string; tone?: string }) {
  return (
    <box flexDirection="column" flexShrink={0}>
      <text fg={theme.muted}>{label}</text>
      <text fg={tone} wrapMode="char">
        {value}
      </text>
    </box>
  )
}

function CompactPath({ label, value }: { label: string; value: string }) {
  return (
    <box flexDirection="row" height={1} flexShrink={0}>
      <box width={10} flexShrink={0}>
        <text fg={theme.muted}>{label}</text>
      </box>
      <text fg={theme.text}>{middleEllipsis(value, 52)}</text>
    </box>
  )
}

export function Settings() {
  const { status, registerKeys, go, backups } = useApp()

  const handleKey = useCallback(
    (key: { name: string }) => {
      if (key.name === "escape") {
        go("dashboard")
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

  const profiles = Object.entries(status.profiles)

  return (
    <box flexDirection="column" flexGrow={1} gap={1} minHeight={0}>
      <box flexDirection="row" gap={1} flexShrink={0}>
        <Tag tone="accent">SETTINGS</Tag>
        <Tag tone="muted">read only</Tag>
        <text fg={theme.dim}>edit the config file, then relaunch</text>
      </box>
      <box flexDirection="row" gap={1} flexGrow={1} minHeight={0}>
        <box flexDirection="column" gap={1} flexGrow={2} minHeight={0}>
          <Panel title="REPOSITORY" height={11} flexGrow={0}>
            <WrappedField label="URL" value={status.repo || "not configured"} tone={theme.accent} />
            <InfoRow label="STATUS" value={status.repoExists ? "reachable" : "missing"} tone={status.repoExists ? "green" : "yellow"} />
            <InfoRow label="ENCRYPTION" value={status.encrypted ? "restic password present" : "no password"} tone={status.encrypted ? "mint" : "yellow"} />
            <InfoRow label="DEFAULT PROFILE" value={status.defaultProfile} />
            <InfoRow label="CONFLICT POLICY" value={`${conflictLabel(status.conflictMode)} (${status.conflictMode})`} />
            <InfoRow label="SNAPSHOTS" value={plural(backups.length, "snapshot")} />
          </Panel>
          <Panel title="PATHS" height={9} flexGrow={0}>
            <CompactPath label="CONFIG" value={status.configPath} />
            <CompactPath label="HOME" value={status.home} />
            <CompactPath label="BACKUPS" value={status.backupsDir} />
            <CompactPath label="LOGS" value={status.logsDir} />
            {status.base ? <CompactPath label="DEMO" value={status.base} /> : null}
          </Panel>
          <Panel title="MODULES" flexGrow={1}>
            <box flexDirection="column">
              {status.modules.map((module) => (
                <box key={module.name} flexDirection="row" height={1} paddingX={1}>
                  <box width={18}>
                    <text fg={theme.text}>{module.name}</text>
                  </box>
                  <box flexGrow={1}>
                    <text fg={theme.dim} wrapMode="none">
                      {truncate(module.description, 30)}
                    </text>
                  </box>
                  <text fg={module.needsAdmin ? theme.yellow : theme.dim}>{module.needsAdmin ? "admin" : "user"}</text>
                </box>
              ))}
            </box>
          </Panel>
        </box>
        <box flexDirection="column" gap={1} width={48} flexShrink={0} minHeight={0}>
          <Panel title="PROFILES" flexGrow={1}>
            <box flexDirection="column" gap={0}>
              {profiles.map(([name, profile]) => {
                const isDefault = name === status.defaultProfile
                return (
                  <box key={name} flexDirection="column">
                    <box flexDirection="row" gap={1}>
                      <text fg={isDefault ? theme.accent : theme.text} attributes={isDefault ? 1 : 0}>
                        {name}
                      </text>
                      {isDefault ? (
                        <Tag tone="accent">default</Tag>
                      ) : (
                        <text fg={theme.dim}>{plural(profile.modules.length, "module")}</text>
                      )}
                    </box>
                    <text fg={theme.muted} wrapMode="word">
                      {isDefault
                        ? profile.modules.join(" · ")
                        : `${profile.includes.length} include roots · ${profile.excludes.length} exclude filters`}
                    </text>
                    {isDefault && profile.includes.length ? (
                      <box flexDirection="column">
                        <text fg={theme.dim}>include</text>
                        <text fg={theme.muted} wrapMode="char">
                          {profile.includes.join(", ")}
                        </text>
                      </box>
                    ) : null}
                    {isDefault && profile.excludes.length ? (
                      <box flexDirection="column">
                        <text fg={theme.dim}>exclude</text>
                        <text fg={theme.muted} wrapMode="char">
                          {profile.excludes.join(", ")}
                        </text>
                      </box>
                    ) : null}
                  </box>
                )
              })}
            </box>
            <box flexGrow={1} />
            <HLine />
            <text fg={theme.dim} wrapMode="word">
              Profiles are declared in {status.configPath}. Use `ditch-windows config init` to create a template.
            </text>
          </Panel>
          <Panel title="SAFETY" height={8} flexGrow={0}>
            <box flexDirection="column" gap={0}>
              <InfoRow label="ELEVATION" value={status.elevated ? "administrator" : "standard user"} tone={status.elevated ? "blue" : "yellow"} />
              <InfoRow label="VERIFY AFTER" value={status.demo ? "simulated" : "configurable"} />
              <InfoRow label="MODE" value={status.demo ? "demo sandbox" : "live system"} tone={status.demo ? "yellow" : "red"} />
              <box height={1} />
              <text fg={theme.dim} wrapMode="word">
                {status.demo ? "Demo mode never touches real system settings." : "Backups are read-only. Restores write to disk and should be previewed first."}
              </text>
            </box>
          </Panel>
        </box>
      </box>
      <KeyBar hints={[["esc", "dashboard"], ["?", "help"], ["q", "quit"]]} />
    </box>
  )
}
