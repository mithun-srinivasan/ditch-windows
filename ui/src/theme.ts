export const theme = {
  bg: "#07080A",
  surface: "#0E1116",
  surfaceAlt: "#141922",
  border: "#1F2630",
  borderBright: "#2B3542",
  text: "#E7E9EC",
  muted: "#8A94A6",
  dim: "#5A6472",
  accent: "#FF6A3D",
  accentSoft: "#3A1D14",
  mint: "#5EEAD4",
  green: "#3DDC97",
  yellow: "#F5C542",
  red: "#FF5C5C",
  blue: "#6EA8FE",
} as const

export type Tone = "text" | "muted" | "dim" | "accent" | "mint" | "green" | "yellow" | "red" | "blue"

export function toneColor(tone: Tone): string {
  return theme[tone]
}
