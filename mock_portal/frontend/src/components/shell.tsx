import { BookOpen, Layers2, Server } from "lucide-react"
import { NavLink, useLocation } from "react-router-dom"

import { useMocks } from "@/data/mocks"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarInset,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarProvider,
} from "@/components/ui/sidebar"

function crumbs(pathname: string) {
  if (pathname.startsWith("/standardization")) {
    return ["只读工作台", "标准化指南"]
  }
  if (pathname.startsWith("/mocks/")) {
    const id = decodeURIComponent(pathname.replace("/mocks/", ""))
    return ["只读工作台", "Mock 列表", id]
  }
  return ["只读工作台", "Mock 列表"]
}

export function Shell({ children }: { children: React.ReactNode }) {
  const { data } = useMocks()
  const location = useLocation()
  const path = crumbs(location.pathname)

  return (
    <SidebarProvider>
      <Sidebar aria-label="主导航">
        <SidebarHeader>
          <span
            aria-hidden="true"
            className="grid size-7 place-items-center rounded-lg border border-border bg-background text-primary"
          >
            <Layers2 className="size-4" />
          </span>
          <span className="text-[14px] font-semibold tracking-tight">Mock Portal</span>
        </SidebarHeader>
        <SidebarGroupLabel>只读工作台</SidebarGroupLabel>
        <SidebarContent>
          <SidebarMenu>
            <SidebarMenuItem>
                  <SidebarMenuButton asChild active={location.pathname === "/" || location.pathname.startsWith("/mocks/")}>
                    <NavLink to="/" end>
                    <Server aria-hidden="true" className="size-4" />
                    <span>Mock 列表</span>
                    <span className="ml-auto hidden text-[11px] tabular-nums text-muted-foreground md:inline">
                      {data ? data.total : "—"}
                    </span>
                    </NavLink>
                  </SidebarMenuButton>
            </SidebarMenuItem>
            <SidebarMenuItem>
                  <SidebarMenuButton asChild active={location.pathname.startsWith("/standardization")}>
                    <NavLink to="/standardization">
                    <BookOpen aria-hidden="true" className="size-4" />
                    <span>标准化指南</span>
                    </NavLink>
                  </SidebarMenuButton>
            </SidebarMenuItem>
          </SidebarMenu>
        </SidebarContent>
        <SidebarFooter>
          <b className="mb-1 block font-medium text-foreground">只读门户</b>
          Mock 数量按 preset 统计，不启动、停止或连接 Mock。
        </SidebarFooter>
      </Sidebar>
      <SidebarInset>
        <header className="flex h-12 items-center gap-1.5 border-b border-border px-4 text-[12px] text-muted-foreground md:h-14 md:px-7">
          {path.map((item, index) => (
            <span key={`${item}-${index}`} className="flex items-center gap-1.5">
              {index > 0 && <span aria-hidden="true">›</span>}
              {index === path.length - 1 ? (
                <b className="font-medium text-foreground">{item}</b>
              ) : (
                <span>{item}</span>
              )}
            </span>
          ))}
          <span className="ml-auto rounded border border-border px-1.5 py-0.5 text-[11px]">只读</span>
        </header>
        <main className="min-w-0 flex-1 px-4 py-6 md:px-7 md:py-7">{children}</main>
        <footer className="flex flex-wrap justify-between gap-2 px-4 pb-5 text-[11px] text-muted-foreground md:px-7">
          <span>状态与地址来自各 preset 的 config.yaml 与诊断缓存</span>
          <span>只读 · 不操作 Mock</span>
        </footer>
      </SidebarInset>
    </SidebarProvider>
  )
}
