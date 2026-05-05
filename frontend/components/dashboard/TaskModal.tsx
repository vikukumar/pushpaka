'use client'

import { useState, useEffect, useRef } from 'react'
import { ProjectTask, DeploymentLog, TaskType } from '@/types'
import { logsApi, tasksApi } from '@/lib/api'
import { X, Terminal, Clock, AlertCircle, CheckCircle2, Loader2, RefreshCw, Download, ScrollText } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'
import { createPortal } from 'react-dom'

interface TaskModalProps {
  task: ProjectTask
  isOpen: boolean
  onClose: () => void
  onRestart?: () => void
}

const taskLabels: Record<TaskType, string> = {
  sync: 'Repository Synchronization',
  fetch: 'Metadata Fetch',
  build: 'Project Build',
  test: 'Automated Testing',
  deploy: 'System Deployment',
  aifix: 'Autonomous AI Repair',
}

export function TaskModal({ task, isOpen, onClose, onRestart }: TaskModalProps) {
  const [logs, setLogs] = useState<DeploymentLog[]>([])
  const [loading, setLoading] = useState(true)
  const [restarting, setRestarting] = useState(false)
  const [mounted, setMounted] = useState(false)
  const scrollRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    setMounted(true)
  }, [])

  useEffect(() => {
    if (isOpen && task) {
      fetchLogs()
      const interval = setInterval(fetchLogs, 3000)
      return () => clearInterval(interval)
    }
  }, [isOpen, task?.id])

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [logs])

  const fetchLogs = async () => {
    try {
      const res = await logsApi.get(task.id)
      if (res.data?.data) {
        setLogs(res.data.data)
      }
    } catch (err) {
      console.error('Failed to fetch logs:', err)
    } finally {
      setLoading(false)
    }
  }

  const handleRestart = async () => {
    setRestarting(true)
    try {
      await tasksApi.restart(task.id)
      if (onRestart) onRestart()
      onClose()
    } catch (err) {
      console.error('Failed to restart task:', err)
    } finally {
      setRestarting(false)
    }
  }

  const downloadLogs = () => {
    const logText = logs.map(l => `[${l.created_at}] [${l.level}] ${l.message}`).join('\n')
    const blob = new Blob([logText], { type: 'text/plain' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `task-${task.id}-logs.txt`
    document.body.appendChild(a)
    a.click()
    document.body.removeChild(a)
    URL.revokeObjectURL(url)
  }

  if (!isOpen || !mounted) return null

  const duration = task.finished_at && task.started_at 
    ? formatDistanceToNow(new Date(task.started_at), { addSuffix: false })
    : task.started_at ? 'Running...' : 'Pending'

  return createPortal(
    <div className="fixed inset-0 z-[999] flex items-center justify-center p-4 bg-slate-950/80 backdrop-blur-sm animate-in fade-in duration-200">
      <div className="bg-slate-900 border border-slate-800 rounded-xl w-full max-w-4xl max-h-[90vh] flex flex-col shadow-2xl overflow-hidden scale-in duration-200">
        {/* Header content continues... */}
        <div className="p-4 border-b border-slate-800 flex items-center justify-between bg-slate-900/50">
          <div className="flex items-center gap-3">
            <div className={`p-2 rounded-lg ${
              task.status === 'completed' ? 'bg-brand-500/10 text-brand-500' :
              task.status === 'failed' ? 'bg-red-500/10 text-red-500' :
              task.status === 'running' ? 'bg-brand-500/20 text-brand-500 animate-pulse' :
              'bg-slate-800 text-slate-400'
            }`}>
              {task.status === 'running' ? <Loader2 size={20} className="animate-spin" /> : 
               task.status === 'completed' ? <CheckCircle2 size={20} /> :
               task.status === 'failed' ? <AlertCircle size={20} /> : <Clock size={20} />}
            </div>
            <div>
              <h2 className="text-lg font-bold text-slate-100">{taskLabels[task.type]}</h2>
              <p className="text-xs text-slate-500 font-mono uppercase tracking-widest">{task.id.slice(0, 8)} • {task.status}</p>
            </div>
          </div>
          <button onClick={onClose} className="p-2 hover:bg-slate-800 rounded-lg text-slate-400 transition-colors">
            <X size={20} />
          </button>
        </div>

        {/* Stats bar */}
        <div className="px-6 py-3 bg-slate-900/30 border-b border-slate-800 flex items-center gap-6 text-sm">
          <div className="flex items-center gap-2 text-slate-400">
            <Clock size={14} />
            <span>Duration: <span className="text-slate-200">{duration}</span></span>
          </div>
          {task.worker_id && (
            <div className="flex items-center gap-2 text-slate-400">
              <ScrollText size={14} />
              <span>Worker: <span className="text-slate-200 font-mono">{task.worker_id}</span></span>
            </div>
          )}
          <div className="ml-auto flex items-center gap-2">
            <button 
              onClick={downloadLogs}
              disabled={logs.length === 0}
              className="flex items-center gap-2 px-3 py-1.5 rounded-lg border border-slate-700 hover:border-slate-600 hover:bg-slate-800 disabled:opacity-50 transition-all text-xs text-slate-300"
            >
              <Download size={14} />
              Export
            </button>
            {(task.status === 'failed' || task.status === 'completed') && (
              <button 
                onClick={handleRestart}
                disabled={restarting}
                className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-brand-500 hover:bg-brand-600 disabled:opacity-50 transition-all text-xs font-bold text-white shadow-lg shadow-brand-500/20"
              >
                {restarting ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                Restart Task
              </button>
            )}
          </div>
        </div>

        {/* Console / Logs */}
        <div className="flex-1 bg-black p-4 font-mono text-sm overflow-hidden flex flex-col">
          <div className="flex items-center gap-2 mb-3 text-slate-500 border-b border-slate-900 pb-2">
            <Terminal size={14} />
            <span className="text-xs uppercase tracking-wider">Console Output</span>
          </div>
          
          <div 
            ref={scrollRef}
            className="flex-1 overflow-y-auto space-y-1 custom-scrollbar"
          >
            {loading ? (
              <div className="flex items-center justify-center h-full text-slate-600 animate-pulse">
                Initializing logs...
              </div>
            ) : logs.length === 0 ? (
              <div className="py-8 text-center text-slate-700 bg-slate-950/50 rounded-lg border border-slate-900 border-dashed">
                Waiting for logs to stream from the worker node...
              </div>
            ) : (
              logs.map((log, i) => (
                <div key={log.id || i} className="group flex gap-4 hover:bg-white/5 px-2 rounded transition-colors py-0.5">
                  <span className="text-slate-600 w-24 shrink-0 select-none">
                    {new Date(log.created_at).toLocaleTimeString([], { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })}
                  </span>
                  <span className={`
                    ${log.level === 'error' ? 'text-red-400' : 
                      log.level === 'warn' ? 'text-yellow-400' : 
                      log.stream === 'system' ? 'text-brand-400 font-bold' :
                      'text-slate-300'}
                  `}>
                    {log.message}
                  </span>
                </div>
              ))
            )}
          </div>
        </div>

        {/* Footer info */}
        {task.error && (
          <div className="p-4 bg-red-500/5 border-t border-red-500/20 text-red-400 flex items-start gap-3">
            <AlertCircle size={18} className="shrink-0 mt-0.5" />
            <div className="text-xs">
              <p className="font-bold mb-1">Execution Error:</p>
              <p className="font-mono bg-black/40 p-2 rounded border border-red-500/10 whitespace-pre-wrap">{task.error}</p>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body
  )
}
