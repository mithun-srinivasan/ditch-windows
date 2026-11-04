import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useKeyboard, useRenderer } from "@opentui/react"
import type { KeyEvent } from "@opentui/core"
import { BridgeClient } from "./bridge"
import { AppContext, type ScreenID } from "./context"
import { theme, type Tone } from "./theme"
import { type BackupRow, type ProgressState, type Status } from "./types"
import { KeyBar, Spinner, Tag, type ScreenKeyHandler } from "./components/primitives"
import { Dashboard } from "./screens/Dashboard"
import { Backup } from "./screens/Backup"
import { Restore } from "./screens/Restore"
import { Backups } from "./screens/Backups"
import { Verify } from "./screens/Verify"
import { Settings } from "./screens/Settings"
import { Onboarding } from "./screens/Onboarding"
import { Help } from "./screens/Help"

const nav: Array<{ id: ScreenID; label: string; hint: string; glyph: string }> = [
  { id: "dashboard", label: "Dashboard", hint: "overview", glyph: "▣" },
  { id: "backup", label: "Backup", hint: "capture state", glyph: "↥" },
  { id: "restore", label: "Restore", hint: "rebuild machine", glyph: "↧" },
  { id: "backups", label: "Backups", hint: "snapshot archive", glyph: "≡" },
  { id: "verify", label: "Verify", hint: "check integrity", glyph: "✓" },
  { id: "settings", label: "Settings", hint: "paths & policy", glyph: "⚙" },
]

const navOrder: ScreenID[] = ["dashboard", "backup", "restore", "backups", "verify", "settings"]

const footerHints: Record<ScreenID, Array<[string, string]>> = {
  dashboard: [
    ["b", "backup"],
    ["r", "restore"],
    ["l", "backups"],
    ["?", "help"],
    ["q", "quit"],
  ],
  backup: [
    ["space", "select"],
    ["enter", "run"],
    ["esc", "back"],
    ["?", "help"],
  ],
  restore: [
    ["j/k", "move"],
    ["enter", "choose"],
    ["esc", "back"],
    ["?", "help"],
  ],
  backups: [
    ["j/k", "move"],
    ["r", "restore"],
    ["v", "verify"],
    ["?", "help"],
  ],
  verify: [
    ["j/k", "move"],
    ["enter", "verify"],
    ["?", "help"],
  ],
  settings: [
    ["?", "help"],
    ["q", "quit"],
  ],
  onboarding: [
    ["enter", "save"],
    ["?", "help"],
    ["q", "quit"],
  ],
}

const progressByType: Record<string, { kind: ProgressState["kind"]; label: string }> = {
  backup_start: { kind: "backup", label: "Capturing modules" },
  restore_start: { kind: "restore", label: "Rebuilding from snapshot" },
}

function Header({ status, busy, progress }: { status: Status | null; busy: boolean; progress: ProgressState | null }) {
  return (
    <box flexDirection="column" flexShrink={0} backgroundColor={theme.bg}>
      <box flexDirection="row" height={1} paddingX={1} justifyContent="space-between">
        <box flexDirection="row" gap={1}>
          <text fg={theme.accent} attributes={1}>
            DITCH
          </text>
          <text fg={theme.dim}>//</text>
          <text fg={theme.text} attributes={1}>
            WINDOWS
          </text>
          <text fg={theme.dim}>11 rebuild kit</text>
        </box>
        <box flexDirection="row" gap={1}>
          {status?.demo ? <Tag tone="yellow">DEMO</Tag> : null}
          <Tag tone={status?.repoExists ? "green" : "yellow"}>{status?.repoExists ? "REPO READY" : "SETUP"}</Tag>
          <Tag tone={status?.encrypted ? "mint" : "dim"}>{status?.encrypted ? "ENCRYPTED" : "PLAINTEXT"}</Tag>
          {status?.elevated ? <Tag tone="blue">ELEVATED</Tag> : null}
          {busy ? <Spinner /> : null}
        </box>
      </box>
      <box flexDirection="row" height={1} paddingX={1} justifyContent="space-between">
        <text fg={theme.dim} wrapMode="none">
          {status ? status.repo || "no repository configured" : "connecting to engine…"}
        </text>
        <text fg={theme.muted}>
          {progress?.message || (status ? `${status.hostname} · ${status.defaultProfile}` : "")}
        </text>
      </box>
      <box height={1} border={["bottom"]} borderColor={theme.border} />
    </box>
  )
}

function Nav({ screen, go, status }: { screen: ScreenID; go: (screen: ScreenID) => void; status: Status | null }) {
  return (
    <box
      width={25}
      flexShrink={0}
      flexDirection="column"
      backgroundColor={theme.surface}
      border={["right"]}
      borderColor={theme.border}
      paddingY={1}
    >
      <text fg={theme.dim} paddingX={1}>
        {"  WORKSPACE"}
      </text>
      <box height={1} />
      {nav.map((item, index) => {
        const active = item.id === screen
        return (
          <box
            key={item.id}
            flexDirection="row"
            height={2}
            paddingX={1}
            backgroundColor={active ? theme.surfaceAlt : theme.surface}
            onMouseDown={() => go(item.id)}
          >
            <box width={2} flexShrink={0}>
              <text fg={active ? theme.accent : theme.dim}>{active ? "▌" : " "}</text>
            </box>
            <box flexDirection="column" flexGrow={1}>
              <text fg={active ? theme.text : theme.muted} attributes={active ? 1 : 0}>
                {item.glyph} {item.label}
              </text>
              <text fg={theme.dim}>
                {"   "}
                {item.hint}
              </text>
            </box>
            <text fg={theme.dim}>{index + 1}</text>
          </box>
        )
      })}
      <box flexGrow={1} />
      <box flexDirection="column" paddingX={2} gap={0}>
        <text fg={theme.dim}>{"PROFILE"}</text>
        <text fg={theme.muted}>{status?.defaultProfile || "—"}</text>
        <box height={1} />
        <text fg={theme.dim}>{"POLICY"}</text>
        <text fg={theme.muted}>{status?.conflictMode || "—"}</text>
        <box height={1} />
        <text fg={theme.dim}>{status ? (status.version === "dev" ? "dev" : `v${status.version}`) : ""}</text>
      </box>
    </box>
  )
}

export function App() {
  const renderer = useRenderer()
  const [client, setClient] = useState<BridgeClient | null>(null)
  const [connectError, setConnectError] = useState<string | null>(null)
  const [status, setStatus] = useState<Status | null>(null)
  const [statusError, setStatusError] = useState<string | null>(null)
  const [backups, setBackups] = useState<BackupRow[]>([])
  const [backupsLoading, setBackupsLoading] = useState(false)
  const [screen, setScreen] = useState<ScreenID>("dashboard")
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<ProgressState | null>(null)
  const [restoreTarget, setRestoreTarget] = useState<string | null>(null)
  const [helpOpen, setHelpOpen] = useState(false)
  const [toast, setToast] = useState<{ message: string; tone: Tone } | null>(null)
  const keysRef = useRef<ScreenKeyHandler | null>(null)
  const fatalRef = useRef(false)
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  useEffect(() => {
    try {
      setClient(new BridgeClient())
    } catch (error) {
      setConnectError(error instanceof Error ? error.message : String(error))
    }
  }, [])

  const notify = useCallback((message: string, tone: Tone = "muted") => {
    setToast({ message, tone })
    if (toastTimer.current) clearTimeout(toastTimer.current)
    toastTimer.current = setTimeout(() => setToast(null), 4200)
  }, [])

  const reloadStatus = useCallback(async () => {
    if (!client) return
    try {
      const data = await client.request<Status>("status")
      setStatus(data)
      setStatusError(null)
      if (!data.configured && !data.demo) setScreen("onboarding")
    } catch (error) {
      if (fatalRef.current) return
      setStatusError(error instanceof Error ? error.message : String(error))
    }
  }, [client])

  const refreshBackups = useCallback(async () => {
    if (!client) return
    setBackupsLoading(true)
    try {
      const data = await client.request<{ backups: BackupRow[] }>("list")
      setBackups(data.backups || [])
    } catch (error) {
      notify(error instanceof Error ? error.message : String(error), "red")
    } finally {
      setBackupsLoading(false)
    }
  }, [client, notify])

  useEffect(() => {
    if (!client) return
    const unsubscribe = client.onEvent((event, data) => {
      if (event === "fatal") {
        const detail = data as { message?: string }
        fatalRef.current = true
        setStatusError(detail?.message || "engine failed to start")
        return
      }
      if (event !== "progress") return
      const update = data as {
        type: string
        module?: string
        message?: string
        error?: string
        items?: number
        details?: string
        status?: string
      }
      setProgress((current) => {
        if (!current) {
          const meta = progressByType[update.type]
          if (!meta) return current
          return {
            kind: meta.kind,
            label: meta.label,
            message: update.message || meta.label,
            modules: [],
          }
        }
        const modules = current.modules.map((module) => {
          if (module.name !== update.module) return module
          let phase = module.phase
          if (update.type === "module_scan") phase = "scan"
          if (update.type === "module_start") phase = "run"
          if (update.type === "module_done") phase = update.status === "ok" ? "ok" : update.status === "skipped" ? "skipped" : "failed"
          return {
            ...module,
            phase,
            items: update.items ?? module.items,
            details: update.details ?? module.details,
            error: update.error ?? module.error,
          }
        })
        return { ...current, modules, message: update.message || current.message }
      })
    })
    return unsubscribe
  }, [client])

  useEffect(() => {
    if (!client) return
    void reloadStatus()
    void refreshBackups()
  }, [client, reloadStatus, refreshBackups])

  useEffect(() => {
    if (!client) return
    if (!status?.configured && !status?.demo) return
    if (backups.length === 0) void refreshBackups()
  }, [status, client, backups.length, refreshBackups])

  const quit = useCallback(() => {
    client?.close()
    renderer.destroy()
    setTimeout(() => process.exit(0), 60)
  }, [client, renderer])

  const go = useCallback(
    (next: ScreenID) => {
      if (next === "onboarding" && status?.configured) return
      setScreen(next)
      setHelpOpen(false)
    },
    [status],
  )

  const registerKeys = useCallback((handler: ScreenKeyHandler | null) => {
    keysRef.current = handler
  }, [])

  const startProgress = useCallback((kind: ProgressState["kind"], label: string, modules: string[]) => {
    setBusy(true)
    setProgress({
      kind,
      label,
      message: label,
      modules: modules.map((name) => ({ name, phase: "queued" })),
    })
  }, [])

  const patchProgress = useCallback((patch: Partial<ProgressState>) => {
    setProgress((current) => (current ? { ...current, ...patch } : current))
  }, [])

  const patchModule = useCallback((name: string, patch: Partial<ProgressState["modules"][number]>) => {
    setProgress((current) => {
      if (!current) return current
      return {
        ...current,
        modules: current.modules.map((module) => (module.name === name ? { ...module, ...patch } : module)),
      }
    })
  }, [])

  const navigate = useCallback(
    (direction: number) => {
      const order = navOrder.filter((id) => status?.configured || id !== "onboarding")
      const index = Math.max(0, order.indexOf(screen))
      setScreen(order[(index + direction + order.length) % order.length])
    },
    [screen, status],
  )

  useKeyboard((key: KeyEvent) => {
    if (helpOpen) {
      if (key.name === "escape" || key.name === "?" || key.name === "q") setHelpOpen(false)
      return
    }
    if (key.name === "?") {
      setHelpOpen(true)
      return
    }
    if (keysRef.current?.(key)) return
    if (key.name === "q") {
      quit()
      return
    }
    if (screen === "onboarding") return
    if (key.name === "tab") {
      navigate(key.shift ? -1 : 1)
      return
    }
    if (key.name === "1" || key.name === "g") go("dashboard")
    else if (key.name === "2") go("backup")
    else if (key.name === "3") go("restore")
    else if (key.name === "4" || key.name === "l") go("backups")
    else if (key.name === "5" || key.name === "v") go("verify")
    else if (key.name === "6" || key.name === "s") go("settings")
    else if (key.name === "d") go("dashboard")
    else if (key.name === "b") go("backup")
    else if (key.name === "r") go("restore")
  })

  const value = useMemo(
    () => ({
      client: client as BridgeClient,
      status,
      statusError,
      reloadStatus,
      backups,
      backupsLoading,
      refreshBackups,
      screen,
      go,
      quit,
      notify,
      busy,
      setBusy,
      progress,
      setProgress,
      restoreTarget,
      setRestoreTarget,
      registerKeys,
      startProgress,
      patchProgress,
      patchModule,
    }),
    [
      client,
      status,
      statusError,
      reloadStatus,
      backups,
      backupsLoading,
      refreshBackups,
      screen,
      go,
      quit,
      notify,
      busy,
      progress,
      restoreTarget,
      registerKeys,
      startProgress,
      patchProgress,
      patchModule,
    ],
  )

  if (connectError) {
    return (
      <box flexGrow={1} flexDirection="column" justifyContent="center" alignItems="center" gap={1}>
        <text fg={theme.red}>ENGINE OFFLINE</text>
        <text fg={theme.muted}>{connectError}</text>
        <text fg={theme.dim}>Run ditch-windows tui from the repository root.</text>
        <KeyBar hints={[["q", "quit"]]} />
      </box>
    )
  }

  if (!client) {
    return (
      <box flexGrow={1} justifyContent="center" alignItems="center">
        <text fg={theme.dim}>starting engine…</text>
      </box>
    )
  }

  let body
  if (statusError && !status) {
    body = (
      <box flexGrow={1} justifyContent="center" alignItems="center" flexDirection="column" gap={1}>
        <text fg={theme.red}>ENGINE ERROR</text>
        <text fg={theme.muted}>{statusError}</text>
        <text fg={theme.dim}>Press q to quit, r to retry.</text>
        <text fg={theme.accent} onMouseDown={() => void reloadStatus()}>
          retry
        </text>
      </box>
    )
  } else if (!status) {
    body = (
      <box flexGrow={1} justifyContent="center" alignItems="center" gap={1}>
        <Spinner />
        <text fg={theme.muted}>reading repository state…</text>
      </box>
    )
  } else if (screen === "onboarding" || (!status.configured && !status.demo)) {
    body = <Onboarding />
  } else if (screen === "backup") {
    body = <Backup />
  } else if (screen === "restore") {
    body = <Restore />
  } else if (screen === "backups") {
    body = <Backups />
  } else if (screen === "verify") {
    body = <Verify />
  } else if (screen === "settings") {
    body = <Settings />
  } else {
    body = <Dashboard />
  }

  return (
    <AppContext.Provider value={value}>
      <box flexGrow={1} flexDirection="column" backgroundColor={theme.bg}>
        <Header status={status} busy={busy} progress={progress} />
        <box flexGrow={1} flexDirection="row" minHeight={0}>
          <Nav screen={screen} go={go} status={status} />
          <box flexGrow={1} flexDirection="column" padding={1} minHeight={0}>
            {body}
          </box>
        </box>
        <box height={1} border={["top"]} borderColor={theme.border} />
        <KeyBar hints={footerHints[screen]} />
        {toast ? (
          <box
            position="absolute"
            right={2}
            bottom={3}
            paddingX={2}
            backgroundColor={theme.surfaceAlt}
            border
            borderStyle="single"
            borderColor={theme[toast.tone]}
            zIndex={200}
          >
            <text fg={theme[toast.tone]}>{toast.message}</text>
          </box>
        ) : null}
        {helpOpen ? <Help onClose={() => setHelpOpen(false)} /> : null}
      </box>
    </AppContext.Provider>
  )
}
