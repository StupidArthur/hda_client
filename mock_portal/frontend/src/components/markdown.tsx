import ReactMarkdown, { type Components } from "react-markdown"
import remarkGfm from "remark-gfm"

import { cn } from "@/lib/utils"

const components: Components = {
  a({ href, children }) {
    const target = typeof href === "string" ? href.trim() : ""
    if (!target) return <span>{children}</span>
    if (/^https?:\/\//i.test(target)) {
      return (
        <a href={target} target="_blank" rel="noreferrer noopener">
          {children}
        </a>
      )
    }
    if (target.startsWith("#") || target.startsWith("/api/")) {
      return <a href={target}>{children}</a>
    }
    // Relative links in summaries are not resolved to local paths; show text
    // rather than exposing an arbitrary filesystem location.
    return <span>{children}</span>
  },
  table({ children }) {
    return (
      <div className="my-3 w-full overflow-x-auto">
        <table>{children}</table>
      </div>
    )
  },
}

export function Markdown({ children, className }: { children: string; className?: string }) {
  return (
    <div
      className={cn(
        "prose prose-sm max-w-none prose-neutral dark:prose-invert",
        "prose-headings:font-semibold prose-headings:tracking-tight",
        "prose-h1:text-[20px] prose-h2:text-[16px] prose-h3:text-[14px]",
        "prose-p:text-[12.5px] prose-li:text-[12.5px] prose-li:my-0.5",
        "prose-a:text-primary prose-a:no-underline hover:prose-a:underline",
        "prose-code:rounded prose-code:bg-secondary prose-code:px-1 prose-code:py-0.5 prose-code:text-[11.5px] prose-code:before:content-none prose-code:after:content-none",
        "prose-pre:rounded-lg prose-pre:bg-secondary prose-pre:text-[11.5px]",
        "prose-table:text-[12px] prose-th:text-muted-foreground prose-th:font-normal",
        "prose-hr:border-border prose-strong:text-foreground",
        className,
      )}
    >
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml components={components}>
        {children}
      </ReactMarkdown>
    </div>
  )
}
