import * as React from "react"
import { BookOpen, Download, PackageOpen } from "lucide-react"

import { Markdown } from "@/components/markdown"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { api, type GuideResponse } from "@/lib/api"
import { formatBytes } from "@/lib/format"

export function StandardizationPage() {
  const [state, setState] = React.useState<{
    data: GuideResponse | null
    loading: boolean
    error: string | null
  }>({ data: null, loading: true, error: null })

  React.useEffect(() => {
    const controller = new AbortController()
    api
      .guide(controller.signal)
      .then((data) => setState({ data, loading: false, error: null }))
      .catch((err: unknown) => {
        if ((err as Error)?.name === "AbortError") return
        setState({ data: null, loading: false, error: err instanceof Error ? err.message : "读取失败" })
      })
    return () => controller.abort()
  }, [])

  if (state.loading) {
    return (
      <div className="flex flex-col gap-3">
        <Skeleton className="h-8 w-56" />
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-32 w-full" />
      </div>
    )
  }

  if (state.error || !state.data) {
    return (
      <Alert variant="bad">
        <BookOpen aria-hidden="true" className="text-bad" />
        <div>
          <AlertTitle>指南读取失败</AlertTitle>
          <AlertDescription>{state.error}</AlertDescription>
        </div>
      </Alert>
    )
  }

  const guide = state.data

  return (
    <div className="min-w-0 max-w-[860px]">
      <div className="mb-6">
        <h1 className="text-[22px] font-semibold tracking-tight md:text-[24px]">{guide.title}</h1>
        <p className="mt-1 text-[12.5px] text-muted-foreground">{guide.intro}</p>
      </div>

      <section className="mb-6 rounded-lg border border-border bg-background p-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-2.5">
            <PackageOpen aria-hidden="true" className="size-5 text-muted-foreground" />
            <div>
              <div className="mono text-[12.5px]">{guide.bundle.name}</div>
              <div className="text-[11px] text-muted-foreground">
                {guide.bundle.contents.length} 个文件 · {formatBytes(guide.bundle.size)}
              </div>
            </div>
          </div>
          <Button asChild variant="primary">
            <a href={guide.bundle.url} download={guide.bundle.name}>
              <Download aria-hidden="true" />
              下载工具包
            </a>
          </Button>
        </div>
        <div className="mt-3 flex flex-wrap gap-1.5">
          {guide.bundle.contents.map((item) => (
            <Badge key={item.path} variant="outline">
              <span className="mono">{item.path}</span>
            </Badge>
          ))}
        </div>
      </section>

      <div className="flex flex-col gap-6">
        {guide.sections.map((section) => (
          <section key={section.heading}>
            <h2 className="mb-2 text-[15px] font-semibold">{section.heading}</h2>
            <div className="rounded-lg border border-border bg-background px-5 py-4">
              <Markdown>{section.body}</Markdown>
            </div>
          </section>
        ))}
      </div>

      <Alert variant="muted" className="mt-6">
        <BookOpen aria-hidden="true" />
        <div>
          <AlertTitle>交付方式</AlertTitle>
          <AlertDescription>
            整理完成后通过现有人工渠道交付，由部署人员放入 preset 目录并更新 config.yaml。
            本页不提供上传、创建 preset 或启停 Mock 的操作。
          </AlertDescription>
        </div>
      </Alert>
    </div>
  )
}
