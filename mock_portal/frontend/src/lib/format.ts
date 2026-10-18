export function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value < 0) return "—"
  if (value < 1024) return `${value} B`
  const units = ["KB", "MB", "GB", "TB"]
  let size = value / 1024
  let index = 0
  while (size >= 1024 && index < units.length - 1) {
    size /= 1024
    index += 1
  }
  return `${size.toFixed(size < 10 ? 1 : 0)} ${units[index]}`
}

function parse(value?: string): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

function pad(value: number): string {
  return value.toString().padStart(2, "0")
}

/** Portal times are UTC by default; every rendered timestamp carries its zone. */
export function formatTime(value: string | undefined, zone: "utc" | "local"): string {
  const date = parse(value)
  if (!date) return "—"
  if (zone === "utc") {
    return (
      `${date.getUTCFullYear()}-${pad(date.getUTCMonth() + 1)}-${pad(date.getUTCDate())} ` +
      `${pad(date.getUTCHours())}:${pad(date.getUTCMinutes())}:${pad(date.getUTCSeconds())} UTC`
    )
  }
  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())} 本地`
  )
}

export function relativeTime(value: string | undefined, now: number = Date.now()): string {
  const date = parse(value)
  if (!date) return "—"
  const diff = Math.round((now - date.getTime()) / 1000)
  if (diff < 0) return "刚刚"
  if (diff < 60) return `${diff} 秒前`
  if (diff < 3600) return `${Math.floor(diff / 60)} 分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)} 小时前`
  return `${Math.floor(diff / 86400)} 天前`
}

export function formatNumber(value: unknown): string {
  if (value === null || value === undefined || value === "") return "—"
  const numeric = typeof value === "number" ? value : Number(value)
  if (!Number.isFinite(numeric)) return String(value)
  return new Intl.NumberFormat("zh-CN").format(numeric)
}

/** Returns the raw string for unknown-shaped diagnostic fields; never invents a value. */
export function displayValue(value: unknown): string {
  if (value === null || value === undefined) return "未知"
  if (typeof value === "string") return value === "" ? "—" : value
  if (typeof value === "number" || typeof value === "boolean") return String(value)
  return JSON.stringify(value)
}
