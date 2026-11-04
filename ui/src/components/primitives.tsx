import { useEffect, useState, type ReactNode } from "react"
import type { KeyEvent } from "@opentui/core"
import { theme, type Tone } from "../theme"

type PanelProps = {
  title?: string
  right?: string
  children?: ReactNode
  flexGrow?: number
  flexShrink?: number
  width?: number | `${number}%`
  height?: number | `${number}%`
  minHeight?: number
  padding?: number
  backgroundColor?: string
  borderColor?: string
  bottomTitle?: string
}

export function Panel({
  title,
  right,
  children,
  flexGrow = 1,
  flexShrink,
  width,
  height,
  minHeight,
  padding = 1,
  backgroundColor = theme.surface,
  borderColor = theme.border,
  bottomTitle,
}: PanelProps) {
  return (
    <box
      flexGrow={flexGrow}
      flexShrink={flexShrink}
      width={width}
      height={height}
      minHeight={minHeight}
      flexDirection="column"
      backgroundColor={backgroundColor}
      border
      borderStyle="single"
      borderColor={borderColor}
      title={title ? ` ${title} ` : undefined}
      titleColor={theme.muted}
      titleAlignment="left"
      bottomTitle={bottomTitle}
      bottomTitleAlignment="right"
      padding={padding}
      overflow="hidden"
    >
      {right ? (
        <box flexDirection="row" justifyContent="flex-end" height={1} flexShrink={0}>
          <text fg={theme.dim}>{right}</text>
        </box>
      ) : null}
      {children}
    </box>
  )
}

export function Tag({
  children,
  tone = "muted",
  filled = false,
}: {
  children: ReactNode
  tone?: Tone
  filled?: boolean
}) {
  const color = theme[tone]
  return (
    <box
      paddingX={1}
      backgroundColor={filled ? color : theme.surfaceAlt}
      height={1}
      flexShrink={0}
    >
      <text fg={filled ? theme.bg : color}>{children}</text>
    </box>
  )
}

export function HLine({ color = theme.border }: { color?: string }) {
  return (
    <box height={1} flexShrink={0} border={["bottom"]} borderColor={color} />
  )
}

export function KeyBar({ hints }: { hints: Array<[string, string]> }) {
  return (
    <box flexDirection="row" gap={2} height={1} flexShrink={0} paddingX={1}>
      {hints.map(([key, label]) => (
        <text key={key}>
          <span fg={theme.accent} attributes={1}>
            {key}
          </span>
          <span fg={theme.muted}> {label}</span>
        </text>
      ))}
    </box>
  )
}

export function ProgressBar({
  value,
  total,
  width = 24,
  color = theme.accent,
}: {
  value: number
  total: number
  width?: number
  color?: string
}) {
  const safeTotal = Math.max(total, 1)
  const ratio = Math.max(0, Math.min(1, value / safeTotal))
  const filled = Math.round(ratio * width)
  return (
    <text>
      <span fg={color}>{"█".repeat(filled)}</span>
      <span fg={theme.borderBright}>{"░".repeat(width - filled)}</span>
      <span fg={theme.muted}>{`  ${value}/${total}`}</span>
    </text>
  )
}

const frames = ["◐", "◓", "◑", "◒"]

export function Spinner({ color = theme.accent }: { color?: string }) {
  const [frame, setFrame] = useState(0)
  useEffect(() => {
    const timer = setInterval(() => setFrame((value) => (value + 1) % frames.length), 140)
    return () => clearInterval(timer)
  }, [])
  return <text fg={color}>{frames[frame]}</text>
}

export function Modal({
  title,
  children,
  width = 68,
  borderColor = theme.accent,
  footer,
}: {
  title: string
  children: ReactNode
  width?: number
  borderColor?: string
  footer?: ReactNode
}) {
  return (
    <box
      position="absolute"
      top={0}
      left={0}
      width="100%"
      height="100%"
      zIndex={100}
      backgroundColor={theme.bg}
      justifyContent="center"
      alignItems="center"
    >
      <box
        width={width}
        flexDirection="column"
        backgroundColor={theme.surface}
        border
        borderStyle="double"
        borderColor={borderColor}
        title={` ${title} `}
        titleColor={borderColor}
        padding={1}
        gap={1}
      >
        {children}
        {footer ? (
          <>
            <HLine />
            {footer}
          </>
        ) : null}
      </box>
    </box>
  )
}

export function EmptyState({ title, hint }: { title: string; hint?: string }) {
  return (
    <box flexGrow={1} justifyContent="center" alignItems="center" flexDirection="column" gap={1}>
      <text fg={theme.dim}>{title}</text>
      {hint ? <text fg={theme.dim}>{hint}</text> : null}
    </box>
  )
}

export function Field({
  label,
  value,
  hint,
}: {
  label: string
  value: string
  hint?: string
}) {
  return (
    <box flexDirection="column" gap={0}>
      <text fg={theme.muted}>{label}</text>
      <text fg={theme.text} wrapMode="char">
        {value}
      </text>
      {hint ? <text fg={theme.dim}>{hint}</text> : null}
    </box>
  )
}

export function InfoRow({
  label,
  value,
  tone = "text",
}: {
  label: string
  value: string
  tone?: Tone
}) {
  return (
    <box flexDirection="row" height={1} flexShrink={0}>
      <box width={18} flexShrink={0}>
        <text fg={theme.muted}>{label}</text>
      </box>
      <text fg={theme[tone]} wrapMode="char">
        {value}
      </text>
    </box>
  )
}

export function statusTone(status: string): Tone {
  switch (status) {
    case "ok":
    case "verified":
    case "ready":
      return "green"
    case "failed":
    case "error":
    case "unavailable":
      return "red"
    case "skipped":
      return "yellow"
    case "scan":
    case "run":
      return "accent"
    default:
      return "muted"
  }
}

export function statusGlyph(status: string): string {
  switch (status) {
    case "ok":
    case "verified":
    case "ready":
      return "●"
    case "failed":
    case "error":
    case "unavailable":
      return "×"
    case "skipped":
      return "◌"
    case "scan":
    case "run":
      return "◐"
    default:
      return "○"
  }
}

export type ScreenKeyHandler = (key: KeyEvent) => boolean
