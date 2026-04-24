'use client'

import { useEffect, useRef, useState, useCallback } from 'react'
import { logsApi } from '@/lib/api'
import { DeploymentLog } from '@/types'
import { useAuthStore } from '@/lib/auth'

interface LogViewerProps {
  deploymentId: string
  /** Whether to use WebSocket for live streaming (default: true for in-progress) */
  live?: boolean
  maxLines?: number
}

type LogLevel = 'all' | 'info' | 'error' | 'warn' | 'debug'

const LEVEL_COLORS: Record<string, string> = {
  error: 'text-red-400',
  warn:  'text-yellow-400',
  info:  'text-slate-300',
  debug: 'text-slate-500',
  system:'text-indigo-400',
}

function LogLine({ log, search }: { log: DeploymentLog; search: string }) {
  const color = LEVEL_COLORS[log.level] || 'text-slate-400'
  const msg = search
    ? log.message.replace(
        new RegExp(`(${search})`, 'gi'),
        '<mark class="bg-yellow-300/30 text-yellow-200 rounded px-0.5">$1</mark>'
      )
    : log.message

  return (
    <div className={`flex gap-2 px-4 py-0.5 hover:bg-white/[0.03] font-mono text-xs leading-5 ${color}`}>
      <span className="shrink-0 text-slate-600 select-none w-6 text-right">{log.stream === 'stderr' ? '!' : '›'}</span>
      <span dangerouslySetInnerHTML={{ __html: msg }} />
    </div>
  )
}

export function LogViewer({ deploymentId, live = false, maxLines = 1000 }: LogViewerProps) {
  const [logs, setLogs]           = useState<DeploymentLog[]>([])
  const [loading, setLoading]     = useState(true)
  const [connected, setConnected] = useState(false)
  const [autoScroll, setAutoScroll] = useState(true)
  const [filter, setFilter]       = useState<LogLevel>('all')
  const [search, setSearch]       = useState('')

  const bottomRef = useRef<HTMLDivElement>(null)
  const wsRef     = useRef<WebSocket | null>(null)
  const { token } = useAuthStore()

  // Fetch initial REST logs
  useEffect(() => {
    setLoading(true)
    logsApi.get(deploymentId)
      .then(r => setLogs(r.data?.data || []))
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [deploymentId])

  // WebSocket live streaming
  const connectWS = useCallback(() => {
    if (!live) return
    if (wsRef.current && wsRef.current.readyState === WebSocket.OPEN) return

    const apiBase = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080'
    const wsBase  = apiBase.replace(/^http/, 'ws')
    // Attach token as query param — browsers cannot set Authorization header on WS
    const wsURL   = `${wsBase}/api/v1/deployments/${deploymentId}/logs/ws?token=${encodeURIComponent(token || '')}`

    const ws = new WebSocket(wsURL)
    wsRef.current = ws

    ws.onopen = () => {
      setConnected(true)
    }

    ws.onmessage = (ev) => {
      try {
        const log: DeploymentLog = JSON.parse(ev.data)
        setLogs(prev => {
          const next = [...prev, log]
          return next.length > maxLines ? next.slice(next.length - maxLines) : next
        })
      } catch { /* ignore malformed frames */ }
    }

    ws.onclose = () => {
      setConnected(false)
      // Reconnect after 3 s if still live
      if (live) {
        setTimeout(() => connectWS(), 3000)
      }
    }

    ws.onerror = () => {
      ws.close()
    }
  }, [deploymentId, live, token, maxLines])

  useEffect(() => {
    connectWS()
    return () => {
      if (wsRef.current) {
        wsRef.current.onclose = null // prevent reconnect on unmount
        wsRef.current.close()
      }
    }
  }, [connectWS])

  // Auto-scroll to bottom
  useEffect(() => {
    if (autoScroll && bottomRef.current) {
      bottomRef.current.scrollIntoView({ behavior: 'smooth' })
    }
  }, [logs, autoScroll])

  const filteredLogs = logs.filter(l => {
    if (filter !== 'all' && l.level !== filter) return false
    if (search && !l.message.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

  return (
    <div className="flex flex-col h-full rounded-xl overflow-hidden border border-[var(--border-color)] bg-[#0d1117]">
      {/* Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-[var(--border-subtle)] bg-[#0d1117]/80 backdrop-blur-sm">
        {live && (
          <span className={`flex items-center gap-1.5 text-[10px] font-medium px-2 py-0.5 rounded-full ${connected ? 'text-green-400 bg-green-400/10 border border-green-400/20' : 'text-slate-500 bg-slate-500/10 border border-slate-500/20'}`}>
            <span className={`inline-block w-1.5 h-1.5 rounded-full ${connected ? 'bg-green-400 animate-pulse' : 'bg-slate-500'}`} />
            {connected ? 'Live' : 'Reconnecting…'}
          </span>
        )}
        <div className="flex gap-1 ml-0">
          {(['all', 'info', 'error', 'warn', 'debug'] as LogLevel[]).map(l => (
            <button
              key={l}
              onClick={() => setFilter(l)}
              className={`px-2 py-0.5 rounded text-[10px] font-medium transition-colors ${
                filter === l
                  ? 'bg-indigo-600/30 text-indigo-300 border border-indigo-500/30'
                  : 'text-slate-500 hover:text-slate-300'
              }`}
            >
              {l}
            </button>
          ))}
        </div>
        <input
          type="text"
          placeholder="Search logs…"
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="ml-auto bg-transparent border border-[var(--border-subtle)] rounded px-2 py-0.5 text-xs text-slate-300 placeholder:text-slate-600 focus:outline-none focus:border-indigo-500/50 w-40"
        />
        <button
          onClick={() => setAutoScroll(p => !p)}
          title={autoScroll ? 'Disable auto-scroll' : 'Enable auto-scroll'}
          className={`text-xs px-2 py-0.5 rounded border transition-colors ${
            autoScroll
              ? 'bg-indigo-600/20 text-indigo-300 border-indigo-500/30'
              : 'text-slate-500 border-[var(--border-subtle)] hover:text-slate-300'
          }`}
        >
          ↓ {autoScroll ? 'Auto' : 'Manual'}
        </button>
      </div>

      {/* Log lines */}
      <div className="flex-1 overflow-y-auto py-2" style={{ fontVariantNumeric: 'tabular-nums' }}>
        {loading ? (
          <div className="text-center py-10 text-slate-600 text-sm">Loading logs…</div>
        ) : filteredLogs.length === 0 ? (
          <div className="text-center py-10 text-slate-600 text-sm">No logs{filter !== 'all' || search ? ' matching filters' : ' yet'}.</div>
        ) : (
          filteredLogs.map((log, i) => (
            <LogLine key={log.id || i} log={log} search={search} />
          ))
        )}
        <div ref={bottomRef} />
      </div>

      {/* Footer */}
      <div className="px-4 py-1.5 border-t border-[var(--border-subtle)] bg-[#0d1117]/80 flex items-center gap-2">
        <span className="text-[10px] text-slate-600">{filteredLogs.length} line{filteredLogs.length !== 1 ? 's' : ''}</span>
        {(filter !== 'all' || search) && (
          <button
            onClick={() => { setFilter('all'); setSearch('') }}
            className="text-[10px] text-indigo-400 hover:text-indigo-300 ml-auto"
          >
            Clear filters
          </button>
        )}
      </div>
    </div>
  )
}
