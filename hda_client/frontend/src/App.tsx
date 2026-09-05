import React, {useEffect, useMemo, useRef, useState} from 'react'
import './style.css'
import {Connect, Disconnect, IsConnected, BrowseTags} from '../wailsjs/go/bindings/ConnectionBinding'
import {StartQuery, CancelQuery} from '../wailsjs/go/bindings/QueryBinding'
import {SaveConfig, LoadConfig} from '../wailsjs/go/bindings/SettingsBinding'
import {EventsOn, EventsOff} from '../wailsjs/runtime/runtime'
import {hda} from '../wailsjs/go/models'

interface DataPoint {
    time: string
    value: number
    quality: string
    has_value: boolean
}

interface TagResult {
    tag: string
    points: DataPoint[]
}

interface QueryProgress {
    done: number
    total: number
    records: number
    active: boolean
    canceled: boolean
}

interface Row {
    tag: string
    time: string
    value: number
    quality: string
    has_value: boolean
}

function fmtTime(t: string): string {
    if (!t) return ''
    const d = new Date(t)
    if (isNaN(d.getTime())) return t
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function nowLocal(): string {
    const d = new Date()
    d.setSeconds(0, 0)
    const pad = (n: number) => String(n).padStart(2, '0')
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function App() {
    const [url, setUrl] = useState('opc.tcp://10.30.144.70:18950/')
    const [ns, setNs] = useState(1)
    const [tagsText, setTagsText] = useState('M0001.VALUE')
    const [endTime, setEndTime] = useState(nowLocal())
    const [durationSec, setDurationSec] = useState(3600)
    const [concurrency, setConcurrency] = useState(16)
    const [connected, setConnected] = useState(false)
    const [busy, setBusy] = useState(false)
    const [progress, setProgress] = useState<QueryProgress | null>(null)
    const [status, setStatus] = useState('')
    const [rows, setRows] = useState<Row[]>([])
    const [tagCount, setTagCount] = useState(0)
    const [showBrowse, setShowBrowse] = useState(false)
    const [browseList, setBrowseList] = useState<hda.ServerTag[]>([])
    const [onlyGood, setOnlyGood] = useState(false)
    const resultRef = useRef<Row[]>([])
    const queryRef = useRef(false)

    useEffect(() => {
        LoadConfig().then((res: any) => {
            if (res && res[1]) {
                const cfg = res[0]
                setUrl(cfg.url)
                setNs(cfg.ns)
                if (cfg.tags && cfg.tags.length) setTagsText(cfg.tags.join('\n'))
                if (cfg.end_time) setEndTime(cfg.end_time.replace(' ', 'T'))
                setDurationSec(cfg.duration_sec)
                setConcurrency(cfg.concurrency)
            }
        }).catch(() => {})
        IsConnected().then((v: boolean) => setConnected(v)).catch(() => {})

        const onProg = (p: QueryProgress) => setProgress(p)
        const onDone = (results: TagResult[]) => {
            queryRef.current = false
            setBusy(false)
            const flat: Row[] = []
            for (const r of results) {
                for (const p of r.points) flat.push({tag: r.tag, time: p.time, value: p.value, quality: p.quality, has_value: p.has_value})
            }
            resultRef.current = flat
            setRows(flat)
            setTagCount(results.length)
            setStatus(`查询完成: ${results.length} 个位号, ${flat.length.toLocaleString()} 条记录`)
        }
        const onErr = (e: string) => {
            queryRef.current = false
            setBusy(false)
            setStatus('查询失败: ' + e)
        }
        EventsOn('hda:progress', onProg)
        EventsOn('hda:done', onDone)
        EventsOn('hda:error', onErr)
        return () => {
            EventsOff('hda:progress')
            EventsOff('hda:done')
            EventsOff('hda:error')
        }
    }, [])

    const tags = useMemo(() => tagsText.split('\n').map(t => t.trim()).filter(Boolean), [tagsText])

    async function onConnect() {
        setStatus('连接中...')
        try {
            await Connect(url)
            setConnected(true)
            setStatus('已连接: ' + url)
        } catch (e: any) {
            setStatus('连接失败: ' + e)
        }
    }
    async function onDisconnect() {
        await Disconnect()
        setConnected(false)
        setStatus('已断开')
    }
    async function onBrowse() {
        setStatus('浏览服务器...')
        try {
            const list = await BrowseTags(ns)
            setBrowseList(list)
            setShowBrowse(true)
            setStatus(`浏览到 ${list.length} 个位号`)
        } catch (e: any) {
            setStatus('浏览失败: ' + e)
        }
    }
    function pickTags(picked: hda.ServerTag[]) {
        const existing = new Set(tags.map(t => t.trim()))
        const add = picked.map(t => t.name).filter(n => !existing.has(n))
        if (add.length) setTagsText([...tagsText.split('\n'), ...add].join('\n'))
        setShowBrowse(false)
    }

    async function onQuery() {
        if (!tags.length) { setStatus('请填写位号'); return }
        const cfg: hda.QueryConfig = new hda.QueryConfig({
            url, ns,
            tags,
            end_time: endTime.replace('T', ' '),
            duration_sec: durationSec,
            concurrency,
        })
        queryRef.current = true
        setBusy(true)
        setProgress({done: 0, total: 0, records: 0, active: true, canceled: false})
        setRows([])
        resultRef.current = []
        setStatus('查询中...')
        try {
            await StartQuery(cfg)
            SaveConfig(cfg).catch(() => {})
        } catch (e: any) {
            queryRef.current = false
            setBusy(false)
            setStatus('发起失败: ' + e)
        }
    }
    async function onCancel() {
        await CancelQuery()
        setStatus('正在取消...')
    }

    function exportCSV() {
        if (!resultRef.current.length) return
        const head = 'time,tag,value,quality\n'
        const body = resultRef.current.map(r => `${r.time},${r.tag},${r.has_value ? r.value : ''},${r.quality}`).join('\n')
        const blob = new Blob(['\ufeff' + head + body], {type: 'text/csv;charset=utf-8'})
        const a = document.createElement('a')
        a.href = URL.createObjectURL(blob)
        a.download = `hda_${new Date().toISOString().replace(/[:.]/g, '-')}.csv`
        a.click()
        URL.revokeObjectURL(a.href)
        setStatus('CSV 已导出')
    }

    const displayRows = useMemo(() => {
        if (!onlyGood) return rows
        return rows.filter(r => r.quality === 'Good')
    }, [rows, onlyGood])

    return (
        <div className="app">
            <header className="topbar">
                <span className="logo">HDA 查询</span>
                <input className="input url" value={url} onChange={e => setUrl(e.target.value)} placeholder="opc.tcp://host:port/..."/>
                <label>ns</label>
                <input className="input ns" type="number" value={ns} onChange={e => setNs(Number(e.target.value))} min={0}/>
                {connected
                    ? <button className="btn danger" onClick={onDisconnect}>断开</button>
                    : <button className="btn primary" onClick={onConnect}>连接</button>}
                <span className={`dot ${connected ? 'on' : 'off'}`}/>
            </header>

            <div className="body">
                <aside className="panel">
                    <section>
                        <h3>位号 (tags)</h3>
                        <textarea className="input tags" value={tagsText} onChange={e => setTagsText(e.target.value)}
                                  placeholder={'每行一个位号, 如\nM0001.VALUE\nM0002.VALUE'} rows={8}/>
                        <div className="row">
                            <button className="btn" onClick={onBrowse}>浏览服务器</button>
                            <span className="muted">{tags.length} 个</span>
                        </div>
                    </section>
                    <section>
                        <h3>时间</h3>
                        <div className="row">
                            <input className="input grow" type="datetime-local" value={endTime} onChange={e => setEndTime(e.target.value)}/>
                        </div>
                        <div className="row">
                            <label>时长(秒)</label>
                            <input className="input grow" type="number" value={durationSec} onChange={e => setDurationSec(Number(e.target.value))} min={1}/>
                        </div>
                        <div className="row">
                            <button className="chip" onClick={() => { setEndTime(nowLocal()); setDurationSec(3600) }}>1h</button>
                            <button className="chip" onClick={() => { setEndTime(nowLocal()); setDurationSec(86400) }}>24h</button>
                            <button className="chip" onClick={() => { setEndTime(nowLocal()); setDurationSec(604800) }}>7d</button>
                        </div>
                    </section>
                    <section>
                        <h3>并发</h3>
                        <input className="input grow" type="number" value={concurrency} onChange={e => setConcurrency(Number(e.target.value))} min={1} max={128}/>
                    </section>
                    <section>
                        {!busy
                            ? <button className="btn primary block" onClick={onQuery} disabled={!connected}>开始查询</button>
                            : <div className="row">
                                <button className="btn danger" onClick={onCancel}>取消</button>
                                <span className="muted">运行中...</span>
                              </div>}
                        {progress && progress.total > 0 && (
                            <div className="prog">
                                <div className="bar"><div className="fill" style={{width: `${Math.min(100, progress.done / progress.total * 100)}%`}}/></div>
                                <div className="muted">段 {progress.done}/{progress.total} · 记录 {progress.records.toLocaleString()}</div>
                            </div>
                        )}
                    </section>
                </aside>

                <main className="panel result">
                    <div className="result-head">
                        <label><input type="checkbox" checked={onlyGood} onChange={e => setOnlyGood(e.target.checked)}/> 只看 Good</label>
                        <span className="muted">{tagCount} 位号 · {displayRows.length.toLocaleString()} 行</span>
                        <button className="btn" onClick={exportCSV}>导出 CSV</button>
                    </div>
                    <VTable rows={displayRows}/>
                </main>
            </div>

            {showBrowse && (
                <div className="modal">
                    <div className="modal-box">
                        <h3>浏览到 {browseList.length} 个位号</h3>
                        <div className="browse-list">
                            {browseList.map(t => (
                                <label key={t.node_id} className="brow">
                                    <input type="checkbox" value={t.name}/>
                                    <span>{t.name}</span>
                                    <span className="muted">{t.node_id}</span>
                                </label>
                            ))}
                        </div>
                        <div className="row right">
                            <button className="btn" onClick={() => setShowBrowse(false)}>关闭</button>
                            <button className="btn primary" onClick={() => {
                                const checked = Array.from(document.querySelectorAll<HTMLInputElement>('.browse-list input:checked'))
                                    .map(i => i.value)
                                const picked = browseList.filter(t => checked.includes(t.name))
                                pickTags(picked)
                            }}>添加选中({document.querySelectorAll('.browse-list input:checked').length})</button>
                        </div>
                    </div>
                </div>
            )}

            <footer className="statusbar">{status}</footer>
        </div>
    )
}

const ROW_H = 28

function VTable({rows}: {rows: Row[]}) {
    const ref = useRef<HTMLDivElement>(null)
    const [scrollTop, setScrollTop] = useState(0)
    const [height, setHeight] = useState(400)

    useEffect(() => {
        const el = ref.current
        if (!el) return
        const ro = new ResizeObserver(() => setHeight(el.clientHeight))
        ro.observe(el)
        return () => ro.disconnect()
    }, [])

    const start = Math.max(0, Math.floor(scrollTop / ROW_H) - 5)
    const end = Math.min(rows.length, start + Math.ceil(height / ROW_H) + 10)
    const slice = rows.slice(start, end)

    return (
        <div className="vtable" ref={ref} onScroll={e => setScrollTop((e.target as HTMLDivElement).scrollTop)}>
            <div className="thead">
                <div className="th t-time">时间</div>
                <div className="th t-tag">位号</div>
                <div className="th t-val">值</div>
                <div className="th t-q">质量</div>
            </div>
            <div style={{height: rows.length * ROW_H, position: 'relative'}}>
                {slice.map((r, i) => (
                    <div key={start + i} className={`tr ${r.quality === 'Good' ? '' : 'bad'}`}
                         style={{top: (start + i) * ROW_H, height: ROW_H}}>
                        <div className="td t-time">{fmtTime(r.time)}</div>
                        <div className="td t-tag">{r.tag}</div>
                        <div className="td t-val">{r.has_value ? r.value : ''}</div>
                        <div className="td t-q">{r.quality}</div>
                    </div>
                ))}
            </div>
        </div>
    )
}

export default App
