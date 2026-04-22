'use client'

import { useQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { projectsApi, deploymentsApi } from '@/lib/api'
import { Project, Deployment } from '@/types'
import { Header } from '@/components/layout/Header'
import { ProjectCard } from '@/components/dashboard/ProjectCard'
import { StatusBadge } from '@/components/dashboard/StatusBadge'
import { SystemStatus } from '@/components/dashboard/SystemStatus'
import { timeAgo } from '@/lib/utils'
import { Plus, Rocket, FolderGit2, Activity, TrendingUp } from 'lucide-react'

const statDefs = [
  {
    label: 'Projects',
    key: 'totalProjects' as const,
    icon: FolderGit2,
    gradient: 'linear-gradient(135deg, #818cf8, #a5b4fc)',
    glow: 'rgba(99,102,241,0.35)',
    iconBg: 'rgba(99,102,241,0.12)',
    iconBorder: 'rgba(99,102,241,0.25)',
    orb: 'rgba(99,102,241,0.18)',
  },
  {
    label: 'Running',
    key: 'activeDeployments' as const,
    icon: Activity,
    gradient: 'linear-gradient(135deg, #34d399, #6ee7b7)',
    glow: 'rgba(52,211,153,0.35)',
    iconBg: 'rgba(52,211,153,0.1)',
    iconBorder: 'rgba(52,211,153,0.25)',
    orb: 'rgba(52,211,153,0.15)',
  },
  {
    label: 'Building',
    key: 'buildingDeployments' as const,
    icon: TrendingUp,
    gradient: 'linear-gradient(135deg, #fbbf24, #fcd34d)',
    glow: 'rgba(251,191,36,0.35)',
    iconBg: 'rgba(251,191,36,0.1)',
    iconBorder: 'rgba(251,191,36,0.25)',
    orb: 'rgba(251,191,36,0.15)',
  },
  {
    label: 'Failed',
    key: 'failedDeployments' as const,
    icon: Rocket,
    gradient: 'linear-gradient(135deg, #f87171, #fca5a5)',
    glow: 'rgba(248,113,113,0.35)',
    iconBg: 'rgba(248,113,113,0.1)',
    iconBorder: 'rgba(248,113,113,0.25)',
    orb: 'rgba(248,113,113,0.15)',
  },
]

export default function DashboardPage() {
  const { data: projectsData } = useQuery({
    queryKey: ['projects'],
    queryFn: () => projectsApi.list().then((r) => r.data),
    refetchInterval: 10000, // Sync every 10s as requested
  })
  const { data: deploymentsData } = useQuery({
    queryKey: ['deployments'],
    queryFn: () => deploymentsApi.list(10).then((r) => r.data),
    refetchInterval: 5000, // Live updates every 5s
  })

  const { data: statsData } = useQuery({
    queryKey: ['deployments-stats'],
    queryFn: () => deploymentsApi.stats().then((r) => r.data),
    refetchInterval: 5000,
  })

  const projects: Project[]    = projectsData?.data    || []
  const deployments: Deployment[] = deploymentsData?.data || []
  const depStats = statsData || {}

  const stats = {
    totalProjects:       projects.length,
    activeDeployments:   depStats['running'] || 0,
    buildingDeployments: depStats['building'] || 0,
    failedDeployments:   depStats['failed'] || 0,
  }

  return (
    <div className="flex flex-col min-h-screen animate-fade-in">
      <Header title="Overview" subtitle="Your deployment platform" />

      <div className="p-6 space-y-6 animate-fade-in">

        {/* """ Stat cards """ */}
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {statDefs.map(({ label, key, icon: Icon, gradient, glow, iconBg, iconBorder, orb }, i) => (
            <div 
              key={label} 
              className={`card relative overflow-hidden group animate-slide-up delay-${(i + 1) * 100}`}
            >
              {/* Ambient orb */}
              <div
                className="absolute -top-6 -right-6 w-20 h-20 rounded-full pointer-events-none opacity-60 group-hover:opacity-100 transition-opacity"
                style={{ background: `radial-gradient(circle, ${orb} 0%, transparent 70%)` }}
              />
              {/* Icon */}
              <div
                className="w-10 h-10 rounded-xl flex items-center justify-center mb-4 transition-transform group-hover:scale-110"
                style={{
                  background: iconBg,
                  border: `1px solid ${iconBorder}`,
                  boxShadow: `0 0 16px -4px ${glow}`,
                }}
              >
                <Icon size={18} style={{ background: gradient, WebkitBackgroundClip: 'text', WebkitTextFillColor: 'transparent', backgroundClip: 'text' }} />
              </div>
              {/* Value */}
              <div
                className="text-4xl font-bold tracking-tight mb-1"
                style={{
                  background: gradient,
                  WebkitBackgroundClip: 'text',
                  WebkitTextFillColor: 'transparent',
                  backgroundClip: 'text',
                  textShadow: 'none',
                }}
              >
                {stats[key]}
              </div>
              <p className="text-xs text-slate-600 font-medium tracking-wider uppercase">{label}</p>
            </div>
          ))}
        </div>

        {/* """ Main grid """ */}
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">

          {/* Projects */}
          <div className="lg:col-span-1 space-y-4 animate-slide-up delay-400">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Projects</h2>
              <Link href="/dashboard/projects/new" className="btn-primary text-xs py-1.5 px-3">
                <Plus size={13} />
                New
              </Link>
            </div>
            {projects.length === 0 ? (
              <div className="card text-center py-10">
                <FolderGit2 size={28} className="mx-auto text-slate-700 mb-3" />
                <p className="text-slate-500 text-sm">No projects yet</p>
                <Link href="/dashboard/projects/new" className="btn-primary mt-4 inline-flex text-xs">
                  <Plus size={13} /> Create project
                </Link>
              </div>
            ) : (
              <div className="space-y-3">
                {projects.slice(0, 4).map((project) => (
                  <ProjectCard
                    key={project.id}
                    project={project}
                    latestDeployment={deployments.find((d) => d.project_id === project.id)}
                    runningCount={deployments.filter(d => d.project_id === project.id && d.status === 'running').length}
                    buildingCount={deployments.filter(d => d.project_id === project.id && d.status === 'building').length}
                    failedCount={deployments.filter(d => d.project_id === project.id && d.status === 'failed').length}
                  />
                ))}
              </div>
            )}
          </div>

          {/* Recent Deployments */}
          <div className="lg:col-span-1 space-y-4 animate-slide-up delay-400">
            <div className="flex items-center justify-between">
              <h2 className="text-sm font-semibold text-slate-300 uppercase tracking-wider">Deployments</h2>
              <Link href="/dashboard/deployments" className="text-[11px] text-slate-600 hover:text-slate-300 transition-colors">
                View all
              </Link>
            </div>
            {deployments.length === 0 ? (
              <div className="card text-center py-10">
                <Rocket size={28} className="mx-auto text-slate-700 mb-3" />
                <p className="text-slate-500 text-sm">No deployments yet</p>
              </div>
            ) : (
              <div className="rounded-xl overflow-hidden bg-deployment-list transition-all shadow-sm">
                {deployments.slice(0, 7).map((d, i) => {
                  const project = projects.find((p) => p.id === d.project_id)
                  return (
                    <Link
                      key={d.id}
                      href={`/dashboard/deployments/${d.id}`}
                      className="flex items-center gap-3 px-4 py-3 transition-all duration-150 group border-b border-[var(--border-subtle)] last:border-0 hover:bg-[var(--brand-glow)]"
                    >
                      <StatusBadge status={d.status} />
                      <div className="flex-1 min-w-0">
                        <div className="text-xs font-medium text-slate-200 truncate group-hover:text-[var(--brand-primary)] transition-colors">
                          {project?.name || 'Unknown'}
                        </div>
                        <div className="text-[10px] text-slate-600">{timeAgo(d.created_at)}</div>
                      </div>
                      <div className="text-[10px] text-slate-700 font-mono shrink-0">{d.id.slice(0, 7)}</div>
                    </Link>
                  )
                })}
              </div>
            )}
          </div>

          {/* System Status */}
          <div className="lg:col-span-1 animate-slide-up delay-400">
            <SystemStatus />
          </div>
        </div>
      </div>
    </div>
  )
}

