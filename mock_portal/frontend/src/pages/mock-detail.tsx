import * as React from "react"
import {
  AlertTriangle,
  ArrowLeft,
  ChevronDown,
  Clock,
  Download,
  FileText,
  HardDrive,
} from "lucide-react"
import { Link, useParams } from "react-router-dom"

import { CopyButton } from "@/components/copy-button"
import { Markdown } from "@/components/markdown"
import { SourceStatusBadge, StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@/components/ui/collapsible"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  api,
  ApiError,
  type FilesResponse,
  type MockDetail,
  type SummaryResponse,
} from "@/lib/api"
import { displayValue, formatBytes, formatNumber, formatTime, relativeTime } from "@/lib/format"
import { cn } from "@/lib/utils"

type Zone = "utc" | "local"

function FieldGroup({
  title,
  rows,
  className,
}: {
  title?: string
  rows: { label: string; value: React.ReactNode; mono?: boolean }[]
  className?: string
}) {
  return (
    <section className={cn("mb-5", className)}>
      {title && <h2 className="mb-2 text-[13px] font-semibold">{title}</h2>}
      <div className="overflow-hidden rounded-lg border border-border">
        {rows.map((row, index) => (
          <div
            key={`${row.label}-${index}`}
            className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-b border-border bg-background px-3.5 py-2.5 last:border-b-0"
          >
            <span className="shrink-0 text-muted-foreground">{row.label}</span>
            <span className={cn("min-w-0 text-right", row.mono && "mono")}>{row.value}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

function diagValue(diag: Record<string, unknown> | null, key: string): unknown {
  if (!diag) return undefined
  return diag[key]
}

function infoValue(info: Record<string, unknown> | null, key: string): unknown {
  if (!info) return undefined
  return info[key]
}

function DiagnosticTab({ detail, zone }: { detail: MockDetail; zone: Zone }) {
  const diag = detail.detail?.diag ?? detail.diagnostic
  const info = detail.detail?.info ?? null
  const playback = detail.playback ?? []
  const rawDiag = React.useMemo(() => {
    if (!diag) return ""
    try {
      return JSON.stringify(diag, null, 2)
    } catch {
      return ""
    }
  }, [diag])

  const timeCell = (value: unknown) => {
    const text = typeof value === "string" ? value : ""
    if (!text) return <span className="text-muted-foreground">未知</span>
    return <span>{formatTime(text, zone)}</span>
  }

  return (
    <div>
      {detail.endpoint_mismatch && (
        <Alert className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-warn" />
          <div>
            <AlertTitle>源端地址与 config 不一致</AlertTitle>
            <AlertDescription>{detail.endpoint_mismatch}</AlertDescription>
          </div>
        </Alert>
      )}

      {detail.status.state === "unmonitored" && (
        <Alert variant="muted" className="mb-4">
          <AlertTriangle aria-hidden="true" />
          <div>
            <AlertTitle>未监控</AlertTitle>
            <AlertDescription>
              该 preset 未配置 diagnostic.listen，门户无法采集诊断信息，也不猜测 UA 端口。说明与下载不受影响。
            </AlertDescription>
          </div>
        </Alert>
      )}

      {detail.status.state === "misconfigured" && (
        <Alert className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-warn" />
          <div>
            <AlertTitle>配置待完善</AlertTitle>
            <AlertDescription>{detail.diag.reason || "诊断地址无法确定，请完善 config.yaml。"}</AlertDescription>
          </div>
        </Alert>
      )}

      {detail.diag.state === "monitored" && detail.detail?.error && (
        <Alert className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-warn" />
          <div>
            <AlertTitle>详情接口暂不可用</AlertTitle>
            <AlertDescription>{detail.detail.error}{diag || info ? "（显示最近一次成功快照）" : "（尚无成功快照）"}</AlertDescription>
          </div>
        </Alert>
      )}

      <FieldGroup
        title="门户采集状态"
        rows={[
          { label: "采集状态", value: <StatusBadge status={detail.status} /> },
          {
            label: "最近尝试",
            value: detail.status.last_attempt ? (
              <span>
                {formatTime(detail.status.last_attempt, zone)}
                <span className="ml-2 text-[11px] text-muted-foreground">
                  {relativeTime(detail.status.last_attempt)}
                </span>
              </span>
            ) : (
              <span className="text-muted-foreground">尚未采集</span>
            ),
          },
          {
            label: "最后成功",
            value: detail.status.last_success ? (
              formatTime(detail.status.last_success, zone)
            ) : (
              <span className="text-muted-foreground">尚无成功快照</span>
            ),
          },
          {
            label: "已采集快照时间",
            value: detail.diagnostic_fetched_at ? (
              formatTime(detail.diagnostic_fetched_at, zone)
            ) : (
              <span className="text-muted-foreground">无</span>
            ),
          },
          { label: "连续失败次数", value: formatNumber(detail.status.consecutive_failures) },
          ...(detail.status.last_error
            ? [{ label: "最近错误", value: <span className="text-bad">{detail.status.last_error}</span> }]
            : []),
          { label: "采集地址", value: detail.diag.url || detail.diag.reason || "未监控", mono: true },
        ]}
      />

      {!diag && !info && (
        <p className="mb-5 rounded-lg border border-border bg-muted/30 px-4 py-5 text-[13px] text-muted-foreground" role="status">
          尚无成功诊断快照。连接与播放配置见下方，数据特征及文件请查看「说明」「下载」。
        </p>
      )}
      {(diag || info) && <>
      <FieldGroup
        title="源端运行状态（原样保留）"
        rows={[
          { label: "status", value: displayValue(diagValue(diag, "status")) },
          {
            label: "message",
            value: <span className="text-left">{displayValue(diagValue(diag, "message"))}</span>,
          },
          { label: "runtime.phase", value: displayValue(diagValue(diag, "runtime.phase")) },
          { label: "程序版本", value: displayValue(infoValue(info, "version")) },
          { label: "位号数", value: formatNumber(infoValue(info, "tag.count")) },
          {
            label: "本次启动导入历史点数",
            value: formatNumber(infoValue(info, "hda.imported_samples")),
          },
        ]}
      />

      <p className="mb-5 -mt-3 text-[11px] text-muted-foreground">
        hda.imported_samples 可能只代表本次启动的导入进度，不等于数据库当前全部历史点数。
      </p>

      <FieldGroup
        title="历史窗口"
        rows={[
          {
            label: "原始历史窗口",
            value:
              infoValue(info, "hda.native.start_time") || infoValue(info, "hda.native.end_time") ? (
                <span>
                  {timeCell(infoValue(info, "hda.native.start_time"))} →{" "}
                  {timeCell(infoValue(info, "hda.native.end_time"))}
                </span>
              ) : (
                <span className="text-muted-foreground">未知</span>
              ),
          },
          {
            label: "实时追加窗口",
            value:
              infoValue(info, "hda.live.start_time") || infoValue(info, "hda.live.end_time") ? (
                <span>
                  {timeCell(infoValue(info, "hda.live.start_time"))} →{" "}
                  {timeCell(infoValue(info, "hda.live.end_time"))}
                </span>
              ) : (
                <span className="text-muted-foreground">未知</span>
              ),
          },
        ]}
      />

      <FieldGroup
        title="最近一轮处理"
        rows={[
          { label: "DA 最近处理时间", value: timeCell(diagValue(diag, "da.last_time")) },
          {
            label: "DA 处理数量",
            value: `${formatNumber(diagValue(diag, "da.tag_count"))} / ${formatNumber(
              diagValue(diag, "da.expected_count"),
            )}`,
          },
          { label: "HDA 最近处理时间", value: timeCell(diagValue(diag, "hda.last_time")) },
          {
            label: "HDA 处理数量",
            value: `${formatNumber(diagValue(diag, "hda.tag_count"))} / ${formatNumber(
              diagValue(diag, "hda.expected_count"),
            )}`,
          },
        ]}
      />

      <FieldGroup
        title="OPC UA"
        rows={[
          { label: "连接数", value: formatNumber(diagValue(diag, "opcua.connection_count")) },
          { label: "会话数", value: formatNumber(diagValue(diag, "opcua.session_count")) },
          { label: "订阅数", value: formatNumber(diagValue(diag, "opcua.subscription_count")) },
          { label: "监控项", value: formatNumber(diagValue(diag, "opcua.monitored_item_count")) },
          { label: "待发通知", value: formatNumber(diagValue(diag, "opcua.notification_pending")) },
          {
            label: "通知合并累计",
            value: formatNumber(diagValue(diag, "opcua.notification_coalesced_total")),
          },
          {
            label: "最近合并时间",
            value: timeCell(diagValue(diag, "opcua.notification_coalesced_last_time")),
          },
        ]}
      />

      </>}
      <FieldGroup
        title="连接与播放配置（来自当前 config.yaml）"
        rows={[
          {
            label: "server.endpoint",
            value: <span className="mono">{detail.ua.raw || "未配置"}</span>,
          },
          {
            label: "server.namespace",
            value: detail.namespace ? <span className="mono">{detail.namespace}</span> : "未配置",
          },
          { label: "server.ns", value: detail.ns === undefined ? "未配置" : formatNumber(detail.ns) },
          {
            label: "播放周期",
            value:
              playback.length === 0 ? (
                <span className="text-muted-foreground">未配置 playback.files</span>
              ) : (
                <span className="flex flex-col items-end gap-0.5">
                  {playback.map((file) => (
                    <span key={file.name} className="mono">
                      {file.name}: {file.period_ms} ms
                    </span>
                  ))}
                </span>
              ),
          },
          {
            label: "history.retention_days",
            value: formatNumber(detail.retention_days),
          },
        ]}
      />

      <div className="mt-6 flex flex-col gap-3">
        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button variant="outline" size="sm" className="w-fit">
              <ChevronDown aria-hidden="true" />
              查看 config.yaml（只读）
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-3">
            <pre className="mono overflow-x-auto rounded-lg border border-border bg-secondary p-3.5 leading-relaxed">
              {detail.config_text || "config.yaml 内容不可用"}
            </pre>
          </CollapsibleContent>
        </Collapsible>

        <Collapsible>
          <CollapsibleTrigger asChild>
            <Button variant="outline" size="sm" className="w-fit">
              <ChevronDown aria-hidden="true" />
              原始诊断字段
            </Button>
          </CollapsibleTrigger>
          <CollapsibleContent className="mt-3">
            <pre className="mono overflow-x-auto rounded-lg border border-border bg-secondary p-3.5 leading-relaxed">
              {rawDiag || "尚无诊断快照"}
            </pre>
          </CollapsibleContent>
        </Collapsible>
      </div>

      <p className="mt-5 text-[11px] text-muted-foreground">
        源端 status 原样保留；门户采集状态与源端运行状态分开。稀疏数据短时间无更新、仅 HDA 无 DA、
        通知合并都不能直接判定历史丢点。时间默认 UTC，切换时区不改变数据。
      </p>
    </div>
  )
}

function SummaryTab({ id }: { id: string }) {
  const [state, setState] = React.useState<{
    data: SummaryResponse | null
    loading: boolean
    error: string | null
  }>({ data: null, loading: true, error: null })

  React.useEffect(() => {
    const controller = new AbortController()
    setState({ data: null, loading: true, error: null })
    api
      .summary(id, controller.signal)
      .then((data) => setState({ data, loading: false, error: null }))
      .catch((err: unknown) => {
        if ((err as Error)?.name === "AbortError") return
        setState({ data: null, loading: false, error: err instanceof Error ? err.message : "读取失败" })
      })
    return () => controller.abort()
  }, [id])

  if (state.loading) {
    return (
      <div className="flex flex-col gap-2">
        {[0, 1, 2, 3].map((index) => (
          <Skeleton key={index} className="h-6 w-full" />
        ))}
      </div>
    )
  }

  if (state.data?.error || state.error) {
    return (
      <Alert variant="muted">
        <FileText aria-hidden="true" />
        <div>
          <AlertTitle>缺少 summary.md</AlertTitle>
          <AlertDescription>{state.data?.error || state.error}</AlertDescription>
        </div>
      </Alert>
    )
  }

  return (
    <div>
      <div className="mb-3 flex flex-wrap items-center gap-2 text-[11px] text-muted-foreground">
        <Badge variant="outline">
          <FileText aria-hidden="true" className="size-3" />
          summary.md
        </Badge>
        <span>最后修改 {formatTime(state.data?.modified_at, "utc")}</span>
      </div>
      <div className="rounded-lg border border-border bg-background px-5 py-4">
        <Markdown>{state.data?.markdown ?? ""}</Markdown>
      </div>
      <p className="mt-3 text-[11px] text-muted-foreground">
        该文件中的配置示例是编写时快照；连接地址和诊断地址以当前 config.yaml 为准。
      </p>
    </div>
  )
}

const categoryLabels: Record<string, string> = {
  hda: "HDA 历史数据",
  da: "DA 实时数据",
  config: "配置",
  summary: "说明",
}

function FilesTab({ id, fallbackCounts }: { id: string; fallbackCounts: { hda: number; da: number } }) {
  const [state, setState] = React.useState<{
    data: FilesResponse | null
    loading: boolean
    error: string | null
  }>({ data: null, loading: true, error: null })

  React.useEffect(() => {
    const controller = new AbortController()
    setState({ data: null, loading: true, error: null })
    api
      .files(id, controller.signal)
      .then((data) => setState({ data, loading: false, error: null }))
      .catch((err: unknown) => {
        if ((err as Error)?.name === "AbortError") return
        setState({ data: null, loading: false, error: err instanceof Error ? err.message : "读取失败" })
      })
    return () => controller.abort()
  }, [id])

  if (state.loading) {
    return (
      <div className="flex flex-col gap-2">
        {[0, 1, 2].map((index) => (
          <Skeleton key={index} className="h-12 w-full" />
        ))}
      </div>
    )
  }

  if (state.error || !state.data) {
    return (
      <Alert variant="bad">
        <AlertTriangle aria-hidden="true" className="text-bad" />
        <div>
          <AlertTitle>文件列表读取失败</AlertTitle>
          <AlertDescription>{state.error}</AlertDescription>
        </div>
      </Alert>
    )
  }

  const groups = ["hda", "da", "config", "summary"] as const
  const files = state.data.files

  return (
    <div>
      {(state.data.da_count === 0 && (state.data.hda_count > 0 || fallbackCounts.hda > 0)) && (
        <Alert variant="muted" className="mb-4">
          <HardDrive aria-hidden="true" />
          <div>
            <AlertTitle>仅 HDA，无 DA 文件</AlertTitle>
            <AlertDescription>
              该 preset 只有历史数据，实时轮播无 DA 来源。不能因为没有 DA 推进就判定历史冻结。
            </AlertDescription>
          </div>
        </Alert>
      )}

      {files.length === 0 ? (
        <Alert variant="muted">
          <HardDrive aria-hidden="true" />
          <div>
            <AlertTitle>没有可下载文件</AlertTitle>
            <AlertDescription>hda/、da/ 下没有第一层 .parquet 文件，也未找到 config.yaml 或 summary.md。</AlertDescription>
          </div>
        </Alert>
      ) : (
        groups.map((category) => {
          const groupFiles = files.filter((file) => file.category === category)
          if (groupFiles.length === 0) return null
          return (
            <section key={category} className="mb-5">
              <h2 className="mb-2 text-[13px] font-semibold">{categoryLabels[category]}</h2>
              <div className="overflow-hidden rounded-lg border border-border">
                {groupFiles.map((file) => (
                  <div
                    key={file.id}
                    className="flex flex-wrap items-center justify-between gap-3 border-b border-border bg-background px-3.5 py-3 last:border-b-0"
                  >
                    <div className="flex min-w-0 items-center gap-2.5">
                      <FileText aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
                      <div className="min-w-0">
                        <div className="mono truncate">{file.name}</div>
                        <div className="text-[11px] text-muted-foreground">
                          {formatBytes(file.size)} · {formatTime(file.modified_at, "utc")}
                        </div>
                      </div>
                    </div>
                    <Button asChild variant="outline" size="sm">
                      <a href={api.downloadURL(id, file.id)} download={file.name}>
                        <Download aria-hidden="true" />
                        下载
                      </a>
                    </Button>
                  </div>
                ))}
              </div>
            </section>
          )
        })
      )}

      <p className="text-[11px] text-muted-foreground">
        只列出 hda/、da/ 第一层的 .parquet 文件以及 config.yaml、summary.md。门户不解析
        Parquet 内容，也不发布 runtime、源数据或任意路径文件。
      </p>
    </div>
  )
}

function AddressSummary({ detail, zone }: { detail: MockDetail; zone: Zone }) {
  const { ua, diag } = detail
  const copyDisabled = !ua.valid || ua.wildcard
  const copyReason = !ua.valid
    ? ua.error || "地址无效，无法复制"
    : ua.wildcard
      ? "监听占位地址，请完善 config 后再复制为可连接地址"
      : undefined

  return (
    <div className="mb-5 flex flex-col gap-2 rounded-lg border border-border bg-secondary/40 px-3.5 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[11px] text-muted-foreground">UA 连接地址</span>
        <span className="mono min-w-0 truncate">{ua.raw || "未配置 server.endpoint"}</span>
        <CopyButton value={ua.raw} disabled={copyDisabled} disabledReason={copyReason} label="复制连接地址" />
        {ua.wildcard && (
          <Badge variant="warn">
            <AlertTriangle aria-hidden="true" className="size-3" />
            监听占位地址，请完善 config
          </Badge>
        )}
        {!ua.valid && ua.error && <Badge variant="bad">{ua.error}</Badge>}
      </div>
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px] text-muted-foreground">
        <span>
          诊断：{diag.state === "monitored" ? <span className="mono">{diag.url}</span> : diag.reason || "未监控"}
        </span>
        <span>Namespace：{detail.namespace || "未配置"}</span>
        {detail.ns !== undefined && <span>ns：{detail.ns}</span>}
        <span className="flex items-center gap-1">
          <Clock aria-hidden="true" className="size-3" />
          快照 {detail.status.last_success ? formatTime(detail.status.last_success, zone) : "尚无"}
        </span>
      </div>
      {diag.state === "monitored" && diag.loopback && (
        <span className="text-[11px] text-warn">
          诊断地址为环回地址，仅本机可访问；跨主机部署时无法从其他机器采集。
        </span>
      )}
    </div>
  )
}

export function MockDetailPage() {
  const { id = "" } = useParams()
  return <MockDetailContent key={id} id={id} />
}

function MockDetailContent({ id }: { id: string }) {
  const [detail, setDetail] = React.useState<MockDetail | null>(null)
  const [error, setError] = React.useState<string | null>(null)
  const [notFound, setNotFound] = React.useState(false)
  const [retry, setRetry] = React.useState(0)
  const [tab, setTab] = React.useState("diagnostic")
  const [zone, setZone] = React.useState<Zone>(() => {
    const stored = window.localStorage?.getItem("mock-portal-zone")
    return stored === "local" ? "local" : "utc"
  })

  React.useEffect(() => {
    let active = true
    let busy = false
    const controller = new AbortController()
    const load = async () => {
      if (busy) return
      busy = true
      try {
        const payload = await api.detail(id, controller.signal)
        if (!active) return
        setDetail(payload)
        setError(null)
        setNotFound(false)
      } catch (err) {
        if (!active) return
        const message = err instanceof Error ? err.message : "详情加载失败"
        setError(message)
        setNotFound(err instanceof ApiError && err.status === 404)
      } finally {
        busy = false
      }
    }
    void load()
    const timer = window.setInterval(() => void load(), 10000)
    return () => {
      active = false
      controller.abort()
      window.clearInterval(timer)
    }
  }, [id, retry])

  React.useEffect(() => {
    window.localStorage?.setItem("mock-portal-zone", zone)
  }, [zone])

  if (notFound) {
    return (
      <Alert variant="bad">
        <AlertTriangle aria-hidden="true" className="text-bad" />
        <div>
          <AlertTitle>未找到该 Mock</AlertTitle>
          <AlertDescription>
            {error}。请返回 <Link to="/" className="text-primary">Mock 列表</Link> 重新选择。
          </AlertDescription>
        </div>
      </Alert>
    )
  }

  if (!detail && error) {
    return (
      <Alert variant="bad">
        <AlertTriangle aria-hidden="true" className="text-bad" />
        <div>
          <AlertTitle>详情加载失败</AlertTitle>
          <AlertDescription>{error}</AlertDescription>
          <div className="mt-3 flex items-center gap-3">
            <Button variant="outline" size="sm" onClick={() => { setError(null); setRetry((value) => value + 1) }}>重试</Button>
            <Link to="/" className="text-primary">返回 Mock 列表</Link>
          </div>
        </div>
      </Alert>
    )
  }

  if (!detail) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-16 w-full" />
        <Skeleton className="h-40 w-full" />
      </div>
    )
  }

  return (
    <div className="min-w-0">
      <Link to="/" className="mb-4 inline-flex items-center gap-1 text-[12px] text-muted-foreground hover:text-foreground">
        <ArrowLeft aria-hidden="true" className="size-3.5" />
        返回 Mock 列表
      </Link>

      <div className="mb-4 flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0">
          <h1 className="text-[22px] font-semibold tracking-tight md:text-[24px]">{detail.id}</h1>
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <StatusBadge status={detail.status} />
            <SourceStatusBadge status={detail.status} />
            {!detail.config_valid && <Badge variant="bad">config 解析失败</Badge>}
            {!detail.summary_exists && <Badge variant="outline">缺少 summary.md</Badge>}
            {detail.ua.wildcard && <Badge variant="warn">占位地址</Badge>}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <span className="text-[11px] text-muted-foreground">时间</span>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setZone((current) => (current === "utc" ? "local" : "utc"))}
          >
            {zone === "utc" ? "UTC" : "本地"}
          </Button>
        </div>
      </div>

      {error && (
        <Alert variant="bad" className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-bad" />
          <div>
            <AlertTitle>刷新失败</AlertTitle>
            <AlertDescription>{error}（显示最近一次成功快照）</AlertDescription>
          </div>
        </Alert>
      )}

      {detail.config_error && (
        <Alert className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-warn" />
          <div>
            <AlertTitle>config.yaml 解析失败</AlertTitle>
            <AlertDescription>{detail.config_error}</AlertDescription>
          </div>
        </Alert>
      )}

      <AddressSummary detail={detail} zone={zone} />

      <Tabs value={tab} onValueChange={setTab}>
        <TabsList>
          <TabsTrigger value="diagnostic">诊断</TabsTrigger>
          <TabsTrigger value="summary">说明</TabsTrigger>
          <TabsTrigger value="files">下载</TabsTrigger>
        </TabsList>
        <TabsContent value="diagnostic">
          <DiagnosticTab detail={detail} zone={zone} />
        </TabsContent>
        <TabsContent value="summary">
          {tab === "summary" && <SummaryTab id={detail.id} />}
        </TabsContent>
        <TabsContent value="files">
          {tab === "files" && (
            <FilesTab id={detail.id} fallbackCounts={{ hda: detail.hda_count, da: detail.da_count }} />
          )}
        </TabsContent>
      </Tabs>
    </div>
  )
}
