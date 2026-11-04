import { createContext, useContext } from "react"
import type { BridgeClient } from "./bridge"
import type { BackupRow, ProgressState, Status } from "./types"
import type { Tone } from "./theme"
import type { ScreenKeyHandler } from "./components/primitives"

export type ScreenID =
  | "dashboard"
  | "backup"
  | "restore"
  | "backups"
  | "verify"
  | "settings"
  | "onboarding"

export type AppContextValue = {
  client: BridgeClient
  status: Status | null
  statusError: string | null
  reloadStatus: () => Promise<void>
  backups: BackupRow[]
  backupsLoading: boolean
  refreshBackups: () => Promise<void>
  screen: ScreenID
  go: (screen: ScreenID) => void
  quit: () => void
  notify: (message: string, tone?: Tone) => void
  busy: boolean
  setBusy: (value: boolean) => void
  progress: ProgressState | null
  setProgress: (value: ProgressState | null) => void
  restoreTarget: string | null
  setRestoreTarget: (value: string | null) => void
  registerKeys: (handler: ScreenKeyHandler | null) => void
  startProgress: (kind: ProgressState["kind"], label: string, modules: string[]) => void
  patchProgress: (patch: Partial<ProgressState>) => void
  patchModule: (name: string, patch: Partial<ProgressState["modules"][number]>) => void
}

export const AppContext = createContext<AppContextValue | null>(null)

export function useApp(): AppContextValue {
  const value = useContext(AppContext)
  if (!value) throw new Error("useApp must be used inside AppContext")
  return value
}
