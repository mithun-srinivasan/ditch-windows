export type BridgeProfile = {
  modules: string[]
  includes: string[]
  excludes: string[]
}

export type BridgeModule = {
  name: string
  description: string
  needsAdmin: boolean
}

export type Status = {
  configured: boolean
  demo: boolean
  repo: string
  repoExists: boolean
  repoError?: string
  encrypted: boolean
  elevated: boolean
  platform: string
  hostname: string
  home: string
  base?: string
  configPath: string
  logsDir: string
  backupsDir: string
  defaultProfile: string
  conflictMode: string
  profiles: Record<string, BridgeProfile>
  modules: BridgeModule[]
  version: string
}

export type BackupRow = {
  id: string
  time: string
  hostname: string
}

export type BackupResult = {
  backupId: string
  snapshots: number
  partial: boolean
  durationMs: number
  modules: ModuleResult[]
  error?: string
}

export type ModuleResult = {
  name: string
  status: string
  items: number
  details?: string
  error?: string
  durationMs: number
}

export type PreviewModule = {
  name: string
  available: boolean
  needsAdmin: boolean
  elevated: boolean
  items: number
  details?: string
  files: number
  bytes: number
  conflicts: number
  error?: string
}

export type PreviewResult = {
  backupId: string
  time: string
  hostname: string
  modules: PreviewModule[]
  files: number
  bytes: number
  conflicts: number
}

export type RestoreModule = {
  name: string
  success: boolean
  skipped: boolean
  error?: string
  durationMs: number
}

export type ApplyResult = {
  backupId: string
  dryRun: boolean
  durationMs: number
  modules: RestoreModule[]
  baks: string[]
}

export type VerifyModule = {
  name: string
  verified: boolean
  error?: string
}

export type VerifyResult = {
  backupId: string
  repositoryOk: boolean
  repositoryError?: string
  hashOk: boolean
  hashError?: string
  modules: VerifyModule[]
}

export type DemoFile = {
  path: string
  size: number
  bak: boolean
}

export type InitResult = {
  saved: boolean
  repo: string
  needsRelaunch: boolean
  message: string
}

export type ProgressModule = {
  name: string
  phase: "queued" | "scan" | "run" | "ok" | "failed" | "skipped"
  items?: number
  details?: string
  error?: string
}

export type ProgressState = {
  kind: "backup" | "restore" | "verify"
  label: string
  modules: ProgressModule[]
  message: string
  finished?: boolean
}
