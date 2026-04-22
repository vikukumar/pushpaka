'use client'

import { useState, useMemo, useRef, useEffect } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { deploymentsApi, logsApi, aiApi } from '@/lib/api'
import { Header } from '@/components/layout/Header'
import { StatusBadge } from '@/components/dashboard/StatusBadge'
import { LogViewer } from '@/components/dashboard/LogViewer'
import { Deployment, DeploymentLog } from '@/types'
import { timeAgo, formatDate } from '@/lib/utils'
import { ExternalLink, GitBranch, GitCommit, Clock, Loader2, RotateCcw, Sparkles, Terminal, Trash2, ChevronDown, ChevronUp, AlertTriangle, Wrench, RefreshCw, Rocket } from 'lucide-react'
import toast from 'react-hot-toast'
import { useConfirm } from '@/components/ui/Modal'

// ---------------------------------------------------------------------------
// Error pattern detection
// ---------------------------------------------------------------------------

interface DetectedIssue {
  type: 'missing_env' | 'oom' | 'port_conflict' | 'build_failure' | 'dependency' | 'crash'
  summary: string
  detail: string
  suggestedFix?: string
  envVars?: string[]
}

const ERROR_PATTERNS: { pattern: RegExp; issue: (m: RegExpMatchArray) => DetectedIssue }[] = [
  {
    pattern: /(?:process\.env\.|os\.environ\[?['"]?|getenv\()(\w+(?:_URL|_KEY|_SECRET|_TOKEN|_DSN|_URI|_HOST|_PASSWORD|_DB)[^'"\s]*)/i,
    issue: (m) => ({
      type: 'missing_env',
      summary: 'Missing environment variable',
      detail: `The environment variable "${m[1]}" is referenced but may not be set.`,
      suggestedFix: `Add "${m[1]}" to your project's environment variables.`,
      envVars: [m[1]],
    }),
  },
  {
    pattern: /(NEXT_PUBLIC_\w+|SUPABASE_URL|DATABASE_URL|MONGODB_URI|REDIS_URL).*(?:is not defined|required|must be set|undefined|null)/i,
    issue: (m) => ({
      type: 'missing_env',
      summary: 'Missing environment variable',
      detail: `"${m[1]}" is required but not configured.`,
      suggestedFix: `Add "${m[1]}" under Project → Environment Variables.`,
      envVars: [m[1]],
    }),
  },
  {
    pattern: /(?:Error: Your project.*URL and Key are required|supabase.*url.*required)/i,
    issue: () => ({
      type: 'missing_env',
      summary: 'Missing Supabase credentials',
      detail: 'Supabase URL and API key are required.',
      suggestedFix: 'Add NEXT_PUBLIC_SUPABASE_URL and NEXT_PUBLIC_SUPABASE_ANON_KEY to environment variables.',
      envVars: ['NEXT_PUBLIC_SUPABASE_URL', 'NEXT_PUBLIC_SUPABASE_ANON_KEY'],
    }),
  },
  {
    pattern: /(?:killed|OOM|out of memory|cannot allocate|Killed process)/i,
    issue: () => ({
      type: 'oom',
      summary: 'Out of Memory (OOM)',
      detail: 'The process was killed due to memory exhaustion.',
      suggestedFix: 'Increase memory_limit in project resource settings or optimise memory usage.',
    }),
  },
  {
    pattern: /(?:address already in use|EADDRINUSE|port \d+ already)/i,
    issue: () => ({
      type: 'port_conflict',
      summary: 'Port conflict',
      detail: 'The configured port is already bound.',
      suggestedFix: 'Change the port in project settings or ensure existing containers are stopped.',
    }),
  },
  {
    pattern: /(?:npm ERR!|pip install.*failed|cargo build.*error|go build.*error)/i,
    issue: () => ({
      type: 'build_failure',
      summary: 'Build command failed',
      detail: 'The build step encountered an error.',
      suggestedFix: 'Review the full build log for specific error messages.',
    }),
  },
  {
    pattern: /(?:Cannot find module|ModuleNotFoundError|no module named|pkg not found)/i,
    issue: (m) => ({
      type: 'dependency',
      summary: 'Missing dependency',
      detail: `A required module or package could not be found (${m[0].slice(0, 60)}).`,
      suggestedFix: 'Ensure all dependencies are listed in package.json / requirements.txt / go.mod.',
    }),
  },
]

function detectIssues(logs: DeploymentLog[], errorMsg?: string): DetectedIssue[] {
  const text = [
    errorMsg ?? '',
    ...logs.map((l) => l.message),
  ].join('\n')

  const found: DetectedIssue[] = []
  const seen = new Set<string>()

  for (const { pattern, issue } of ERROR_PATTERNS) {
    const m = text.match(pattern)
    if (m) {
      const det = issue(m)
      if (!seen.has(det.summary)) {
        seen.add(det.summary)
        found.push(det)
      }
    }
  }
  return found
}

export default function DeploymentDetailPage() {
  const pathname = usePathname()
  const router = useRouter()
  const id = pathname.split('/')[3] || ''
  const queryClient = useQueryClient()
  const [analyzing, setAnalyzing] = useState(false)
  const [analysis, setAnalysis] = useState<string | null>(null)
  const [showAnalysis, setShowAnalysis] = useState(true)
  const [deleting, setDeleting] = useState(false)
  const [fixLoading, setFixLoading] = useState<string | null>(null)
  const [fixes, setFixes] = useState<Record<string, string>>({})
  const [showMonitor, setShowMonitor] = useState(true)
  const { confirm, Component: ConfirmModal } = useConfirm()

  const { data: deployment, isLoading: deployLoading } = useQuery<Deployment>({
    queryKey: ['deployment', id],
    queryFn: () => deploymentsApi.get(id).then((r) => r.data),
    refetchInterval: (d) => {
      const status = d?.state.data?.status
      if (status === 'building') return 3000
      // Only poll 'queued' for up to 30s; after that it should have been processed or failed
      if (status === 'queued') {
        const created = d?.state.data?.created_at
        const age = created ? Date.now() - new Date(created).getTime() : 0
        return age < 30_000 ? 3000 : false
      }
      return false
    },
  })

  const { data: logsData, refetch: refetchLogs } = useQuery({
    queryKey: ['logs', id],
    queryFn: () => logsApi.get(id).then((r) => r.data),
    refetchInterval: () => {
      if (deployment?.status === 'building') return 2000
      if (deployment?.status === 'queued') {
        const age = deployment?.created_at ? Date.now() - new Date(deployment.created_at).getTime() : 0
        return age < 30_000 ? 2000 : false
      }
      // Keep fetching for 30 s after the deployment finishes to capture any
      // tail logs that were written after the last in-flight poll.
      if (deployment?.finished_at) {
        const age = Date.now() - new Date(deployment.finished_at).getTime()
        if (age < 30_000) return 2000
      }
      return false
    },
  })

  // One-shot re-fetch when the status transitions to a terminal state so the
  // UI always shows the complete log tail even for very short builds.
  const prevStatusRef = useRef<string | undefined>(undefined)
  useEffect(() => {
    const current = deployment?.status
    const prev = prevStatusRef.current
    const isTerminal = current === 'running' || current === 'failed' || current === 'stopped'
    const wasActive = prev === 'building' || prev === 'queued'
    if (isTerminal && wasActive) {
      refetchLogs()
    }
    prevStatusRef.current = current
  }, [deployment?.status, refetchLogs])

  const logs = useMemo<DeploymentLog[]>(() => logsData?.data ?? [], [logsData?.data])
  const isLive = deployment?.status === 'building'

  // Detect issues from logs + error_msg once deployment is failed
  const detectedIssues = useMemo(() => {
    if (!deployment || deployment.status === 'building' || deployment.status === 'queued') return []
    return detectIssues(logs, deployment.error_msg)
  }, [logs, deployment])

  const handleGetFix = async (issue: DetectedIssue) => {
    const key = issue.summary
    setFixLoading(key)
    try {
      const prompt = `Deployment failed with: "${issue.detail}"\nSuggested fix hint: ${issue.suggestedFix ?? 'none'}\nProvide a concise, actionable fix with exact steps or commands.`
      const res = await aiApi.chat(prompt, id)
      setFixes((f) => ({ ...f, [key]: res.data?.reply ?? 'No suggestion returned.' }))
    } catch {
      toast.error('AI fix request failed')
    } finally {
      setFixLoading(null)
    }
  }

  const handleRollback = async () => {
    try {
      await deploymentsApi.rollback(id)
      toast.success('Rollback deployment triggered!')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    } catch {
      toast.error('Rollback failed')
    }
  }

  const handleRestart = async () => {
    const loadingToast = toast.loading('Restarting deployment...')
    try {
      const res = await deploymentsApi.restart(id)
      toast.dismiss(loadingToast)
      toast.success('Restart triggered!')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
      router.push(`/dashboard/deployments/${res.data.id}`)
    } catch {
      toast.dismiss(loadingToast)
      toast.error('Restart failed')
    }
  }

  const handleAnalyze = async () => {
    setAnalyzing(true)
    setAnalysis(null)
    setShowAnalysis(true)
    try {
      const res = await aiApi.analyzeLogs(id)
      setAnalysis(res.data?.analysis ?? 'No analysis returned.')
    } catch (e: any) {
      const errMsg = e.response?.data?.error || 'AI analysis failed. Check your AI configuration in Settings.'
      toast.error(errMsg)
    } finally {
      setAnalyzing(false)
    }
  }

  const handleDelete = async () => {
    const ok = await confirm({
      title: 'Delete Deployment',
      message: 'Are you sure you want to delete this deployment record? The container will not be stopped.',
      confirmText: 'Delete',
      type: 'error'
    })
    if (!ok) return
    setDeleting(true)
    try {
      await deploymentsApi.delete(id)
      toast.success('Deployment deleted')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
      router.push('/dashboard/deployments')
    } catch {
      toast.error('Failed to delete deployment')
    } finally {
      setDeleting(false)
    }
  }

  const handlePromote = async () => {
    try {
      await deploymentsApi.promote(id)
      toast.success('Deployment promoted to Default!')
      queryClient.invalidateQueries({ queryKey: ['deployment', id] })
      queryClient.invalidateQueries({ queryKey: ['project'] })
    } catch (e: any) {
      toast.error(e.response?.data?.error || 'Promotion failed')
    }
  }

  if (deployLoading) {
    return (
      <div className="flex flex-col min-h-screen">
        <Header title="Deployment" />
        <div className="flex justify-center items-center h-64">
          <Loader2 size={24} className="animate-spin text-brand-400" />
        </div>
      </div>
    )
  }

  if (!deployment) {
    return (
      <div className="flex flex-col min-h-screen">
        <Header title="Deployment not found" />
        <div className="p-6"><p className="text-slate-400">Deployment not found.</p></div>
      </div>
    )
  }

  return (
    <div className="flex flex-col min-h-screen">
      <Header
        title={`Deployment ${deployment.id.slice(0, 8)}`}
        subtitle={`Project ${deployment.project_id.slice(0, 8)}`}
      />

      <div className="p-4 md:p-6 space-y-4 md:space-y-5 pb-24 md:pb-6">
        {/* Header card */}
        <div className="card">
          <div className="flex items-start justify-between flex-wrap gap-4">
            <div className="space-y-2 min-w-0 flex-1">
              <div className="flex items-center gap-3 flex-wrap">
                <StatusBadge status={deployment.status} />
                <span className="text-xs text-slate-500 font-mono truncate max-w-full">{deployment.id}</span>
              </div>

              <div className="flex flex-wrap items-center gap-3 text-sm text-slate-400">
                <span className="flex items-center gap-1.5">
                  <GitBranch size={13} />
                  {deployment.branch}
                </span>
                {deployment.commit_sha && (
                  <span className="flex items-center gap-1.5 font-mono">
                    <GitCommit size={13} />
                    {deployment.commit_sha.slice(0, 7)}
                  </span>
                )}
                <span className="flex items-center gap-1.5">
                  <Clock size={13} />
                  {timeAgo(deployment.created_at)}
                </span>
              </div>

              {deployment.commit_msg && (
                <p className="text-sm text-slate-300 italic">&ldquo;{deployment.commit_msg}&rdquo;</p>
              )}

              {deployment.error_msg && (
                <p className="text-sm text-red-400 bg-red-500/10 rounded px-2 py-1">
                  {deployment.error_msg}
                </p>
              )}
            </div>

            {/* Action buttons — wrap on mobile */}
            <div className="flex flex-wrap items-center gap-2 w-full sm:w-auto">
              {deployment.url && (
                <a
                  href={deployment.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="btn-secondary text-sm flex-1 sm:flex-none justify-center"
                >
                  <ExternalLink size={14} />
                  Open
                </a>
              )}
              <button className="btn-secondary text-sm flex-1 sm:flex-none justify-center" onClick={handleRollback}>
                <RotateCcw size={14} />
                Rollback
              </button>
              <button className="btn-secondary text-sm flex-1 sm:flex-none justify-center" onClick={handleRestart}>
                <RefreshCw size={14} />
                Restart
              </button>
              {deployment.status === 'running' && !deployment.is_default && (
                <button
                  className="btn-primary text-sm flex-1 sm:flex-none justify-center"
                  onClick={handlePromote}
                  title="Promote to Default (Live)"
                >
                  <Rocket size={14} />
                  Promote
                </button>
              )}
              <button
                className="btn-secondary text-sm flex-1 sm:flex-none justify-center"
                onClick={handleAnalyze}
                disabled={analyzing}
                title="Analyze logs with AI"
              >
                {analyzing
                  ? <Loader2 size={14} className="animate-spin" />
                  : <Sparkles size={14} />}
                Analyze
              </button>
              <a
                href={`/dashboard/deployments/${id}/terminal`}
                className="btn-secondary text-sm flex-1 sm:flex-none justify-center"
                title="Open web terminal"
              >
                <Terminal size={14} />
                Terminal
              </a>
              <button
                className="btn-danger text-sm flex-1 sm:flex-none justify-center"
                onClick={handleDelete}
                disabled={deleting}
                title="Delete deployment"
              >
                {deleting ? <Loader2 size={14} className="animate-spin" /> : <Trash2 size={14} />}
                Delete
              </button>
            </div>
          </div>

          {/* Timeline */}
          <div className="mt-4 grid grid-cols-3 gap-3 pt-4 border-t border-surface-border">
            {[
              { label: 'Created', value: formatDate(deployment.created_at) },
              { label: 'Started',  value: formatDate(deployment.started_at) },
              { label: 'Finished', value: formatDate(deployment.finished_at) },
            ].map(({ label, value }) => (
              <div key={label}>
                <div className="text-xs text-slate-500 mb-0.5">{label}</div>
                <div className="text-xs text-slate-300">{value}</div>
              </div>
            ))}
          </div>
        </div>

        {/* AI Monitor — auto-detected issues */}
        {detectedIssues.length > 0 && (
          <div className="card" style={{ borderColor: 'rgba(245,158,11,0.4)', background: 'rgba(245,158,11,0.05)' }}>
            <div
              className="flex items-center justify-between mb-3 cursor-pointer"
              onClick={() => setShowMonitor((v) => !v)}
            >
              <div className="flex items-center gap-2">
                <AlertTriangle size={14} className="text-amber-400" />
                <span className="text-sm font-semibold text-slate-200">
                  AI Monitor detected {detectedIssues.length} issue{detectedIssues.length > 1 ? 's' : ''}
                </span>
              </div>
              {showMonitor ? <ChevronUp size={14} className="text-slate-500" /> : <ChevronDown size={14} className="text-slate-500" />}
            </div>

            {showMonitor && (
              <div className="space-y-4">
                {detectedIssues.map((issue) => (
                  <div key={issue.summary} className="rounded-lg p-3 bg-amber-500/10 border border-amber-500/20">
                    <div className="flex items-start justify-between gap-3 flex-wrap">
                      <div className="min-w-0 flex-1">
                        <p className="text-sm font-semibold text-amber-300">{issue.summary}</p>
                        <p className="text-xs text-slate-400 mt-0.5">{issue.detail}</p>
                        {issue.envVars && (
                          <div className="flex flex-wrap gap-1.5 mt-2">
                            {issue.envVars.map((v) => (
                              <code key={v} className="text-xs bg-slate-800 text-brand-300 px-1.5 py-0.5 rounded font-mono">{v}</code>
                            ))}
                          </div>
                        )}
                        {fixes[issue.summary] && (
                          <div className="mt-3 p-2 rounded bg-slate-800/60 border border-slate-700">
                            <p className="text-xs text-slate-300 font-semibold mb-1 flex items-center gap-1.5">
                              <Sparkles size={11} className="text-brand-400" /> AI Fix Suggestion
                            </p>
                            <p className="text-xs text-slate-300 whitespace-pre-wrap leading-relaxed font-sans">{fixes[issue.summary]}</p>
                          </div>
                        )}
                      </div>
                      <button
                        onClick={() => handleGetFix(issue)}
                        disabled={fixLoading === issue.summary}
                        className="btn-secondary text-xs shrink-0"
                      >
                        {fixLoading === issue.summary
                          ? <Loader2 size={12} className="animate-spin" />
                          : <Wrench size={12} />}
                        {fixes[issue.summary] ? 'Refresh Fix' : 'Get AI Fix'}
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </div>
        )}

        {/* AI Analysis panel */}
        {analysis && (
          <div
            className="card"
            style={{ borderColor: 'rgba(99,102,241,0.3)', background: 'rgba(99,102,241,0.05)' }}
          >
            <div
              className="flex items-center justify-between mb-3 cursor-pointer"
              onClick={() => setShowAnalysis((v) => !v)}
            >
              <div className="flex items-center gap-2">
                <Sparkles size={14} className="text-brand-400" />
                <span className="text-sm font-semibold text-slate-200">AI Log Analysis</span>
              </div>
              {showAnalysis ? <ChevronUp size={14} className="text-slate-500" /> : <ChevronDown size={14} className="text-slate-500" />}
            </div>
            {showAnalysis && (
              <pre className="text-sm text-slate-300 whitespace-pre-wrap leading-relaxed font-sans">
                {analysis}
              </pre>
            )}
          </div>
        )}

        {/* Logs */}
        <LogViewer
          live={isLive}
          deploymentId={id}
        />
      </div>
      {ConfirmModal}
    </div>
  )
}
