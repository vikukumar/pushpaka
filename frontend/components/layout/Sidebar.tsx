'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import {
  LayoutDashboard,
  FolderGit2,
  Rocket,
  Globe,
  Settings,
  Activity,
  LogOut,
  Shield,
  X,
  Sparkles,
  BotMessageSquare,
  Database,
  AlertTriangle,
  Container,
  Server,
  ChevronDown,
  ChevronRight,
  FileCode,
  Files as FilesIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/lib/auth'
import { useRouter } from 'next/navigation'
import { useSidebar } from '@/components/providers/AuthProvider'
import { useState } from 'react'

interface NavItem {
  href: string
  label: string
  icon: React.ElementType
}

interface NavGroup {
  label: string
  icon: React.ElementType
  items: NavItem[]
}

const mainNavItems: NavItem[] = [
  { href: '/dashboard',             label: 'Overview',     icon: LayoutDashboard },
  { href: '/dashboard/projects',    label: 'Projects',     icon: FolderGit2 },
  { href: '/dashboard/deployments', label: 'Deployments',  icon: Rocket },
  { href: '/dashboard/domains',     label: 'Domains',      icon: Globe },
  { href: '/dashboard/activity',    label: 'Activity',     icon: Activity },
  { href: '/dashboard/editor',      label: 'Code',         icon: FileCode },
  { href: '/dashboard/files',       label: 'Files',        icon: FilesIcon },
  { href: '/dashboard/audit',       label: 'Audit Log',    icon: Shield },
]

const navGroups: NavGroup[] = [
  {
    label: 'AI Assistant',
    icon: Sparkles,
    items: [
      { href: '/dashboard/ai/chat',       label: 'Support Agent',  icon: BotMessageSquare },
      { href: '/dashboard/ai/monitoring', label: 'AI Monitoring',  icon: AlertTriangle },
      { href: '/dashboard/ai/rag',        label: 'Knowledge Base', icon: Database },
    ],
  },
  {
    label: 'Infrastructure',
    icon: Server,
    items: [
      { href: '/dashboard/infra/workers', label: 'Workers',        icon: Server },
      { href: '/dashboard/infra/docker',  label: 'Docker',         icon: Container },
      { href: '/dashboard/infra/k8s',     label: 'Kubernetes',     icon: Server },
    ],
  },
]

function NavGroupSection({ group, close }: { group: NavGroup; close: () => void }) {
  const pathname = usePathname()
  const isAnyActive = group.items.some(
    (i) => pathname === i.href || pathname.startsWith(i.href + '/')
  )
  const [open, setOpen] = useState(isAnyActive)

  return (
    <div>
      <button
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-2.5 w-full px-3 py-2 text-xs font-semibold uppercase tracking-widest text-slate-600 hover:text-slate-400 transition-colors"
      >
        <group.icon size={11} />
        <span className="flex-1 text-left">{group.label}</span>
        {open ? <ChevronDown size={11} /> : <ChevronRight size={11} />}
      </button>
      {open && (
        <div className="pl-1">
          {group.items.map(({ href, label, icon: Icon }) => {
            const isActive = pathname === href || pathname.startsWith(href + '/')
            return (
              <Link
                key={href}
                href={href}
                onClick={close}
                className={cn(
                  'relative flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium',
                  'transition-all duration-200 group overflow-hidden',
                  isActive ? 'text-[var(--text-primary)]' : 'text-slate-500 hover:text-slate-300'
                )}
                style={
                  isActive
                    ? {
                        background: 'linear-gradient(90deg, var(--brand-glow) 0%, transparent 100%)',
                        boxShadow: 'inset 3px 0 0 var(--brand-primary), inset 0 1px 0 var(--border-subtle)',
                      }
                    : undefined
                }
              >
                {!isActive && (
                  <span className="absolute inset-0 opacity-0 group-hover:opacity-100 rounded-lg transition-opacity duration-200"
                    style={{ background: 'rgba(255,255,255,0.025)' }} />
                )}
                <Icon
                  size={15}
                  className={cn(
                    'shrink-0 transition-colors',
                    isActive ? 'text-brand-400' : 'text-slate-600 group-hover:text-slate-400'
                  )}
                  style={isActive ? { filter: 'drop-shadow(0 0 5px rgba(129,140,248,0.75))' } : undefined}
                />
                <span className="truncate flex-1">{label}</span>
                {isActive && (
                  <span className="w-1.5 h-1.5 rounded-full bg-brand-400 shrink-0"
                    style={{ boxShadow: '0 0 8px rgba(129,140,248,0.9)' }} />
                )}
              </Link>
            )
          })}
        </div>
      )}
    </div>
  )
}

export function Sidebar() {
  const pathname = usePathname()
  const { user, clearAuth } = useAuthStore()
  const router = useRouter()
  const { open, close } = useSidebar()

  const handleLogout = () => {
    clearAuth()
    router.push('/login')
  }

  return (
    <aside
      className={cn(
        'w-64 h-screen flex flex-col z-50 overflow-hidden transition-all duration-300',
        'md:fixed md:left-0 md:top-0 md:translate-x-0 sidebar-glass',
        'fixed left-0 top-0',
        open ? 'translate-x-0' : '-translate-x-full md:translate-x-0',
      )}
    >
      {/* Ambient glow */}
      <div
        className="absolute -top-16 -left-16 w-56 h-56 rounded-full pointer-events-none"
        style={{ background: 'radial-gradient(circle, rgba(99,102,241,0.09) 0%, transparent 70%)' }}
      />

      {/* Logo */}
      <div
        className="px-5 py-4 relative shrink-0 flex items-center justify-between"
        style={{ borderBottom: '1px solid var(--border-color)' }}
      >
        <Link href="/dashboard" onClick={close} className="flex items-center gap-3 group">
          <div
            className="w-9 h-9 rounded-xl flex items-center justify-center shrink-0"
            style={{
              background: 'linear-gradient(135deg, #3730a3 0%, #4f46e5 55%, #7c3aed 100%)',
              boxShadow: '0 4px 16px rgba(99,102,241,0.45), inset 0 1px 0 rgba(255,255,255,0.2)',
            }}
          >
            <svg viewBox="0 0 32 32" className="w-5 h-5" fill="none">
              <rect x="3" y="15" width="26" height="10" rx="5" fill="white" fillOpacity="0.95"/>
              <circle cx="8"  cy="15" r="4" fill="white" fillOpacity="0.95"/>
              <circle cx="16" cy="12" r="5" fill="white" fillOpacity="0.95"/>
              <circle cx="24" cy="14" r="4" fill="white" fillOpacity="0.95"/>
              <path d="M3 18 Q0 16 1 20 Q0 23 3 22"   fill="#22d3ee"/>
              <path d="M29 18 Q32 16 31 20 Q32 23 29 22" fill="#22d3ee"/>
              <line x1="16" y1="4" x2="16" y2="9" stroke="rgba(255,255,255,0.6)" strokeWidth="1.5" strokeLinecap="round"/>
              <circle cx="16" cy="3.5" r="1.5" fill="#22d3ee"/>
            </svg>
          </div>
          <div className="min-w-0">
            <div
              className="font-bold tracking-tight text-[15px] leading-none"
              style={{
                background: 'var(--header-title)',
                WebkitBackgroundClip: 'text',
                WebkitTextFillColor: 'transparent',
                backgroundClip: 'text',
              }}
            >
              Pushpaka
            </div>
            <div className="text-[9px] text-slate-600 mt-1 tracking-[0.18em] uppercase font-semibold">
              Platform
            </div>
          </div>
        </Link>
        <button onClick={close} className="md:hidden p-1.5 rounded-lg text-slate-500 hover:text-[var(--text-primary)] hover:bg-[var(--border-subtle)] transition-colors">
          <X size={16} />
        </button>
      </div>

      {/* Navigation */}
      <nav className="flex-1 p-3 space-y-0.5 overflow-y-auto">
        {/* Main nav */}
        {mainNavItems.map(({ href, label, icon: Icon }) => {
          const isActive =
            href === '/dashboard'
              ? pathname === href
              : pathname === href || pathname.startsWith(href + '/')
          return (
            <Link
              key={href}
              href={href}
              onClick={close}
              className={cn(
                'relative flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium',
                'transition-all duration-200 group overflow-hidden',
                isActive ? 'text-white' : 'text-slate-500 hover:text-slate-300'
              )}
              style={
                isActive
                  ? {
                      background: 'linear-gradient(90deg, rgba(99,102,241,0.22) 0%, rgba(99,102,241,0.07) 55%, transparent 100%)',
                      boxShadow: 'inset 3px 0 0 #818cf8, inset 0 1px 0 rgba(255,255,255,0.04)',
                    }
                  : undefined
              }
            >
              {!isActive && (
                <span className="absolute inset-0 opacity-0 group-hover:opacity-100 rounded-lg transition-opacity duration-200"
                  style={{ background: 'rgba(255,255,255,0.025)' }} />
              )}
              <Icon
                size={15}
                className={cn(
                  'shrink-0 transition-colors',
                  isActive ? 'text-brand-400' : 'text-slate-600 group-hover:text-slate-400'
                )}
                style={isActive ? { filter: 'drop-shadow(0 0 5px rgba(129,140,248,0.75))' } : undefined}
              />
              <span className="truncate flex-1">{label}</span>
              {isActive && (
                <span className="w-1.5 h-1.5 rounded-full bg-brand-400 shrink-0"
                  style={{ boxShadow: '0 0 8px rgba(129,140,248,0.9)' }} />
              )}
            </Link>
          )
        })}

        {/* Divider */}
        <div className="my-2 border-t border-[var(--border-subtle)]" />

        {/* Groups */}
        {navGroups.map((group) => (
          <NavGroupSection key={group.label} group={group} close={close} />
        ))}

        {/* Admin section — visible to admin users only */}
        {user?.role === 'admin' && (
          <>
            <div className="my-2 border-t border-[var(--border-subtle)]" />
            <div className="px-3 py-1 text-[9px] font-semibold uppercase tracking-widest text-indigo-500/70">Admin</div>
            {[{ href: '/dashboard/admin/users', label: 'User Management', icon: Shield }].map(({ href, label, icon: Icon }) => {
              const isActive = pathname === href || pathname.startsWith(href + '/')
              return (
                <Link
                  key={href}
                  href={href}
                  onClick={close}
                  className={cn(
                    'relative flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium',
                    'transition-all duration-200 group overflow-hidden',
                    isActive ? 'text-white' : 'text-indigo-400/70 hover:text-indigo-300'
                  )}
                  style={
                    isActive
                      ? {
                          background: 'linear-gradient(90deg, rgba(99,102,241,0.22) 0%, rgba(99,102,241,0.07) 55%, transparent 100%)',
                          boxShadow: 'inset 3px 0 0 #818cf8',
                        }
                      : undefined
                  }
                >
                  <Icon size={15} className={cn('shrink-0', isActive ? 'text-indigo-400' : 'text-indigo-500/50 group-hover:text-indigo-400')} />
                  <span className="truncate flex-1">{label}</span>
                </Link>
              )
            })}
          </>
        )}

        {/* Divider */}
        <div className="my-2 border-t border-[var(--border-subtle)]" />

        {/* Settings */}
        {(() => {
          const href = '/dashboard/settings'
          const isActive = pathname === href || pathname.startsWith(href + '/')
          return (
            <Link
              href={href}
              onClick={close}
              className={cn(
                'relative flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium',
                'transition-all duration-200 group overflow-hidden',
                isActive ? 'text-white' : 'text-slate-500 hover:text-slate-300'
              )}
              style={
                isActive
                  ? {
                      background: 'linear-gradient(90deg, rgba(99,102,241,0.22) 0%, rgba(99,102,241,0.07) 55%, transparent 100%)',
                      boxShadow: 'inset 3px 0 0 #818cf8, inset 0 1px 0 rgba(255,255,255,0.04)',
                    }
                  : undefined
              }
            >
              {!isActive && (
                <span className="absolute inset-0 opacity-0 group-hover:opacity-100 rounded-lg transition-opacity"
                  style={{ background: 'rgba(255,255,255,0.025)' }} />
              )}
              <Settings
                size={15}
                className={cn('shrink-0 transition-colors', isActive ? 'text-brand-400' : 'text-slate-600 group-hover:text-slate-400')}
                style={isActive ? { filter: 'drop-shadow(0 0 5px rgba(129,140,248,0.75))' } : undefined}
              />
              <span className="truncate flex-1">Settings</span>
              {isActive && (
                <span className="w-1.5 h-1.5 rounded-full bg-brand-400 shrink-0"
                  style={{ boxShadow: '0 0 8px rgba(129,140,248,0.9)' }} />
              )}
            </Link>
          )
        })()}
      </nav>

      {/* User section */}
      <div className="p-3 relative shrink-0 border-t border-[var(--border-subtle)]">
        <div className="flex items-center gap-3 px-3 py-2.5 rounded-lg border border-[var(--border-subtle)] bg-[var(--bg-elevated)]">
          {/* Avatar */}
          {user?.avatar_url ? (
            <img src={user.avatar_url} alt="" className="w-8 h-8 rounded-lg object-cover shrink-0" />
          ) : (
            <div
              className="w-8 h-8 rounded-lg flex items-center justify-center text-white text-xs font-bold shrink-0"
              style={{ background: 'linear-gradient(135deg, #4338ca, #6366f1)', boxShadow: '0 2px 8px rgba(99,102,241,0.4)' }}
            >
              {user?.name?.[0]?.toUpperCase() || 'U'}
            </div>
          )}
          <div className="flex-1 min-w-0">
            <div className="text-sm font-medium text-[var(--text-primary)] truncate">{user?.name}</div>
            <div className="flex items-center gap-1.5 mt-0.5">
              {user?.role === 'admin' && (
                <span className="text-[9px] px-1.5 py-0.5 rounded-full bg-indigo-500/15 text-indigo-400 border border-indigo-500/20 font-semibold flex items-center gap-0.5">
                  <Shield size={8} /> admin
                </span>
              )}
              <div className="text-[11px] text-slate-500 truncate">{user?.email}</div>
            </div>
          </div>
          <button onClick={handleLogout} className="text-slate-600 hover:text-red-400 transition-colors p-1 rounded shrink-0" title="Sign out">
            <LogOut size={14} />
          </button>
        </div>
      </div>
    </aside>
  )
}
