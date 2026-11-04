import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process"
import { createInterface } from "node:readline"

type Pending = {
  resolve: (value: unknown) => void
  reject: (reason: Error) => void
}

type Reply = {
  id?: number
  ok: boolean
  data?: unknown
  error?: string
}

type EventEnvelope = {
  event: string
  data: unknown
}

export type BridgeEventListener = (event: string, data: unknown) => void

/**
 * BridgeClient owns the JSONL process that hosts the Go engine. It is the only
 * place that talks to `ditch-windows bridge`; screens call request().
 */
export class BridgeClient {
  private child: ChildProcessWithoutNullStreams
  private pending = new Map<number, Pending>()
  private listeners = new Set<BridgeEventListener>()
  private nextID = 1
  private stderr = ""
  private closed = false

  constructor() {
    const executable = process.env.DITCH_BRIDGE
    if (!executable) {
      throw new Error("DITCH_BRIDGE is not set. Start the UI through ditch-windows tui.")
    }
    let args: string[] = []
    const rawArgs = process.env.DITCH_BRIDGE_ARGS_JSON
    if (rawArgs) {
      try {
        const parsed = JSON.parse(rawArgs)
        if (Array.isArray(parsed)) args = parsed.map(String)
      } catch {
        throw new Error("DITCH_BRIDGE_ARGS_JSON is not valid JSON")
      }
    }
    this.child = spawn(executable, args, {
      stdio: ["pipe", "pipe", "pipe"],
      env: process.env,
      windowsHide: true,
    })

    const lines = createInterface({ input: this.child.stdout })
    lines.on("line", (line) => this.handleLine(line))

    this.child.stderr.on("data", (chunk: Buffer) => {
      this.stderr = (this.stderr + chunk.toString()).slice(-8000)
    })
    this.child.on("error", (error) => this.failAll(error))
    this.child.on("exit", (code) => {
      if (this.closed) return
      const detail = this.stderr.trim()
      const suffix = detail ? `\n${detail}` : ""
      this.failAll(new Error(`engine exited with code ${code ?? "unknown"}${suffix}`))
      this.emit("exit", { code })
    })
  }

  request<T>(cmd: string, args: Record<string, unknown> = {}): Promise<T> {
    if (this.closed) return Promise.reject(new Error("engine connection is closed"))
    const id = this.nextID++
    const payload = JSON.stringify({ id, cmd, args }) + "\n"
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (value: unknown) => void, reject })
      this.child.stdin.write(payload, (error) => {
        if (error) {
          this.pending.delete(id)
          reject(error)
        }
      })
    })
  }

  onEvent(listener: BridgeEventListener): () => void {
    this.listeners.add(listener)
    return () => this.listeners.delete(listener)
  }

  close(): void {
    if (this.closed) return
    this.closed = true
    try {
      this.child.stdin.write(JSON.stringify({ id: 0, cmd: "quit", args: {} }) + "\n")
      this.child.stdin.end()
    } catch {
      // already gone
    }
    setTimeout(() => {
      if (!this.child.killed) this.child.kill()
    }, 400).unref()
  }

  private handleLine(line: string): void {
    const trimmed = line.trim()
    if (!trimmed) return
    let message: Reply | EventEnvelope
    try {
      message = JSON.parse(trimmed)
    } catch {
      return
    }
    if ("event" in message) {
      this.emit(message.event, message.data)
      return
    }
    const reply = message as Reply
    if (typeof reply.id !== "number") return
    const pending = this.pending.get(reply.id)
    if (!pending) return
    this.pending.delete(reply.id)
    if (reply.ok) {
      pending.resolve(reply.data)
    } else {
      pending.reject(new Error(reply.error || "engine request failed"))
    }
  }

  private emit(event: string, data: unknown): void {
    for (const listener of this.listeners) listener(event, data)
  }

  private failAll(error: Error): void {
    for (const pending of this.pending.values()) pending.reject(error)
    this.pending.clear()
  }
}
