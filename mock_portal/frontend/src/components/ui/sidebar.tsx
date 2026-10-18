import * as React from "react"
import { Slot } from "@radix-ui/react-slot"

import { cn } from "@/lib/utils"

type SidebarContextValue = {
  open: boolean
  setOpen: (open: boolean) => void
}

const SidebarContext = React.createContext<SidebarContextValue>({
  open: true,
  setOpen: () => {},
})

export function SidebarProvider({
  className,
  children,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) {
  const [open, setOpen] = React.useState(true)
  return (
    <SidebarContext.Provider value={{ open, setOpen }}>
      <div className={cn("flex min-h-svh w-full flex-col md:flex-row", className)} {...props}>
        {children}
      </div>
    </SidebarContext.Provider>
  )
}

export function useSidebar() {
  return React.useContext(SidebarContext)
}

export function Sidebar({ className, ...props }: React.HTMLAttributes<HTMLElement>) {
  return (
    <aside
      data-state={useSidebar().open ? "expanded" : "collapsed"}
      className={cn(
        "flex w-full shrink-0 flex-col gap-4 border-b border-sidebar-border bg-sidebar p-4 text-sidebar-foreground",
        "md:w-[212px] md:gap-6 md:border-b-0 md:border-r md:p-5",
        className,
      )}
      {...props}
    />
  )
}

export function SidebarHeader({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center gap-2.5 px-1", className)} {...props} />
}

export function SidebarContent({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex flex-col gap-1", className)} {...props} />
}

export function SidebarFooter({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "mt-auto hidden border-t border-sidebar-border px-1 pt-3 text-[11px] text-muted-foreground md:block",
        className,
      )}
      {...props}
    />
  )
}

export function SidebarMenu({ className, ...props }: React.HTMLAttributes<HTMLUListElement>) {
  return <ul className={cn("flex list-none flex-row gap-1 p-0 md:flex-col", className)} {...props} />
}

export function SidebarMenuItem({ className, ...props }: React.HTMLAttributes<HTMLLIElement>) {
  return <li className={cn("min-w-0 flex-1 md:flex-none", className)} {...props} />
}

export function SidebarMenuButton({
  active,
  asChild = false,
  className,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { active?: boolean; asChild?: boolean }) {
  const Component = asChild ? Slot : "button"
  return (
    <Component
      type={asChild ? undefined : "button"}
      aria-current={active ? "page" : undefined}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[12.5px] font-medium transition-colors",
        "justify-center md:justify-start",
        active ? "bg-sidebar-accent text-accent-foreground" : "text-sidebar-foreground hover:bg-sidebar-accent/70",
        className,
      )}
      {...props}
    />
  )
}

export function SidebarGroupLabel({ className, ...props }: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <p
      className={cn("hidden px-2.5 text-[11px] text-muted-foreground md:block", className)}
      {...props}
    />
  )
}

export function SidebarInset({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex min-w-0 flex-1 flex-col", className)} {...props} />
}
