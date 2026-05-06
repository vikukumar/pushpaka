'use client'

import { useState } from 'react'
import { usePathname } from 'next/navigation'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import Link from 'next/link'
import { projectsApi, deploymentsApi, tasksApi, registryApi } from '@/lib/api'
import { Header } from '@/components/layout/Header'
import { StatusBadge } from '@/components/dashboard/StatusBadge'
import { TaskProgress } from '@/components/dashboard/TaskProgress'
import { RegistryRepoList } from '@/components/dashboard/RegistryRepoList'
import { RegistrySettings } from '@/components/dashboard/RegistrySettings'
import { useConfirm } from '@/components/ui/Modal'
import { timeAgo } from '@/lib/utils'
import toast from 'react-hot-toast'
import { ProjectTask } from '@/types'
import {
  Rocket, GitBranch, Globe, Key, Settings,
  ExternalLink, RefreshCw, Loader2, ChevronRight, GitCommit, Code2, AlertTriangle, CheckCircle2, XCircle
} from 'lucide-react'

export default function ProjectDetailPage() {
  const pathname = usePathname()
  // useParams() returns the build-time placeholder '_' in the static export
  // because serverProvidedParams is baked into the pre-rendered RSC payload.
  // Read the real UUID from position 3 of the pathname instead.
  const id = pathname.split('/')[3] || ''
  const queryClient = useQueryClient()
  const [deployLoading, setDeployLoading] = useState(false)
  const [registryTab, setRegistryTab] = useState<'repos' | 'settings'>('repos')

  const { data: projectData, isLoading } = useQuery({
    queryKey: ['project', id],
    queryFn: () => projectsApi.get(id).then((r) => r.data),
  })

  const { data: deploymentsData } = useQuery({
    queryKey: ['deployments', 'project', id],
    queryFn: () => deploymentsApi.list(5, 0, id).then((r) => r.data),
    refetchInterval: 5000,
  })

  const { data: tasksData } = useQuery({
    queryKey: ['tasks', id],
    queryFn: () => tasksApi.list(id).then((r) => r.data),
    refetchInterval: 3000,
  })
  const { confirm, Component: ConfirmModal } = useConfirm()

  const project = projectData
  const deployments = deploymentsData?.data || []
  const tasks: ProjectTask[] = tasksData?.data || []

  // Only consider the LATEST task of each type to determine pipeline status
  const latestTaskByType = tasks.reduce((acc, t) => {
    if (!acc[t.type] || new Date(t.created_at) > new Date(acc[t.type].created_at)) {
      acc[t.type] = t
    }
    return acc
  }, {} as Record<string, ProjectTask>)

  const latestTasks = Object.values(latestTaskByType)
  const isPipelineRunning = latestTasks.some(t => t.status === 'running' || t.status === 'pending')
  const isPipelineFailed = latestTasks.some(t => t.status === 'failed')
  const isPipelineComplete = latestTasks.some(t => t.type === 'test' && t.status === 'completed')
  const hasBuildArtifact = latestTasks.some(t => t.type === 'build' && t.status === 'completed')

  const latestTask = tasks.length > 0 ? tasks[tasks.length - 1] : null

  const latestDeployment = deployments[0]
  const projectHealth = (() => {
    if (isPipelineRunning) return 'inprogress'
    if (latestDeployment?.status === 'failed' || (isPipelineFailed && !latestDeployment)) return 'failed'
    if (latestDeployment?.status === 'running') {
      if (isPipelineFailed) return 'degraded'
      return 'healthy'
    }
    return 'unknown'
  })()

  const handleDeploy = async () => {
    // Smart Deploy Logic
    const testTask = [...tasks].reverse().find(t => t.type === 'test')

    if (testTask) {
      if (testTask.status === 'failed') {
        const ok = await confirm({
          title: 'Deployment Warning',
          message: 'The latest tests for this project have FAILED. Are you sure you want to deploy this code anyway?',
          confirmText: 'Deploy Anyway',
          type: 'error'
        })
        if (!ok) return
      } else if (testTask.status !== 'completed') {
        const ok = await confirm({
          title: 'Tests Incomplete',
          message: 'Automated testing is still in progress (or pending). Would you like to bypass tests and deploy immediately?',
          confirmText: 'Deploy Now (Bypass Tests)',
          type: 'warn'
        })
        if (!ok) return
      }
    } else {
      // No test task found at all
      const ok = await confirm({
        title: 'No Test Results',
        message: 'No test results were found for the current commit. Proceed with deployment?',
        confirmText: 'Proceed to Deploy',
        type: 'confirm'
      })
      if (!ok) return
    }

    setDeployLoading(true)
    try {
      await deploymentsApi.trigger({ project_id: id })
      toast.success('Deployment triggered!')
      queryClient.invalidateQueries({ queryKey: ['deployments'] })
    } catch {
      toast.error('Failed to trigger deployment')
    } finally {
      setDeployLoading(false)
    }
  }

  const handleSync = async () => {
    const loadingToast = toast.loading('Triggering synchronization task...')
    try {
      await projectsApi.sync(id)
      toast.dismiss(loadingToast)
      toast.success('Synchronization task started!')
      queryClient.invalidateQueries({ queryKey: ['tasks', id] })
    } catch (err: any) {
      toast.dismiss(loadingToast)
      toast.error(err.response?.data?.error || 'Failed to trigger sync task')
    }
  }

  if (isLoading) {
    return (
      <div className="flex flex-col min-h-screen">
        <Header title="Project" />
        <div className="p-6 flex items-center justify-center h-64">
          <Loader2 size={24} className="animate-spin text-brand-400" />
        </div>
      </div>
    )
  }

  if (!project) {
    return (
      <div className="flex flex-col min-h-screen">
        <Header title="Project not found" />
        <div className="p-6">
          <p className="text-slate-400">Project not found or you don&apos;t have access.</p>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col min-h-screen animate-fade-in">
      <Header
        title={project.name}
        subtitle={
          <div className="flex items-center gap-4 min-w-0">
            <span className="opacity-70 truncate max-w-[200px] sm:max-w-md">{project.repo_url}</span>
            <div className={`flex items-center gap-1.5 px-2 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider border ${
              projectHealth === 'healthy' ? 'bg-emerald-500/10 text-emerald-400 border-emerald-500/20' :
              projectHealth === 'inprogress' ? 'bg-brand-500/10 text-brand-400 border-brand-500/20 animate-pulse' :
              projectHealth === 'degraded' ? 'bg-amber-500/10 text-amber-400 border-amber-500/20' :
              projectHealth === 'failed' ? 'bg-red-500/10 text-red-400 border-red-500/20' :
              'bg-slate-800 text-slate-400 border-slate-700'
            }`}>
              {projectHealth === 'healthy' && <CheckCircle2 size={10} />}
              {projectHealth === 'inprogress' && <Loader2 size={10} className="animate-spin" />}
              {projectHealth === 'degraded' && <AlertTriangle size={10} />}
              {projectHealth === 'failed' && <XCircle size={10} />}
              {projectHealth === 'inprogress' ? 'In Progress' : projectHealth}
            </div>
          </div>
        }
      />

      <div className="p-4 md:p-6 space-y-6">
        {project.type === 'registry' ? (
          <>
            <div className="flex gap-1 p-1 bg-slate-900 border border-slate-800 rounded-xl w-fit">
              <button 
                onClick={() => setRegistryTab('repos')}
                className={`px-4 py-2 text-xs font-medium rounded-lg transition-all ${
                  registryTab === 'repos' ? 'bg-brand-600 text-white shadow-lg' : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                Repositories
              </button>
              <button 
                onClick={() => setRegistryTab('settings')}
                className={`px-4 py-2 text-xs font-medium rounded-lg transition-all ${
                  registryTab === 'settings' ? 'bg-brand-600 text-white shadow-lg' : 'text-slate-400 hover:text-slate-200'
                }`}
              >
                Instructions
              </button>
            </div>

            {registryTab === 'repos' ? (
              <RegistryRepoList projectId={id} />
            ) : (
              <RegistrySettings projectId={id} />
            )}
          </>
        ) : (
          <>
            {/* Task Progress - NEW */}
            <TaskProgress tasks={tasks} onRefresh={() => queryClient.invalidateQueries({ queryKey: ['tasks', id] })} />

            {/* Actions row */}
            <div className="flex items-center gap-3 flex-wrap">
              {isPipelineComplete || (isPipelineFailed && hasBuildArtifact) ? (
                <button
                  onClick={handleDeploy}
                  disabled={deployLoading}
                  className={`btn-primary ${isPipelineFailed ? 'bg-red-600 hover:bg-red-700' : ''}`}
                >
                  {deployLoading ? (
                    <Loader2 size={15} className="animate-spin" />
                  ) : (
                    <Rocket size={15} />
                  )}
                  {isPipelineFailed ? 'Force Deploy' : 'Deploy Now'}
                </button>
              ) : isPipelineRunning ? (
                <div className="flex items-center gap-2 px-4 py-2 bg-slate-800 rounded-lg text-slate-400 text-sm border border-slate-700">
                  <Loader2 size={15} className="animate-spin text-brand-400" />
                  Processing Pipeline...
                </div>
              ) : (
                <button
                  onClick={handleSync}
                  className="btn-primary"
                >
                  <RefreshCw size={14} />
                  {tasks.length === 0 ? 'Start Sync' : 'Re-sync'}
                </button>
              )}

              {tasks.length > 0 && !isPipelineRunning && (
                <button
                  onClick={handleSync}
                  className="btn-secondary"
                >
                  <RefreshCw size={14} />
                  Sync
                </button>
              )}

              <Link href={`/dashboard/projects/${id}/deployments`} className="btn-secondary">
                <RefreshCw size={14} />
                Deployments
              </Link>
              
              <div className="relative group">
                <button className="btn-secondary flex items-center gap-2">
                  <Settings size={14} />
                  Configure
                  <ChevronRight size={14} className="rotate-90" />
                </button>
                
                <div className="absolute top-full left-0 mt-2 w-48 bg-slate-900 border border-slate-800 rounded-xl shadow-2xl opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-50 p-1 overflow-hidden backdrop-blur-xl">
                  <Link href={`/dashboard/projects/${id}/env`} className="flex items-center gap-3 px-4 py-2.5 text-sm text-slate-300 hover:bg-slate-800 rounded-lg transition-colors">
                    <Key size={14} className="text-brand-400" />
                    Env Vars
                  </Link>
                  <Link href={`/dashboard/projects/${id}/domains`} className="flex items-center gap-3 px-4 py-2.5 text-sm text-slate-300 hover:bg-slate-800 rounded-lg transition-colors">
                    <Globe size={14} className="text-brand-400" />
                    Domains
                  </Link>
                  <Link href={`/dashboard/projects/${id}/settings`} className="flex items-center gap-3 px-4 py-2.5 text-sm text-slate-300 hover:bg-slate-800 rounded-lg transition-colors border-t border-white/5 mt-1 pt-3">
                    <Settings size={14} className="text-brand-400" />
                    Project Settings
                  </Link>
                </div>
              </div>

              <Link href={`/dashboard/projects/${id}/editor`} className="btn-secondary">
                <Code2 size={14} />
                Editor
              </Link>
            </div>

            {/* Project info */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="card">
                <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider mb-4">
                  Project Details
                </h3>
                <dl className="space-y-3">
                  {[
                    { label: 'Repository', value: project.repo_url, mono: true },
                    { label: 'Branch', value: project.branch },
                    { label: 'Latest Commit', value: project.latest_commit_sha ? `${project.latest_commit_sha.slice(0, 7)}: ${project.latest_commit_msg}` : 'Checking remote...', mono: true },
                    { label: 'Last Synced', value: project.latest_commit_at ? timeAgo(project.latest_commit_at) : 'Never' },
                    { label: 'Framework', value: project.framework || 'Auto-detect' },
                    { label: 'Port', value: project.port?.toString() || '3000' },
                    { label: 'Build Command', value: project.build_command || '', mono: true },
                    { label: 'Start Command', value: project.start_command || '', mono: true },
                  ].map(({ label, value, mono }) => (
                    <div key={label} className="flex flex-col sm:flex-row sm:items-start gap-1 sm:gap-3 py-1.5 border-b border-white/[0.03] last:border-0">
                      <dt className="text-slate-500 text-xs w-full sm:w-36 shrink-0">{label}</dt>
                      <dd className={`text-sm text-slate-200 break-all flex-1 ${mono ? 'font-mono' : ''}`}>{value}</dd>
                    </div>
                  ))}
                </dl>
              </div>

              {/* Latest deployments */}
              <div className="card">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">
                    Recent Deployments
                  </h3>
                  <Link
                    href={`/dashboard/projects/${id}/deployments`}
                    className="text-xs text-slate-500 hover:text-brand-400 flex items-center gap-1"
                  >
                    View all <ChevronRight size={12} />
                  </Link>
                </div>

                {deployments.length === 0 ? (
                  <div className="text-center py-8">
                    <Rocket size={28} className="mx-auto text-slate-700 mb-2" />
                    <p className="text-slate-500 text-sm">No deployments yet</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    {deployments.map((d: any) => (
                      <Link
                        key={d.id}
                        href={`/dashboard/deployments/${d.id}`}
                        className="flex items-center gap-3 p-2.5 rounded-lg hover:bg-slate-800 transition-colors"
                      >
                        <StatusBadge status={d.status} />
                        <div className="flex-1 min-w-0 text-xs">
                          <div className="text-slate-300 flex items-center gap-1.5">
                            <GitBranch size={10} /> {d.branch}
                            {d.commit_sha && (
                              <>
                                <GitCommit size={10} /> {d.commit_sha.slice(0, 7)}
                              </>
                            )}
                          </div>
                          <div className="text-slate-600">{timeAgo(d.created_at)}</div>
                        </div>
                        {d.url && (
                          <a
                            href={d.url}
                            target="_blank"
                            rel="noopener noreferrer"
                            className="text-slate-600 hover:text-brand-400"
                            onClick={(e) => e.stopPropagation()}
                          >
                            <ExternalLink size={12} />
                          </a>
                        )}
                      </Link>
                    ))}
                  </div>
                )}
              </div>
            </div>
          </>
        )}
      </div>
      {ConfirmModal}
    </div>
  )
}


// Required for Next.js static export with dynamic segments.
export function generateStaticParams() {
  return []
}
