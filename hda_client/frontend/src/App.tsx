import React, {ChangeEvent, useEffect, useMemo, useRef, useState} from 'react'
import ReactMarkdown from 'react-markdown'
import './style.css'
import userGuide from './user-guide.md?raw'
import {CancelQuery, ChooseParquetFile, ExpandTagExpression, ParseTagCSV, PreviewTagExpression, ReadParquetAnomalyPage, ReadParquetNodesPage, ReadParquetSelectedSummary, ReadParquetSummary, ReadParquetTrend, StartParquetQuery} from '../wailsjs/go/bindings/QueryBinding'
import {SaveSettings, LoadSettings} from '../wailsjs/go/bindings/SettingsBinding'
import {Connect, Disconnect} from '../wailsjs/go/bindings/ConnectionBinding'
import {EventsOff, EventsOn} from '../wailsjs/runtime/runtime'
import {hda} from '../wailsjs/go/models'

type Mode = 'direct' | 'expression' | 'csv'
type Tab = 'query' | 'anomaly' | 'analysis' | 'guide'
type AnalysisView = 'chart' | 'table'
interface Progress { done: number; total: number; records: number }
interface ParquetResult { path: string; nodes: number; records: number; good: number; bad: number; uncertain: number; no_value: number; elapsed_ms: number; rate: number }
interface ParquetRow { timestamp: string; node: string; value: number; quality: string; has_value: boolean }
interface ParquetPage { rows: ParquetRow[]; total: number; nodes: string[] }
interface ExpressionPreview { count: number; first: string; second: string; last: string }
interface ParquetSummary { records: number; good: number; bad: number; uncertain: number; no_value: number; start: string; end: string; nodes: string[] }
interface TrendPoint { timestamp: string; value: number; min: number; max: number }
interface TrendSeries { node: string; points: TrendPoint[] }
interface AnomalyRow { kind: string; node: string; start: string; end: string; count: number }
interface AnomalyPage { rows: AnomalyRow[]; total: number; bad: number; uncertain: number; good_empty: number; anomaly_records: number }

const PAGE_SIZE = 500
const ANOMALY_PAGE_SIZE = 200
// 输出框默认值: .\data\history_YYYYMMDD_HHMMSS.parquet(相对路径由后端按 exe 目录解析)
const defaultOutputPattern = /^\.\\data\\history_\d{8}_\d{6}(?:_\d{3})?\.parquet$/
// 旧版本保存对话框默认落在桌面/文档的 history.parquet(无时间戳), 视为过期值
const staleOutputPattern = /(^|[\\/])history\.parquet$/
type DurationUnit = '秒' | '分' | '时' | '天'
const unitSeconds: Record<DurationUnit, number> = { '秒': 1, '分': 60, '时': 3600, '天': 86400 }
// 恢复配置时把秒数拆成最直观的 数值+单位 组合
function splitDuration(sec: number): [number, DurationUnit] {
  if (sec % 86400 === 0) return [sec / 86400, '天']
  if (sec % 3600 === 0) return [sec / 3600, '时']
  if (sec % 60 === 0) return [sec / 60, '分']
  return [sec, '秒']
}

// 统一当前时间来源: 截止时间与输出文件名时间戳共用同一格式化逻辑, 均保留秒位
function nowStamp() {
  const d = new Date()
  const p = (n: number) => String(n).padStart(2, '0')
  return {
    file: `${d.getFullYear()}${p(d.getMonth()+1)}${p(d.getDate())}_${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}_${String(d.getMilliseconds()).padStart(3, '0')}`,
    end: `${d.getFullYear()}-${p(d.getMonth()+1)}-${p(d.getDate())}T${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`,
  }
}
function defaultOutput() { return `.\\data\\history_${nowStamp().file}.parquet` }
function previewText(p: ExpressionPreview | null, error: string): string {
  if (error) return error
  if (!p) return '解析中…'
  if (p.count <= 1) return `将生成 ${p.count} 个：${p.first}`
  if (p.count === 2) return `将生成 2 个：${p.first}, ${p.last}`
  return `将生成 ${p.count.toLocaleString()} 个：${p.first}, ${p.second}, …, ${p.last}`
}
function directNodes(source: string) { return [...new Set(source.split(/[，,;；\s]+/).map(x => x.trim()).filter(Boolean))] }
function formatTime(timestamp: string) {
  if (!timestamp) return ''
  const date = new Date(timestamp)
  if (Number.isNaN(date.getTime())) return timestamp
  const p = (n: number, width = 2) => String(n).padStart(width, '0')
  return `${date.getFullYear()}-${p(date.getMonth()+1)}-${p(date.getDate())} ${p(date.getHours())}:${p(date.getMinutes())}:${p(date.getSeconds())}.${p(date.getMilliseconds(), 3)}`
}
function anomalyDuration(start: string, end: string) {
  const seconds = Math.max(0, Math.round((new Date(end).getTime()-new Date(start).getTime())/1000))
  if (seconds === 0) return '单点'
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.floor(seconds/60)} 分 ${seconds%60} 秒`
  return `${Math.floor(seconds/3600)} 时 ${Math.floor(seconds%3600/60)} 分`
}

const chartColors = ['#2563eb', '#dc2626', '#059669', '#d97706', '#7c3aed', '#0891b2', '#db2777', '#4b5563']
function TrendChart({series}: {series: TrendSeries[]}) {
  const [range, setRange] = useState<[number, number] | null>(null)
  const [drag, setDrag] = useState<{start: number; current: number} | null>(null)
  useEffect(() => setRange(null), [series])
  const allPoints = series.flatMap(s => s.points)
  let fullMin = Infinity, fullMax = -Infinity
  for (const point of allPoints) { const value = new Date(point.timestamp).getTime(); if (value < fullMin) fullMin = value; if (value > fullMax) fullMax = value }
  const minTime = range?.[0] ?? fullMin, maxTime = range?.[1] ?? fullMax
  const visibleSeries = series.map(item => ({...item, points: item.points.filter(point => { const value = new Date(point.timestamp).getTime(); return value >= minTime && value <= maxTime })}))
  const points = visibleSeries.flatMap(s => s.points)
  if (!points.length) return <div className="chart-empty">当前位号没有可绘制的数据</div>
  let minValue = Infinity, maxValue = -Infinity
  for (const point of points) { if (point.min < minValue) minValue = point.min; if (point.max > maxValue) maxValue = point.max }
  const x = (value: number) => 56 + (value-minTime) / Math.max(1, maxTime-minTime) * 1020
  const y = (value: number) => 24 + (maxValue-value) / Math.max(1e-12, maxValue-minValue) * 286
  const svgX = (event: React.PointerEvent<SVGSVGElement>) => (event.clientX-event.currentTarget.getBoundingClientRect().left)/event.currentTarget.getBoundingClientRect().width*1100
  const timeAt = (position: number) => minTime + Math.max(0, Math.min(1, (position-56)/1020))*(maxTime-minTime)
  return <div className="chart-wrap"><div className="chart-tools"><span>拖拽框选时间范围</span>{range && <button onClick={() => setRange(null)}>显示全部</button>}</div><svg viewBox="0 0 1100 350" preserveAspectRatio="none" role="img" aria-label="趋势图" onPointerDown={event => { const position=svgX(event); event.currentTarget.setPointerCapture(event.pointerId); setDrag({start:position,current:position}) }} onPointerMove={event => drag && setDrag({...drag,current:svgX(event)})} onPointerUp={() => { if (drag && Math.abs(drag.current-drag.start)>8) { const a=timeAt(Math.min(drag.start,drag.current)), b=timeAt(Math.max(drag.start,drag.current)); setRange([a,b]) }; setDrag(null) }}>
    {[0,1,2,3,4].map(i => <line key={i} x1="56" x2="1076" y1={24+i*71.5} y2={24+i*71.5} className="grid"/>)}
    {visibleSeries.map((s, index) => {
      const color = chartColors[index % chartColors.length]
      const path = s.points.map((p, i) => `${i ? 'L':'M'}${x(new Date(p.timestamp).getTime()).toFixed(1)},${y(p.value).toFixed(1)}`).join(' ')
      return <g key={s.node}>{s.points.map((p, i) => <line key={i} x1={x(new Date(p.timestamp).getTime())} x2={x(new Date(p.timestamp).getTime())} y1={y(p.min)} y2={y(p.max)} stroke={color} opacity=".18"/>)}<path d={path} fill="none" stroke={color} strokeWidth="1.8" vectorEffect="non-scaling-stroke"/></g>
    })}
    {drag && <rect x={Math.min(drag.start,drag.current)} y="24" width={Math.abs(drag.current-drag.start)} height="286" className="selection"/>}
    <text x="52" y="20" textAnchor="end">{maxValue.toPrecision(5)}</text><text x="52" y="314" textAnchor="end">{minValue.toPrecision(5)}</text>
    <text x="56" y="338">{formatTime(new Date(minTime).toISOString())}</text><text x="1076" y="338" textAnchor="end">{formatTime(new Date(maxTime).toISOString())}</text>
  </svg><div className="legend">{series.map((s,i) => <span key={s.node}><i style={{background: chartColors[i%chartColors.length]}}/>{s.node}</span>)}</div></div>
}

function Pager({offset, total, onPage, pageSize=PAGE_SIZE}: {offset: number; total: number; onPage: (offset: number) => void; pageSize?: number}) {
  const pages = Math.max(1, Math.ceil(total/pageSize)), current = Math.floor(offset/pageSize)+1
  const [value, setValue] = useState(String(current))
  useEffect(() => setValue(String(current)), [current])
  const go = () => { const page = Math.max(1, Math.min(pages, Number.parseInt(value)||1)); setValue(String(page)); onPage((page-1)*pageSize) }
  return <div className="pager"><span>{total ? offset+1 : 0}–{Math.min(offset+pageSize,total)} / {total.toLocaleString()}</span><button disabled={current<=1} onClick={() => onPage(offset-pageSize)}>上一页</button><label>第 <input type="number" min="1" max={pages} value={value} onChange={e => setValue(e.target.value)} onBlur={go} onKeyDown={e => {if(e.key==='Enter') go()}}/> / {pages.toLocaleString()} 页</label><button disabled={current>=pages} onClick={() => onPage(offset+pageSize)}>下一页</button></div>
}

export default function App() {
  const [tab, setTab] = useState<Tab>('query')
  const [url, setURL] = useState('opc.tcp://127.0.0.1:48630/hda-mocker/')
  const [ns, setNS] = useState(2)
  const [mode, setMode] = useState<Mode>('direct')
  const [direct, setDirect] = useState('dynamic_0001')
  const [expression, setExpression] = useState('M{index:4d}.VALUE[0,9999]')
  const [expressionPreview, setExpressionPreview] = useState<ExpressionPreview | null>(null)
  const [expressionError, setExpressionError] = useState('')
  const [csvName, setCSVName] = useState('')
  const [csvNodes, setCSVNodes] = useState<string[]>([])
  const [endTime, setEndTime] = useState(nowStamp().end)
  const [durationValue, setDurationValue] = useState(1)
  const [durationUnit, setDurationUnit] = useState<DurationUnit>('时')
  const [pageSize, setPageSize] = useState(5000)
  const [output, setOutput] = useState(defaultOutput())
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<Progress | null>(null)
  const [started, setStarted] = useState(0)
  const [, setTick] = useState(0)
  const [status, setStatus] = useState('')
  const [lastResult, setLastResult] = useState<ParquetResult | null>(null)
  const [anomalyFile, setAnomalyFile] = useState('')
  const [anomalyKind, setAnomalyKind] = useState('')
  const [anomalySearch, setAnomalySearch] = useState('')
  const [anomalyPage, setAnomalyPage] = useState<AnomalyPage | null>(null)
  const [anomalyOffset, setAnomalyOffset] = useState(0)
  const [anomalyLoading, setAnomalyLoading] = useState(false)
  const [analysisFile, setAnalysisFile] = useState('')
  const [loadedAnalysisFile, setLoadedAnalysisFile] = useState('')
  const [analysisView, setAnalysisView] = useState<AnalysisView>('chart')
  const [analysisSummary, setAnalysisSummary] = useState<ParquetSummary | null>(null)
  const [selectedSummary, setSelectedSummary] = useState<ParquetSummary | null>(null)
  const [selectedNodes, setSelectedNodes] = useState<string[]>([])
  const [nodeSearch, setNodeSearch] = useState('')
  const [nodeScroll, setNodeScroll] = useState(0)
  const [trend, setTrend] = useState<TrendSeries[]>([])
  const [analysisLoading, setAnalysisLoading] = useState(false)
  const [page, setPage] = useState<ParquetPage | null>(null)
  const [offset, setOffset] = useState(0)
  const [connected, setConnected] = useState(false)
  const [connecting, setConnecting] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const nodes = useMemo(() => mode === 'direct' ? directNodes(direct) : csvNodes, [mode, direct, csvNodes])
  const durationSec = Math.max(0, Math.round(durationValue * unitSeconds[durationUnit]))
  const elapsed = started ? Math.max(0, (Date.now() - started) / 1000) : 0
  const rate = elapsed > 0 && progress ? progress.records / elapsed : 0
  const filteredAnalysisNodes = useMemo(() => {
    const query = nodeSearch.trim().toLocaleLowerCase()
    return analysisSummary?.nodes.filter(node => !query || node.toLocaleLowerCase().includes(query)) ?? []
  }, [analysisSummary, nodeSearch])
  const visibleNodeStart = Math.max(0, Math.floor(nodeScroll / 32) - 3)
  const visibleAnalysisNodes = filteredAnalysisNodes.slice(visibleNodeStart, visibleNodeStart + 22)

  useEffect(() => {
    // 恢复上次会话的界面输入现场; 未保存过(URL 为空)时保持默认值
    LoadSettings().then((s) => {
      if (!s?.url) return
      setURL(s.url)
      if (s.ns !== undefined && s.ns !== null) setNS(s.ns)
      if (s.mode === 'direct' || s.mode === 'expression' || s.mode === 'csv') setMode(s.mode)
      if (s.direct) setDirect(s.direct)
      if (s.expression) setExpression(s.expression)
      if (s.csv_name) setCSVName(s.csv_name)
      if (s.csv_nodes?.length) setCSVNodes(s.csv_nodes)
      // 截止时间不恢复: 每次打开都取软件启动时的当前时间
      if (s.duration_sec > 0) { const [v, u] = splitDuration(s.duration_sec); setDurationValue(v); setDurationUnit(u) }
      if (s.page_size > 0) setPageSize(s.page_size)
      // 保存的是默认路径模式或旧版弹窗默认值时, 恢复为新的时间戳默认, 避免覆盖旧文件
      if (s.output) setOutput(defaultOutputPattern.test(s.output) || staleOutputPattern.test(s.output) ? defaultOutput() : s.output)
    }).catch(error => setStatus(`加载配置失败：${error}`))
    const onProgress = (p: Progress) => setProgress(p)
    const onDone = (result: ParquetResult | {canceled: boolean}) => {
      setBusy(false); setStarted(0)
      // 输出框仍是默认路径时滚动到新时间戳, 下一次查询写新文件
      setOutput(prev => (defaultOutputPattern.test(prev) ? defaultOutput() : prev))
      if ('canceled' in result) { setStatus('查询已取消'); return }
      setLastResult(result); setStatus(`完成：${result.records.toLocaleString()} 条，${result.rate.toFixed(0)} 点/s · ${result.path}`)
      loadAnomalies(result.path, 0, '', '')
    }
    const onError = (message: string) => {
      setBusy(false); setStarted(0); setStatus(message)
      setOutput(prev => (defaultOutputPattern.test(prev) ? defaultOutput() : prev))
    }
    EventsOn('hda:progress', onProgress); EventsOn('hda:parquet:done', onDone); EventsOn('hda:error', onError)
    return () => { EventsOff('hda:progress'); EventsOff('hda:parquet:done'); EventsOff('hda:error') }
  // Wails event subscriptions only need to be registered once.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    let outdated = false
    const timer = window.setTimeout(() => {
      PreviewTagExpression(expression).then(preview => {
        if (!outdated) { setExpressionPreview(preview); setExpressionError('') }
      }).catch(error => {
        if (!outdated) { setExpressionPreview(null); setExpressionError(String(error)) }
      })
    }, 150)
    return () => { outdated = true; window.clearTimeout(timer) }
  }, [expression])

  useEffect(() => {
    if (!busy) return
    const timer = window.setInterval(() => setTick(v => v + 1), 250)
    return () => window.clearInterval(timer)
  }, [busy])

  async function connect() {
    if (connecting || connected) return
    setConnecting(true)
    try {
      await Connect(url)
      setConnected(true)
      setStatus('已连接服务器')
    } catch (e: any) { setStatus(`连接失败：${e}`) } finally { setConnecting(false) }
  }
  async function disconnect() {
    try { await Disconnect() } catch { /* 断开失败也按本地已断开处理 */ }
    setConnected(false)
  }

  async function query() {
    if (mode === 'expression' && expressionError) { setStatus(expressionError); return }
    let queryNodes = nodes
    if (mode === 'expression') {
      try { queryNodes = await ExpandTagExpression(expression) } catch (error: any) { setStatus(String(error)); return }
    }
    if (!queryNodes.length) { setStatus('请先输入位号'); return }
    const path = output.trim() // 留空由后端落到默认 data/history_时间戳.parquet
    const cfg = new hda.QueryConfig({url, ns, tags: queryNodes, end_time: endTime, duration_sec: durationSec, page_size: Math.max(1, Math.round(pageSize)), concurrency: 16})
    setBusy(true); setProgress({done: 0, total: queryNodes.length, records: 0}); setStarted(Date.now()); setStatus('查询中…')
    try {
      await StartParquetQuery(cfg, path)
      // 持久化界面输入现场(表达式原文等), 而非展开后的位号列表
      SaveSettings(new hda.AppSettings({url, ns, mode, direct, expression, csv_name: csvName, csv_nodes: csvNodes, end_time: endTime, duration_sec: durationSec, page_size: Math.max(1, Math.round(pageSize)), output: path})).catch(() => {})
    } catch (e: any) { setBusy(false); setStarted(0); setStatus(`无法开始：${e}`) }
  }
  async function importCSV(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]; if (!file) return
    try {
      const parsed = await ParseTagCSV(await file.text())
      setCSVName(file.name); setCSVNodes(parsed); setMode('csv')
    } catch (error: any) { setStatus(`CSV 导入失败：${error}`) }
    e.target.value = ''
  }
  function downloadTemplate() {
    const a = document.createElement('a'); a.href = URL.createObjectURL(new Blob(['node\nM0001.VALUE\nM0002.VALUE\n'], {type:'text/csv'})); a.download = 'hda-nodes-template.csv'; a.click(); URL.revokeObjectURL(a.href)
  }
  async function loadPage(file = loadedAnalysisFile, nextOffset = offset, selected = selectedNodes) {
    if (!file) return
    try { const result = await ReadParquetNodesPage(file, selected, nextOffset, PAGE_SIZE) as unknown as ParquetPage; setAnalysisFile(file); setOffset(nextOffset); setPage(result); setStatus('') } catch (e: any) { setStatus(`读取失败：${e}`) }
  }
  async function loadAnalysis(file: string) {
    if (!file) return
    setAnalysisLoading(true); setStatus('正在读取 Parquet…')
    try {
      const summary = await ReadParquetSummary(file) as unknown as ParquetSummary
      setAnalysisFile(file); setLoadedAnalysisFile(file); setAnalysisSummary(summary); setNodeSearch(''); setNodeScroll(0)
      setSelectedNodes(summary.nodes.length ? [summary.nodes[0]] : [])
      if (!summary.nodes.length) { setPage(null); setTrend([]) }
      setStatus('')
    } catch (e: any) { setStatus(`读取失败：${e}`) } finally { setAnalysisLoading(false) }
  }
  async function chooseAnalysis() { try { const file = await ChooseParquetFile(); if (file) await loadAnalysis(file) } catch (e: any) { setStatus(String(e)) } }
  async function loadAnomalies(file = anomalyFile, nextOffset = anomalyOffset, kind = anomalyKind, search = anomalySearch) {
    if (!file) return
    setAnomalyLoading(true)
    try {
      const result = await ReadParquetAnomalyPage(file, kind, search, nextOffset, ANOMALY_PAGE_SIZE) as unknown as AnomalyPage
      setAnomalyFile(file); setAnomalyPage(result); setAnomalyOffset(nextOffset); setStatus('')
    } catch (error: any) { setStatus(`异常值读取失败：${error}`) } finally { setAnomalyLoading(false) }
  }
  async function chooseAnomalyFile() { try { const file = await ChooseParquetFile(); if (file) { setAnomalyKind(''); setAnomalySearch(''); await loadAnomalies(file,0,'','') } } catch (e: any) { setStatus(String(e)) } }
  function toggleAnalysisNode(node: string) {
    setSelectedNodes(current => current.includes(node) ? current.filter(item => item !== node) : [...current, node])
  }

  useEffect(() => {
    if (!loadedAnalysisFile || !analysisSummary || !selectedNodes.length) { setTrend([]); setPage(null); setSelectedSummary(null); return }
    let outdated = false
    setAnalysisLoading(true)
    Promise.all([
      ReadParquetTrend(loadedAnalysisFile, selectedNodes.slice(0, 12), 700),
      ReadParquetNodesPage(loadedAnalysisFile, selectedNodes, 0, PAGE_SIZE),
      ReadParquetSelectedSummary(loadedAnalysisFile, selectedNodes),
    ]).then(([nextTrend, nextPage, nextSummary]) => {
      if (!outdated) { setTrend(nextTrend as unknown as TrendSeries[]); setPage(nextPage as unknown as ParquetPage); setSelectedSummary(nextSummary as unknown as ParquetSummary); setOffset(0); setStatus(selectedNodes.length > 12 ? `已选 ${selectedNodes.length} 个位号，曲线显示前 12 个，表格和统计包含全部` : '') }
    }).catch(error => { if (!outdated) setStatus(`分析失败：${error}`) })
      .finally(() => { if (!outdated) setAnalysisLoading(false) })
    return () => { outdated = true }
  }, [loadedAnalysisFile, analysisSummary, selectedNodes])

  return <main className="app">
    <header className="connection">
      <label className="head-label">服务器配置</label>
      <input value={url} onChange={e => setURL(e.target.value)} aria-label="OPC UA URL" placeholder="opc.tcp://host:port/"/>
      <label>NS <input type="number" min="0" value={ns} onChange={e => setNS(Number(e.target.value))}/></label>
      {connected
        ? <button className="conn on" onClick={disconnect}>已连接 · 断开</button>
        : <button className="conn" disabled={connecting} onClick={connect}>{connecting ? '连接中…' : '连接'}</button>}
      <span className="watermark">v1.1 designed by @yuzechao Industrial AI</span>
    </header>
    <nav className="tabs"><button className={tab === 'query' ? 'active' : ''} onClick={() => setTab('query')}>查询</button><button className={tab === 'anomaly' ? 'active' : ''} onClick={() => { setTab('anomaly'); if (!anomalyFile && lastResult) loadAnomalies(lastResult.path,0,'','') }}>异常值{anomalyPage && anomalyPage.anomaly_records > 0 && <em>{anomalyPage.anomaly_records.toLocaleString()}</em>}</button><button className={tab === 'analysis' ? 'active' : ''} onClick={() => setTab('analysis')}>数据详情</button><button className={tab === 'guide' ? 'active' : ''} onClick={() => setTab('guide')}>使用说明</button></nav>
    {tab === 'query' ? <section className="content query">
      <h2 className="section-title">查询配置</h2>
      <div className="time"><label>截止 <input type="text" value={endTime} onChange={e => setEndTime(e.target.value)} placeholder="2026-09-07T16:30:00" title="格式 yyyy-MM-ddTHH:mm:ss，与配置文件一致"/></label><label>时长 <input type="number" min="1" value={durationValue} onChange={e => setDurationValue(Number(e.target.value))}/><select value={durationUnit} onChange={e => setDurationUnit(e.target.value as DurationUnit)}>{(['秒', '分', '时', '天'] as DurationUnit[]).map(u => <option key={u} value={u}>{u}</option>)}</select></label><label>单页上限 <input type="number" min="1" max="1000000" value={pageSize} onChange={e => setPageSize(Number(e.target.value))}/></label></div>
      <div className="output"><label>输出路径</label><input value={output} onChange={e => setOutput(e.target.value)} placeholder="输出 Parquet 文件"/></div>
      <h2 className="section-title">查询位号</h2>
      <div className="mode-tabs"><button className={mode==='direct'?'active':''} onClick={() => setMode('direct')}>直接输入</button><button className={mode==='expression'?'active':''} onClick={() => setMode('expression')}>表达式</button><button className={mode==='csv'?'active':''} onClick={() => setMode('csv')}>CSV 导入</button></div>
      {mode === 'direct' && (
        <textarea value={direct} onChange={e => setDirect(e.target.value)} placeholder="M0001.VALUE, M0002.VALUE；也支持空格和换行" rows={5}/>
      )}
      {mode === 'expression' && <><input value={expression} onChange={e => setExpression(e.target.value)} aria-label="位号表达式"/><small className={expressionError ? 'error' : ''}>{previewText(expressionPreview, expressionError)}</small></>}
      {mode === 'csv' && <div className="import"><span>{csvName ? `${csvName} · ${nodes.length.toLocaleString()} 个位号` : '选择一个 node 列 CSV 文件'}</span><button onClick={() => inputRef.current?.click()}>选择 CSV</button><button className="link" onClick={downloadTemplate}>下载模板</button></div>}
      <input ref={inputRef} type="file" accept=".csv,text/csv" hidden onChange={importCSV}/>
      {busy ? <button className="run cancel" onClick={() => CancelQuery()}>取消查询</button> : <button className="run" onClick={query}>查询</button>}
      {(busy || lastResult) && <div className="progress"><div><i style={{width: `${progress && progress.total ? progress.done / progress.total * 100 : 0}%`}}/></div><span>{progress?.done ?? 0}/{progress?.total ?? 0} 个位号</span><span>{(progress?.records ?? lastResult?.records ?? 0).toLocaleString()} 条</span>{!busy && lastResult && <><span>Good {lastResult.good.toLocaleString()}</span><span>Bad {lastResult.bad.toLocaleString()}</span><span>Uncertain {lastResult.uncertain.toLocaleString()}</span>{lastResult.no_value > 0 && <span>无值 {lastResult.no_value.toLocaleString()}</span>}</>}<span>{(busy ? elapsed : (lastResult?.elapsed_ms ?? 0) / 1000).toFixed(2)}s</span><span>{(busy ? rate : lastResult?.rate ?? 0).toFixed(0)} 点/s</span></div>}
      {lastResult && <div className="result-actions"><button className="link analyze anomaly-link" onClick={() => { setTab('anomaly'); loadAnomalies(lastResult.path,0,'','') }}>查看异常值</button><button className="link analyze" onClick={() => { setTab('analysis'); loadAnalysis(lastResult.path) }}>查看数据详情</button></div>}
    </section> : tab === 'anomaly' ? <section className="content anomaly-page">
      <div className="output"><input value={anomalyFile} onChange={e => setAnomalyFile(e.target.value)} onKeyDown={e => { if(e.key==='Enter') loadAnomalies(anomalyFile,0) }} placeholder="选择 Parquet 文件，或输入路径后回车"/><button onClick={chooseAnomalyFile}>选择</button></div>
      {anomalyPage && <div className="anomaly-content">
        <div className="anomaly-toolbar"><div className="anomaly-filters">{[['','全部'],['Bad',`Bad ${anomalyPage.bad.toLocaleString()}`],['Uncertain',`Uncertain ${anomalyPage.uncertain.toLocaleString()}`],['Good 空值',`Good 空值 ${anomalyPage.good_empty.toLocaleString()}`]].map(([value,label]) => <button key={value} className={anomalyKind===value?'active':''} onClick={() => {setAnomalyKind(value); loadAnomalies(anomalyFile,0,value,anomalySearch)}}>{label}</button>)}</div><div className="anomaly-search"><input value={anomalySearch} onChange={e => setAnomalySearch(e.target.value)} onKeyDown={e => {if(e.key==='Enter') loadAnomalies(anomalyFile,0,anomalyKind,anomalySearch)}} placeholder="搜索位号"/><button onClick={() => loadAnomalies(anomalyFile,0,anomalyKind,anomalySearch)}>搜索</button></div></div>
        {anomalyLoading && <div className="anomaly-loading">读取中…</div>}
        <div className="anomaly-table-scroll"><table><thead><tr><th>类型</th><th>位号</th><th>开始时间</th><th>结束时间</th><th>持续时间</th><th>记录数</th></tr></thead><tbody>{anomalyPage.rows.map((row,index) => <tr key={`${row.node}-${row.start}-${index}`} className={row.kind==='Bad'?'quality-bad':row.kind==='Uncertain'?'quality-uncertain':'quality-empty'}><td>{row.kind}</td><td>{row.node}</td><td>{formatTime(row.start)}</td><td>{formatTime(row.end)}</td><td>{anomalyDuration(row.start,row.end)}</td><td>{row.count.toLocaleString()}</td></tr>)}</tbody></table>{!anomalyPage.rows.length && <div className="no-anomaly">未发现符合条件的异常值</div>}</div>
        <Pager offset={anomalyOffset} total={anomalyPage.total} pageSize={ANOMALY_PAGE_SIZE} onPage={next => loadAnomalies(anomalyFile,next)}/>
      </div>}
    </section> : tab === 'analysis' ? <section className="content analysis">
      <div className="output"><input value={analysisFile} onChange={e => setAnalysisFile(e.target.value)} onKeyDown={e => { if (e.key === 'Enter') loadAnalysis(analysisFile) }} placeholder="选择 Parquet 文件，或输入路径后回车"/><button onClick={chooseAnalysis}>选择</button></div>
      {analysisSummary && <div className="analysis-shell">
        <aside className="node-panel">
          <div className="node-head"><strong>位号</strong><span>{selectedNodes.length}/{analysisSummary.nodes.length.toLocaleString()}</span></div>
          <div className="node-actions"><button onClick={() => setSelectedNodes(analysisSummary.nodes)}>全选</button><button onClick={() => setSelectedNodes([])}>取消</button></div>
          <input value={nodeSearch} onChange={e => { setNodeSearch(e.target.value); setNodeScroll(0) }} placeholder="搜索位号"/>
          <div className="node-list" onScroll={e => setNodeScroll(e.currentTarget.scrollTop)}>
            <div style={{height: filteredAnalysisNodes.length * 32, position: 'relative'}}>
              {visibleAnalysisNodes.map((node, index) => <label key={node} className="node-row" style={{top: (visibleNodeStart + index) * 32}} title={node}><input type="checkbox" checked={selectedNodes.includes(node)} onChange={() => toggleAnalysisNode(node)}/><span>{node}</span></label>)}
            </div>
          </div>
        </aside>
        <div className="analysis-main">
          <div className="summary"><span><b>{selectedSummary?.records.toLocaleString() ?? 0}</b> 条已选数据</span><span>Good <b>{selectedSummary?.good.toLocaleString() ?? 0}</b></span><span>Bad <b>{selectedSummary?.bad.toLocaleString() ?? 0}</b></span><span>Uncertain <b>{selectedSummary?.uncertain.toLocaleString() ?? 0}</b></span><span>{selectedSummary ? `${formatTime(selectedSummary.start)} — ${formatTime(selectedSummary.end)}` : ''}</span></div>
          <div className="view-tabs"><button className={analysisView==='chart'?'active':''} onClick={() => setAnalysisView('chart')}>曲线</button><button className={analysisView==='table'?'active':''} onClick={() => setAnalysisView('table')}>表格</button>{analysisLoading && <span>读取中…</span>}</div>
          {analysisView === 'chart' ? <TrendChart series={trend}/> : page && <div className="table-view"><div className="table-scroll"><table><thead><tr><th>时间</th><th>位号</th><th>值</th><th>质量</th></tr></thead><tbody>{page.rows.map((row, i) => <tr key={`${row.node}-${row.timestamp}-${i}`} className={row.quality.startsWith('Bad') ? 'quality-bad' : row.quality.startsWith('Uncertain') ? 'quality-uncertain' : ''}><td>{formatTime(row.timestamp)}</td><td>{row.node}</td><td>{row.has_value ? row.value : ''}</td><td>{row.quality}</td></tr>)}</tbody></table></div><Pager offset={offset} total={page.total} onPage={next => loadPage(loadedAnalysisFile,next)}/></div>}
        </div>
      </div>}
    </section> : <section className="content guide"><article className="markdown"><ReactMarkdown>{userGuide}</ReactMarkdown></article></section>}
    <footer>{status}</footer>
  </main>
}
