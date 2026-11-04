export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  let value = bytes
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `${value >= 10 || unit === 0 ? value.toFixed(0) : value.toFixed(1)} ${units[unit]}`
}

export function formatTime(value: string | undefined): string {
  if (!value) return "never"
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
}

export function formatDuration(ms: number | undefined): string {
  if (!ms || ms < 0) return "—"
  if (ms < 1000) return `${ms}ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  return `${minutes}m ${Math.round(seconds % 60)}s`
}

export function shortId(id: string | undefined): string {
  if (!id) return "—"
  return id.length > 22 ? id.slice(0, 22) : id
}

export function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value))
}

export function moveIndex(index: number, delta: number, length: number): number {
  if (length <= 0) return 0
  return (index + delta + length) % length
}

export function conflictLabel(mode: string): string {
  switch (mode) {
    case "overwrite_keep_bak":
      return "keep .bak"
    case "skip":
      return "skip existing"
    case "ask_per_item":
      return "ask per file"
    default:
      return mode || "keep .bak"
  }
}

export function plural(count: number, one: string, many = one + "s"): string {
  return `${count} ${count === 1 ? one : many}`
}

export function truncate(value: string, max: number): string {
  if (max <= 1 || value.length <= max) return value
  return value.slice(0, max - 1) + "…"
}

export function middleEllipsis(value: string, max: number): string {
  if (value.length <= max) return value
  if (max <= 5) return value.slice(0, max)
  const keep = max - 3
  const head = Math.ceil(keep / 2)
  const tail = keep - head
  return value.slice(0, head) + "…" + value.slice(value.length - tail)
}
