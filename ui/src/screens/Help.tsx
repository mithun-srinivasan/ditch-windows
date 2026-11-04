import { theme } from "../theme"
import { HLine, Modal, Tag } from "../components/primitives"

export function Help({ onClose }: { onClose: () => void }) {
  return (
    <Modal
      title="CONTROL ROOM"
      width={78}
      borderColor={theme.mint}
      footer={
        <box flexDirection="row" justifyContent="space-between" width="100%">
          <text fg={theme.dim}>esc / ? closes this panel</text>
          <text fg={theme.accent} onMouseDown={onClose}>
            close
          </text>
        </box>
      }
    >
      <text fg={theme.text} attributes={1}>
        DITCH // WINDOWS 11 REBUILD KIT
      </text>
      <text fg={theme.muted} wrapMode="word">
        Capture the parts of Windows that make it yours, then rebuild them after a wipe, upgrade, or a machine that
        stopped cooperating.
      </text>
      <HLine />
      <box flexDirection="row" gap={2}>
        <box width={34} flexDirection="column" gap={0}>
          <text fg={theme.dim}>NAVIGATION</text>
          <Shortcut keys="g / 1" label="dashboard" />
          <Shortcut keys="b / 2" label="backup" />
          <Shortcut keys="r / 3" label="restore" />
          <Shortcut keys="l / 4" label="archive" />
          <Shortcut keys="v / 5" label="verify" />
          <Shortcut keys="s / 6" label="settings" />
          <Shortcut keys="tab" label="cycle screens" />
          <Shortcut keys="q" label="quit" />
        </box>
        <box flexGrow={1} flexDirection="column" gap={0}>
          <text fg={theme.dim}>IN A LIST</text>
          <Shortcut keys="j / ↓" label="move down" />
          <Shortcut keys="k / ↑" label="move up" />
          <Shortcut keys="space" label="toggle module" />
          <Shortcut keys="enter" label="select / run" />
          <Shortcut keys="esc" label="back" />
          <Shortcut keys="?" label="this help" />
        </box>
      </box>
      <HLine />
      <box flexDirection="row" gap={1}>
        <Tag tone="accent">BACKUP</Tag>
        <text fg={theme.muted} wrapMode="word">
          reads state and writes an encrypted restic snapshot. Read-only.
        </text>
      </box>
      <box flexDirection="row" gap={1}>
        <Tag tone="mint">RESTORE</Tag>
        <text fg={theme.muted} wrapMode="word">
          preview first, then apply. Existing files follow the configured conflict policy.
        </text>
      </box>
      <box flexDirection="row" gap={1}>
        <Tag tone="red">DEMO</Tag>
        <text fg={theme.muted} wrapMode="word">
          simulated modules and a sandbox home. Safe to break.
        </text>
      </box>
    </Modal>
  )
}

function Shortcut({ keys, label }: { keys: string; label: string }) {
  return (
    <box flexDirection="row" height={1}>
      <box width={10}>
        <text fg={theme.accent}>{keys}</text>
      </box>
      <text fg={theme.muted}>{label}</text>
    </box>
  )
}
