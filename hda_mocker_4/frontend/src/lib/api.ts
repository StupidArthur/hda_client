import {
  GetLogs,
  GetRuntimeStatus,
  GetTagSnapshots,
  GetVersion,
  InspectPreset,
  PickPresetDirectory,
  StartPreset,
  StopService,
} from '../../wailsjs/go/main/App'

export type PlaybackFile = {
  name: string; periodMs: number; currentRow: number; totalRows: number
  lastCommit: string; lastCommitMs: number; status: string; lastError: string
}
export type RuntimeStatus = {
  phase: string; message: string; presetRoot: string; endpoint: string; startedAt: string
  hdaFilesDone: number; hdaFilesTotal: number; hdaSkipped: number; hdaSamples: number; hdaElapsedMs: number; importFile: string
  tagCount: number; goodCount: number; uncertainCount: number; badCount: number; waitingCount: number; daTagCount: number; playback: PlaybackFile[]
}
export type DatasetFile = {
  name: string; kind: string; rows: number; tags: string[]; tagCount: number
  statusColumns: number; hdaValueColumns: number; hdaStatusColumns: number; periodMs: number
}
export type Preset = {
  root: string; configPath: string; configYaml: string; endpoint: string; namespace: string; files: DatasetFile[]; error: string
}
export type TagRow = {
  name: string; daValue: number | null; daQuality: number; daTime: string; daState: string
  hdaValue: number | null; hdaQuality: number; hdaTime: string; hdaState: string
}

export const api = {
  getLogs: () => GetLogs() as Promise<string[]>,
  getStatus: () => GetRuntimeStatus() as Promise<RuntimeStatus>,
  getTags: (query: string, filter: string) => GetTagSnapshots(query, filter) as Promise<TagRow[]>,
  getVersion: () => GetVersion() as Promise<string>,
  inspect: (root: string) => InspectPreset(root) as Promise<Preset>,
  pickDirectory: () => PickPresetDirectory() as Promise<string>,
  start: (root: string) => StartPreset(root) as Promise<string>,
  stop: () => StopService(),
}
