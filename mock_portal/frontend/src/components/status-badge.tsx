import { AlertCircle, AlertTriangle, CheckCircle2, CircleDashed, MinusCircle } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import type { CollectionState, StatusView } from "@/lib/api"

const stateMeta: Record<
  CollectionState,
  { label: string; variant: "good" | "warn" | "bad" | "outline"; icon: typeof CheckCircle2 }
> = {
  ok: { label: "采集正常", variant: "good", icon: CheckCircle2 },
  pending: { label: "待检查", variant: "outline", icon: CircleDashed },
  degraded: { label: "采集异常", variant: "warn", icon: AlertTriangle },
  stale: { label: "数据过期", variant: "warn", icon: AlertTriangle },
  unreachable: { label: "不可达", variant: "bad", icon: AlertCircle },
  unmonitored: { label: "未监控", variant: "outline", icon: MinusCircle },
  misconfigured: { label: "配置待完善", variant: "warn", icon: AlertTriangle },
}

export function collectionLabel(state: CollectionState): string {
  return stateMeta[state]?.label ?? state
}

export function StatusBadge({ status }: { status: StatusView }) {
  const meta = stateMeta[status.state] ?? stateMeta.pending
  const Icon = meta.icon
  const badge = (
    <Badge variant={meta.variant}>
      <Icon aria-hidden="true" className="size-3" />
      {meta.label}
    </Badge>
  )
  if (!status.reason && !status.last_error) return badge
  return (
    <Tooltip>
      <TooltipTrigger asChild>{badge}</TooltipTrigger>
      <TooltipContent>{status.reason || status.last_error}</TooltipContent>
    </Tooltip>
  )
}

const sourceMeta: Record<string, { label: string; variant: "good" | "warn" | "bad" | "outline" }> = {
  ok: { label: "源端正常", variant: "good" },
  warn: { label: "源端告警", variant: "warn" },
  error: { label: "源端错误", variant: "bad" },
}

export function SourceStatusBadge({ status }: { status: StatusView }) {
  if (!status.source_status) {
    return <Badge variant="outline">源端未知</Badge>
  }
  const meta = sourceMeta[status.source_status] ?? {
    label: `源端 ${status.source_status}`,
    variant: "outline" as const,
  }
  const badge = (
    <Badge variant={meta.variant}>
      <span aria-hidden="true" className="inline-block size-1.5 rounded-full bg-current" />
      {meta.label}
    </Badge>
  )
  if (!status.source_message) return badge
  return (
    <Tooltip>
      <TooltipTrigger asChild>{badge}</TooltipTrigger>
      <TooltipContent>{status.source_message}</TooltipContent>
    </Tooltip>
  )
}

export function StateDot({ tone }: { tone: "good" | "warn" | "bad" | "muted" }) {
  const color =
    tone === "good" ? "text-good" : tone === "warn" ? "text-warn" : tone === "bad" ? "text-bad" : "text-muted-foreground"
  return <span aria-hidden="true" className={`inline-block size-1.5 rounded-full bg-current ${color}`} />
}
