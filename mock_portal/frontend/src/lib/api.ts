export type EndpointView = {
  raw: string
  valid: boolean
  host: string
  port: number
  wildcard: boolean
  loopback: boolean
  error?: string
}

export type DiagTargetView = {
  listen: string
  url: string
  host: string
  port: number
  state: "monitored" | "unmonitored" | "incomplete" | "invalid"
  loopback: boolean
  reason?: string
}

export type CollectionState =
  | "pending"
  | "ok"
  | "degraded"
  | "stale"
  | "unreachable"
  | "unmonitored"
  | "misconfigured"

export type StatusView = {
  state: CollectionState
  source_status: string
  source_message: string
  last_attempt?: string
  last_success?: string
  last_error?: string
  consecutive_failures: number
  fetched_at?: string
  reason?: string
}

export type PlaybackFile = { name: string; period_ms: number }

export type MockView = {
  id: string
  namespace: string
  ns?: number
  ua: EndpointView
  diag: DiagTargetView
  config_valid: boolean
  config_error?: string
  config_modified_at?: string
  summary_exists: boolean
  summary_modified_at?: string
  summary_size?: number
  playback: PlaybackFile[] | null
  hda_count: number
  da_count: number
  retention_days: number
  status: StatusView
}

export type MockList = {
  total: number
  scanned_at: string
  stale: boolean
  error?: string
  mocks: MockView[]
}

export type DetailResult = {
  info: Record<string, unknown> | null
  diag: Record<string, unknown> | null
  fetched_at?: string
  error?: string
}

export type MockDetail = MockView & {
  config_text: string
  diagnostic: Record<string, unknown> | null
  diagnostic_fetched_at?: string
  detail: DetailResult
  endpoint_mismatch?: string
}

export type SummaryResponse = {
  id: string
  markdown: string
  modified_at?: string
  size?: number
  error?: string
}

export type FileView = {
  id: string
  name: string
  category: "hda" | "da" | "config" | "summary"
  size: number
  modified_at: string
}

export type FilesResponse = {
  id: string
  files: FileView[]
  hda_count: number
  da_count: number
}

export type GuideSection = { heading: string; body: string }

export type GuideResponse = {
  title: string
  intro: string
  sections: GuideSection[]
  bundle: {
    name: string
    url: string
    size: number
    contents: { path: string; size: number }[]
  }
}

export class ApiError extends Error {
  status: number
  constructor(message: string, status: number) {
    super(message)
    this.status = status
  }
}

async function request<T>(path: string, signal?: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal, headers: { Accept: "application/json" } })
  const text = await response.text()
  let payload: unknown = null
  if (text) {
    try {
      payload = JSON.parse(text)
    } catch {
      payload = null
    }
  }
  if (!response.ok) {
    const message =
      payload && typeof payload === "object" && "error" in payload
        ? String((payload as { error: unknown }).error)
        : `请求失败（HTTP ${response.status}）`
    throw new ApiError(message, response.status)
  }
  if (payload === null) {
    throw new ApiError("响应不是有效 JSON", response.status)
  }
  return payload as T
}

export const api = {
  list: (signal?: AbortSignal, refresh = false) =>
    request<MockList>(`/api/mocks${refresh ? "?refresh=1" : ""}`, signal),
  detail: (id: string, signal?: AbortSignal) =>
    request<MockDetail>(`/api/mocks/${encodeURIComponent(id)}`, signal),
  summary: (id: string, signal?: AbortSignal) =>
    request<SummaryResponse>(`/api/mocks/${encodeURIComponent(id)}/summary`, signal),
  files: (id: string, signal?: AbortSignal) =>
    request<FilesResponse>(`/api/mocks/${encodeURIComponent(id)}/files`, signal),
  guide: (signal?: AbortSignal) => request<GuideResponse>("/api/standardization", signal),
  downloadURL: (id: string, fileId: string) =>
    `/api/mocks/${encodeURIComponent(id)}/files/${encodeURIComponent(fileId)}`,
}
