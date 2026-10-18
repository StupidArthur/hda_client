import { Check, Copy } from "lucide-react"
import * as React from "react"

import { Button } from "@/components/ui/button"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { useToast } from "@/components/toast"
import { cn } from "@/lib/utils"

type CopyButtonProps = {
  value: string
  disabled?: boolean
  disabledReason?: string
  label?: string
  size?: "sm" | "icon"
  className?: string
}

export function CopyButton({
  value,
  disabled,
  disabledReason,
  label = "复制",
  size = "icon",
  className,
}: CopyButtonProps) {
  const { push } = useToast()
  const [copied, setCopied] = React.useState(false)

  const copy = async () => {
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(value)
      } else {
        const area = document.createElement("textarea")
        area.value = value
        area.setAttribute("readonly", "true")
        area.style.position = "fixed"
        area.style.opacity = "0"
        document.body.appendChild(area)
        area.select()
        document.execCommand("copy")
        document.body.removeChild(area)
      }
      setCopied(true)
      push("已复制")
      window.setTimeout(() => setCopied(false), 1600)
    } catch {
      push("复制失败，请手动选择文本")
    }
  }

  const button = (
    <Button
      type="button"
      variant="ghost"
      size={size}
      aria-label={label}
      disabled={disabled}
      onClick={copy}
      className={cn(disabled ? "text-muted-foreground" : "text-muted-foreground hover:text-foreground", className)}
    >
      {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
      {size === "sm" && <span>{label}</span>}
    </Button>
  )

  if (!disabled || !disabledReason) return button
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <span tabIndex={0} className="inline-flex">
          {button}
        </span>
      </TooltipTrigger>
      <TooltipContent>{disabledReason}</TooltipContent>
    </Tooltip>
  )
}
