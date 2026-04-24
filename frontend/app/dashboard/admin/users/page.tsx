'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/lib/api'
import { useAuthStore } from '@/lib/auth'
import { Header } from '@/components/layout/Header'
import { SafeUser, UpdateUserRoleRequest, UsersListResponse } from '@/types'
import toast from 'react-hot-toast'
import { Shield, UserCheck, UserX, ChevronDown } from 'lucide-react'

const ROLES = ['admin', 'user', 'viewer'] as const
type Role = (typeof ROLES)[number]

const ROLE_COLORS: Record<Role, string> = {
  admin:  'text-indigo-400 bg-indigo-400/10 border-indigo-400/20',
  user:   'text-blue-400 bg-blue-400/10 border-blue-400/20',
  viewer: 'text-slate-400 bg-slate-400/10 border-slate-400/20',
}

function RoleBadge({ role }: { role: string }) {
  const color = ROLE_COLORS[role as Role] || ROLE_COLORS.viewer
  return (
    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-semibold border ${color}`}>
      {role === 'admin' && <Shield size={10} />}
      {role}
    </span>
  )
}

export default function AdminUsersPage() {
  const { user: currentUser } = useAuthStore()
  const qc = useQueryClient()
  const [page, setPage] = useState(0)
  const limit = 20

  const { data, isLoading } = useQuery({
    queryKey: ['admin-users', page],
    queryFn: () =>
      apiClient.get<UsersListResponse>(`/admin/users?limit=${limit}&offset=${page * limit}`)
        .then(r => r.data),
  })

  const updateRole = useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateUserRoleRequest }) =>
      apiClient.put(`/admin/users/${id}/role`, body),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['admin-users'] })
      toast.success('User updated')
    },
    onError: () => toast.error('Failed to update user'),
  })

  const users: SafeUser[] = data?.data || []
  const total = data?.total || 0

  if (currentUser?.role !== 'admin') {
    return (
      <div className="flex flex-col min-h-screen">
        <Header title="Admin" subtitle="User Management" />
        <div className="flex-1 flex items-center justify-center">
          <div className="text-center">
            <Shield size={48} className="mx-auto text-slate-700 mb-4" />
            <h2 className="text-xl font-bold text-slate-300 mb-2">Access Denied</h2>
            <p className="text-slate-500 text-sm">You need admin role to access this page.</p>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className="flex flex-col min-h-screen animate-fade-in">
      <Header title="User Management" subtitle={`${total} registered users`} />

      <div className="p-6 space-y-4">
        <div className="card overflow-hidden p-0">
          {/* Table header */}
          <div className="grid grid-cols-[2fr_1fr_1fr_1fr_auto] items-center gap-4 px-6 py-3 border-b border-[var(--border-subtle)] text-[11px] font-semibold uppercase tracking-wider text-slate-500">
            <span>User</span>
            <span>Role</span>
            <span>Status</span>
            <span>Provider</span>
            <span>Actions</span>
          </div>

          {isLoading ? (
            <div className="py-12 text-center text-slate-500 text-sm">Loading users…</div>
          ) : users.length === 0 ? (
            <div className="py-12 text-center text-slate-500 text-sm">No users found.</div>
          ) : (
            <div className="divide-y divide-[var(--border-subtle)]">
              {users.map(u => (
                <div
                  key={u.id}
                  className="grid grid-cols-[2fr_1fr_1fr_1fr_auto] items-center gap-4 px-6 py-4 hover:bg-[var(--brand-glow)] transition-colors"
                >
                  {/* User info */}
                  <div className="flex items-center gap-3 min-w-0">
                    <div className="w-9 h-9 rounded-full bg-indigo-600/20 border border-indigo-500/30 flex items-center justify-center text-sm font-bold text-indigo-300 shrink-0">
                      {u.avatar_url ? (
                        <img src={u.avatar_url} alt="" className="w-9 h-9 rounded-full object-cover" />
                      ) : (
                        u.name.charAt(0).toUpperCase()
                      )}
                    </div>
                    <div className="min-w-0">
                      <div className="text-sm font-medium text-slate-200 truncate">
                        {u.name}
                        {u.id === currentUser?.id && (
                          <span className="ml-2 text-[10px] text-slate-500">(you)</span>
                        )}
                      </div>
                      <div className="text-xs text-slate-500 truncate">{u.email}</div>
                    </div>
                  </div>

                  {/* Role */}
                  <div>
                    <RoleBadge role={u.role} />
                  </div>

                  {/* Status */}
                  <div>
                    <span className={`inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium border ${u.is_active ? 'text-green-400 bg-green-400/10 border-green-400/20' : 'text-red-400 bg-red-400/10 border-red-400/20'}`}>
                      {u.is_active ? <UserCheck size={10} /> : <UserX size={10} />}
                      {u.is_active ? 'Active' : 'Disabled'}
                    </span>
                  </div>

                  {/* OAuth Provider */}
                  <div className="text-xs text-slate-500 capitalize">
                    {u.oauth_provider || '—'}
                  </div>

                  {/* Actions */}
                  <div className="flex items-center gap-2">
                    {u.id !== currentUser?.id && (
                      <>
                        <div className="relative group">
                          <button className="flex items-center gap-1 px-3 py-1.5 rounded-lg text-xs font-medium bg-[var(--bg-elevated)] border border-[var(--border-color)] hover:border-indigo-500/40 text-slate-300 transition-all">
                            Role <ChevronDown size={12} />
                          </button>
                          <div className="absolute right-0 top-full mt-1 bg-[var(--bg-elevated)] border border-[var(--border-color)] rounded-lg shadow-xl z-10 opacity-0 group-hover:opacity-100 pointer-events-none group-hover:pointer-events-auto transition-opacity min-w-[100px]">
                            {ROLES.map(r => (
                              <button
                                key={r}
                                onClick={() => updateRole.mutate({ id: u.id, body: { role: r } })}
                                className={`block w-full text-left px-4 py-2 text-xs hover:bg-indigo-600/10 transition-colors ${u.role === r ? 'text-indigo-400 font-semibold' : 'text-slate-300'}`}
                              >
                                {r}
                              </button>
                            ))}
                          </div>
                        </div>
                        <button
                          onClick={() => {
                            const newActive = !u.is_active
                            updateRole.mutate({ id: u.id, body: { role: u.role as Role, is_active: newActive } })
                          }}
                          className={`px-3 py-1.5 rounded-lg text-xs font-medium border transition-all ${
                            u.is_active
                              ? 'text-red-400 border-red-400/20 hover:bg-red-400/10'
                              : 'text-green-400 border-green-400/20 hover:bg-green-400/10'
                          }`}
                        >
                          {u.is_active ? 'Disable' : 'Enable'}
                        </button>
                      </>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Pagination */}
        {total > limit && (
          <div className="flex items-center justify-between text-sm text-slate-500">
            <span>
              Showing {page * limit + 1}–{Math.min((page + 1) * limit, total)} of {total}
            </span>
            <div className="flex gap-2">
              <button
                onClick={() => setPage(p => Math.max(0, p - 1))}
                disabled={page === 0}
                className="btn-secondary text-xs px-3 py-1.5 disabled:opacity-40"
              >
                Previous
              </button>
              <button
                onClick={() => setPage(p => p + 1)}
                disabled={(page + 1) * limit >= total}
                className="btn-secondary text-xs px-3 py-1.5 disabled:opacity-40"
              >
                Next
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
