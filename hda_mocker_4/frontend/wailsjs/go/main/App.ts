// Code shape compatible with Wails v2 generated bindings. Keeping this small
// facade in source also lets `npm run build` validate the React application
// before the native Windows linker is available.
declare global {
  interface Window { go?: Record<string, Record<string, Record<string, (...args: unknown[]) => Promise<unknown>>>> }
}

function invoke(name: string, ...args: unknown[]): Promise<unknown> {
  const method = window.go?.main?.App?.[name]
  if (!method) return Promise.reject(new Error('桌面绑定尚未就绪'))
  return method(...args)
}

export const GetLogs = () => invoke('GetLogs')
export const GetRuntimeStatus = () => invoke('GetRuntimeStatus')
export const GetTagSnapshots = (query: string, filter: string) => invoke('GetTagSnapshots', query, filter)
export const GetVersion = () => invoke('GetVersion')
export const InspectPreset = (root: string) => invoke('InspectPreset', root)
export const PickPresetDirectory = () => invoke('PickPresetDirectory')
export const StartPreset = (root: string) => invoke('StartPreset', root)
export const StopService = () => invoke('StopService')
