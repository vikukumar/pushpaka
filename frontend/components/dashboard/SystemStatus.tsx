'use client'

import { useQuery } from '@tanstack/react-query'
import { systemApi } from '@/lib/api'
import { SystemInfo } from '@/types'
import { Container, GitBranch, Server, RefreshCw, Zap, Brain, Activity } from 'lucide-react'

function StatusDot({ ok, pulse = false }: { ok: boolean; pulse?: boolean }) {
  const color = ok ? '#4ade80' : '#f87171'
  const glow = ok ? 'rgba(74,222,128,0.6)' : 'rgba(248,113,113,0.6)'
  return (
    <span
      className={pulse && ok ? 'animate-glow-pulse' : ''}
      style={{
        display: 'inline-block',
        width: 8, height: 8,
        borderRadius: '50%',
        background: color,
        boxShadow: `0 0 8px ${glow}`,
        flexShrink: 0,
      }}
    />
  )
}

function Row({
  icon: Icon, iconColor, label, detail, ok, extra,
}: {
  icon: React.ElementType
  iconColor: string
  label: string
  detail?: string
  ok: boolean
  extra?: React.ReactNode
}) {
  return (
    <div
      className="flex items-center gap-3 p-3 rounded-xl"
      style={{
        background: 'rgba(255,255,255,0.025)',
        border: '1px solid rgba(99,102,241,0.09)',
        boxShadow: 'inset 0 1px 0 rgba(255,255,255,0.03)',
      }}
    >
      {/* Icon halo */}
      <div
        className="w-9 h-9 rounded-lg flex items-center justify-center shrink-0"
        style={{
          background: `${iconColor}12`,
          border: `1px solid ${iconColor}30`,
          boxShadow: `0 0 12px -4px ${iconColor}40`,
        }}
      >
        <Icon size={15} style={{ color: iconColor }} />
      </div>

      {/* Text */}
      <div className="flex-1 min-w-0">
        <p className="text-xs font-semibold text-slate-200 truncate">{label}</p>
        {detail && (
          <p className="text-[10px] text-slate-600 mt-0.5 truncate">{detail}</p>
        )}
      </div>

      {/* Status + extra */}
      <div className="flex items-center gap-2 shrink-0">
        {extra}
        <StatusDot ok={ok} pulse={ok} />
      </div>
    </div>
  )
}

export function SystemStatus() {
  const { data, isLoading, isError, refetch, isFetching } = useQuery<SystemInfo>({
    queryKey: ['system'],
    queryFn: () => systemApi.get().then((r) => r.data),
    refetchInterval: 10_000,
  })

  if (isLoading) return <LoadingCard />
  if (isError || !data) return <ErrorCard />

  const { docker, git, runtime } = data

  return (
    <div className="space-y-4">
      {/* 1. Core System Health */}
      <div className="card">
        <Header icon={Server} color="#818cf8" title="System Health" onRefresh={refetch} isFetching={isFetching} />
        <div className="space-y-2.5">
          <Row
            icon={Container}
            iconColor={docker.available ? '#4ade80' : '#f87171'}
            label="Docker"
            detail={docker.available ? docker.host || 'Connected' : 'Not found - direct deploy mode'}
            ok={docker.available}
          />
          <Row
            icon={GitBranch}
            iconColor={git.available ? '#34d399' : '#f87171'}
            label="Git"
            detail={git.version || (git.available ? 'Available' : 'Not found')}
            ok={git.available}
          />
          <Row
            icon={Zap}
            iconColor="#22d3ee"
            label="Runtime"
            detail={`${runtime.os} / ${runtime.arch}${runtime.in_container ? ' * container' : ''}`}
            ok
          />
        </div>
      </div>

      {/* 2. Host Node Details (Separated out) */}
      {data.load && (
        <div className="card shadow-lg" style={{ border: '1px solid rgba(59,130,246,0.15)' }}>
          <Header icon={Activity} color="#3b82f6" title="Host Details" />
          <div className="space-y-3">
            <div className="flex items-center justify-between px-1">
              <span className="text-[11px] font-mono text-slate-300 bg-blue-500/10 px-2 py-0.5 rounded border border-blue-500/20">
                {data.load.hostname}
              </span>
              <span className="text-[10px] text-slate-500 font-mono">{data.load.ip}</span>
            </div>

            {/* CPU Bar */}
            <ProgressBar label="CPU Load" percent={data.load.cpu_percent} color="#60a5fa" />

            {/* RAM Bar */}
            <ProgressBar
              label={`RAM Used (${(data.load.ram_used / 1024 / 1024 / 1024).toFixed(1)}GB)`}
              percent={data.load.ram_percent}
              color="#a78bfa"
            />

            <p className="text-[9px] text-center text-slate-600 font-medium">
              Total Memory: {(data.load.ram_total / 1024 / 1024 / 1024).toFixed(1)}GB
            </p>
          </div>
        </div>
      )}

      {/* 3. Worker Pipeline & Role Loading (Separated out) */}
      <div className="card shadow-xl" style={{ border: '1px solid rgba(168,85,247,0.1)' }}>
        <Header icon={Brain} color="#a855f7" title="Role Loading" />
        <div className="space-y-3">
          {data.workers.tracked && data.workers.total > 0 ? (
            [
              { label: 'Sync', active: data.workers.sync_active, total: data.workers.sync, color: '#fbbf24' },
              { label: 'Build', active: data.workers.build_active, total: data.workers.build, color: '#6366f1' },
              { label: 'Test', active: data.workers.test_active, total: data.workers.test, color: '#10b981' },
              { label: 'AI', active: data.workers.ai_active, total: data.workers.ai, color: '#a855f7' },
              { label: 'Deploy', active: data.workers.deploy_active, total: data.workers.deploy, color: '#f43f5e' },
            ].map(role => role.total > 0 && (
              <ProgressBar
                key={role.label}
                label={`${role.label} Workers`}
                percent={(role.active / role.total) * 100}
                color={role.color}
                subtext={`${role.active}/${role.total} active`}
              />
            ))
          ) : (
            <p className="text-[10px] text-slate-600 text-center py-2">No internal workers tracked</p>
          )}
        </div>
      </div>
    </div>
  )
}

function Header({ icon: Icon, color, title, onRefresh, isFetching }: any) {
  return (
    <div className="flex items-center justify-between mb-4">
      <h2
        className="text-xs font-bold uppercase tracking-wider flex items-center gap-2"
        style={{ color: color }}
      >
        <Icon size={14} />
        {title}
      </h2>
      {onRefresh && (
        <button
          onClick={onRefresh}
          className="p-1 rounded-lg transition-colors text-slate-600 hover:text-slate-300 bg-white/5"
        >
          <RefreshCw size={10} className={isFetching ? 'animate-spin-slow' : ''} />
        </button>
      )}
    </div>
  )
}

function ProgressBar({ label, percent, color, subtext }: { label: string, percent: number, color: string, subtext?: string }) {
  return (
    <div className="space-y-1.5">
      <div className="flex items-center justify-between">
        <span className="text-[10px] text-slate-400 font-medium">{label}</span>
        <span className="text-[10px] text-slate-500 font-mono">{subtext || `${percent.toFixed(0)}%`}</span>
      </div>
      <div className="h-1.5 rounded-full bg-white/5 overflow-hidden">
        <div
          className="h-full transition-all duration-700 rounded-full"
          style={{
            width: `${percent}%`,
            backgroundColor: color,
            boxShadow: `0 0 10px ${color}40`
          }}
        />
      </div>
    </div>
  )
}

function LoadingCard() {
  return (
    <div className="card space-y-3 animate-pulse">
      <div className="h-4 rounded bg-white/5 w-1/3" />
      <div className="h-14 rounded-xl bg-white/[0.02]" />
      <div className="h-14 rounded-xl bg-white/[0.02]" />
    </div>
  )
}

function ErrorCard() {
  return (
    <div className="card">
      <p className="text-sm text-red-400">Failed to load system status</p>
    </div>
  )
}
