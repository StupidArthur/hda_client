import * as React from "react"

import { api, type MockList } from "@/lib/api"

type MocksState = {
  data: MockList | null
  loading: boolean
  refreshing: boolean
  error: string | null
  refresh: (force?: boolean) => void
}

const MocksContext = React.createContext<MocksState>({
  data: null,
  loading: true,
  refreshing: false,
  error: null,
  refresh: () => {},
})

export function useMocks() {
  return React.useContext(MocksContext)
}

const REFRESH_INTERVAL_MS = 15000

export function MocksProvider({ children }: { children: React.ReactNode }) {
  const [data, setData] = React.useState<MockList | null>(null)
  const [loading, setLoading] = React.useState(true)
  const [refreshing, setRefreshing] = React.useState(false)
  const [error, setError] = React.useState<string | null>(null)
  const controllerRef = React.useRef<AbortController | null>(null)

  const load = React.useCallback(async (force: boolean) => {
    controllerRef.current?.abort()
    const controller = new AbortController()
    controllerRef.current = controller
    if (force) setRefreshing(true)
    try {
      const payload = await api.list(controller.signal, force)
      setData(payload)
      setError(null)
    } catch (err) {
      if ((err as Error)?.name === "AbortError") return
      setError(err instanceof Error ? err.message : "列表加载失败")
    } finally {
      if (!controller.signal.aborted) {
        setLoading(false)
        setRefreshing(false)
      }
    }
  }, [])

  React.useEffect(() => {
    void load(false)
    const timer = window.setInterval(() => void load(false), REFRESH_INTERVAL_MS)
    return () => {
      window.clearInterval(timer)
      controllerRef.current?.abort()
    }
  }, [load])

  const value = React.useMemo<MocksState>(
    () => ({
      data,
      loading,
      refreshing,
      error,
      refresh: (force = true) => void load(force),
    }),
    [data, loading, refreshing, error, load],
  )

  return <MocksContext.Provider value={value}>{children}</MocksContext.Provider>
}
