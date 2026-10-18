import { AlertTriangle, RefreshCw, ServerCog } from "lucide-react"
import { Link } from "react-router-dom"

import { CopyButton } from "@/components/copy-button"
import { SourceStatusBadge, StatusBadge } from "@/components/status-badge"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useMocks } from "@/data/mocks"
import { formatTime, relativeTime } from "@/lib/format"
import type { MockView } from "@/lib/api"
import { cn } from "@/lib/utils"

function AddressCell({ mock }: { mock: MockView }) {
  const { ua } = mock
  if (!ua.valid) {
    return (
      <span className="flex flex-col gap-1">
        <span className="mono text-muted-foreground">{ua.raw || "未配置"}</span>
        <span className="text-[11px] text-bad">{ua.error || "地址无效"}</span>
      </span>
    )
  }
  if (ua.wildcard) {
    return (
      <span className="flex flex-col gap-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="mono truncate">{ua.raw}</span>
          <CopyButton
            value={ua.raw}
            disabled
            disabledReason="监听占位地址，请完善 config 后再复制为可连接地址"
            label="复制连接地址"
          />
        </span>
        <Tooltip>
          <TooltipTrigger asChild>
            <span className="inline-flex w-fit items-center gap-1 text-[11px] text-warn">
              <AlertTriangle aria-hidden="true" className="size-3" />
              监听占位地址，请完善 config
            </span>
          </TooltipTrigger>
          <TooltipContent>
            server.endpoint 仍是 0.0.0.0 样例。请部署人员在 preset 的 config.yaml 中填写本机真实 IP 与端口。
          </TooltipContent>
        </Tooltip>
      </span>
    )
  }
  return (
    <span className="flex min-w-0 items-center gap-1.5">
      <span className="mono truncate">{ua.raw}</span>
      <CopyButton value={ua.raw} label="复制连接地址" />
    </span>
  )
}

function DiagnosticCell({ mock }: { mock: MockView }) {
  return (
    <span className="flex flex-col gap-1">
      <span className="flex flex-wrap items-center gap-1.5">
        <StatusBadge status={mock.status} />
        <SourceStatusBadge status={mock.status} />
      </span>
      <span className="text-[11px] text-muted-foreground">
        最近尝试 {relativeTime(mock.status.last_attempt)}
      </span>
    </span>
  )
}

export function MockListPage() {
  const { data, loading, error, refresh, refreshing } = useMocks()

  return (
    <div className="min-w-0">
      <div className="mb-6 flex flex-wrap items-start justify-between gap-3">
        <div>
          <h1 className="text-[22px] font-semibold tracking-tight md:text-[24px]">Mock 列表</h1>
          <p className="mt-1 text-[12.5px] text-muted-foreground">
            {data
              ? `共 ${data.total} 个 Mock，按 presets 第一层目录统计。`
              : "正在读取 presets 目录…"}
            地址与状态来自各 preset 的 config.yaml 与诊断缓存，门户不启动或停止 Mock。
          </p>
        </div>
        <Button variant="default" onClick={() => refresh(true)} disabled={refreshing}>
          <RefreshCw aria-hidden="true" className={cn(refreshing && "animate-spin")} />
          刷新
        </Button>
      </div>

      {error && (
        <Alert variant="bad" className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-bad" />
          <div>
            <AlertTitle>列表读取失败</AlertTitle>
            <AlertDescription>
              {error}
              {data?.scanned_at ? `；当前显示 ${formatTime(data.scanned_at, "utc")} 的旧清单。` : "。"}
            </AlertDescription>
          </div>
        </Alert>
      )}

      {data?.stale && !error && (
        <Alert variant="warn" className="mb-4">
          <AlertTriangle aria-hidden="true" className="text-warn" />
          <div>
            <AlertTitle>清单已过期</AlertTitle>
            <AlertDescription>{data.error || "presets 根目录读取失败，显示上次成功清单。"}</AlertDescription>
          </div>
        </Alert>
      )}

      {loading && !data ? (
        <div className="flex flex-col gap-2">
          {[0, 1, 2].map((index) => (
            <Skeleton key={index} className="h-14 w-full" />
          ))}
        </div>
      ) : !data || data.total === 0 ? (
        <Alert variant="muted">
          <ServerCog aria-hidden="true" />
          <div>
            <AlertTitle>没有发现 Mock</AlertTitle>
            <AlertDescription>
              {error
                ? "无法读取 presets 根目录，暂不显示数量。"
                : "presets 下没有包含 config.yaml 的目录。带 config.yaml 的第一层子目录才会被统计。"}
            </AlertDescription>
          </div>
        </Alert>
      ) : (
        <>
          <div className="hidden md:block">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-[20%] pl-0">Mock</TableHead>
                  <TableHead className="w-[26%]">UA 连接地址</TableHead>
                  <TableHead className="w-[24%]">诊断</TableHead>
                  <TableHead className="w-[18%]">最近成功采集</TableHead>
                  <TableHead className="w-[12%] pr-0 text-right" />
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.mocks.map((mock) => (
                  <TableRow key={mock.id} interactive>
                    <TableCell className="pl-0">
                      <div className="font-medium">{mock.id}</div>
                      <div className="text-[11px] text-muted-foreground">
                        {mock.namespace ? `Namespace ${mock.namespace}` : "未配置 Namespace"}
                        {` · HDA ${mock.hda_count} 个文件 / DA ${mock.da_count} 个文件`}
                      </div>
                    </TableCell>
                    <TableCell>
                      <AddressCell mock={mock} />
                    </TableCell>
                    <TableCell>
                      <DiagnosticCell mock={mock} />
                    </TableCell>
                    <TableCell className="text-[12px]">
                      {mock.status.last_success ? (
                        <span className="flex flex-col gap-0.5">
                          <span>{formatTime(mock.status.last_success, "utc")}</span>
                          <span className="text-[11px] text-muted-foreground">
                            {relativeTime(mock.status.last_success)}
                          </span>
                        </span>
                      ) : (
                        <span className="text-muted-foreground">尚无成功快照</span>
                      )}
                    </TableCell>
                    <TableCell className="pr-0 text-right">
                      <Button asChild variant="outline" size="sm">
                        <Link to={`/mocks/${encodeURIComponent(mock.id)}`}>查看</Link>
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>

          <ul className="flex list-none flex-col gap-2 p-0 md:hidden">
            {data.mocks.map((mock) => (
              <li key={mock.id} className="rounded-lg border border-border p-3.5">
                <div className="flex items-start justify-between gap-2">
                  <div className="min-w-0">
                    <div className="font-medium">{mock.id}</div>
                    <div className="text-[11px] text-muted-foreground">
                      {mock.namespace ? `Namespace ${mock.namespace}` : "未配置 Namespace"}
                    </div>
                  </div>
                  <Button asChild variant="outline" size="sm">
                    <Link to={`/mocks/${encodeURIComponent(mock.id)}`}>查看</Link>
                  </Button>
                </div>
                <div className="mt-3 flex flex-col gap-2 text-[12px]">
                  <div>
                    <div className="text-[11px] text-muted-foreground">UA 连接地址</div>
                    <AddressCell mock={mock} />
                  </div>
                  <div>
                    <div className="text-[11px] text-muted-foreground">诊断</div>
                    <DiagnosticCell mock={mock} />
                  </div>
                  <div>
                    <div className="text-[11px] text-muted-foreground">最近成功采集</div>
                    <div>{mock.status.last_success ? formatTime(mock.status.last_success, "utc") : "尚无成功快照"}</div>
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </>
      )}

      {data?.scanned_at && (
        <p className="mt-4 text-[11px] text-muted-foreground">
          目录扫描于 {formatTime(data.scanned_at, "utc")}（UTC）。刷新按钮只重新读取门户缓存与文件，不操作 Mock。
        </p>
      )}
    </div>
  )
}
