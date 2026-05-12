'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import Link from 'next/link'
import { projectsApi } from '@/lib/api'
import { Project } from '@/types'
import { Header } from '@/components/layout/Header'
import { FolderGit2, Plus, Search, Server, X, Loader2 } from 'lucide-react'

// ─── Create Namespace Modal ────────────────────────────────────────────────────
function CreateNamespaceModal({ onClose }: { onClose: () => void }) {
  const [name, setName] = useState('')
  const [error, setError] = useState('')
  const queryClient = useQueryClient()

  const { mutate, isPending } = useMutation({
    mutationFn: () =>
      projectsApi.create({
        name,
        repo_url: '',      // registry namespaces don't need a repo
        // @ts-expect-error type field is valid on the backend
        type: 'registry',
      }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['projects', 'registry'] })
      onClose()
    },
    onError: (err: any) => {
      setError(err?.response?.data?.error || err?.message || 'Failed to create namespace')
    },
  })

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    if (!name.trim()) { setError('Name is required'); return }
    if (!/^[a-z0-9][a-z0-9._-]*$/.test(name)) {
      setError('Name must start with a letter or digit and contain only lowercase letters, digits, dots, hyphens or underscores')
      return
    }
    mutate()
  }

  return (
    /* backdrop */
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={(e) => { if (e.target === e.currentTarget) onClose() }}
    >
      <div className="relative w-full max-w-md mx-4 bg-slate-900 border border-slate-700 rounded-xl shadow-2xl">
        {/* header */}
        <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-indigo-500/10 text-indigo-400">
              <FolderGit2 size={18} />
            </div>
            <h2 className="text-base font-semibold text-white">New Registry Namespace</h2>
          </div>
          <button
            onClick={onClose}
            className="text-slate-400 hover:text-white transition-colors p-1 rounded"
          >
            <X size={18} />
          </button>
        </div>

        {/* body */}
        <form onSubmit={handleSubmit} className="px-6 py-5 space-y-4">
          <div>
            <label className="block text-sm font-medium text-slate-300 mb-1.5">
              Namespace name <span className="text-red-400">*</span>
            </label>
            <input
              type="text"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value.toLowerCase())}
              placeholder="e.g. my-project"
              className="input w-full"
            />
            <p className="text-xs text-slate-500 mt-1.5">
              Lowercase letters, digits, dots, hyphens and underscores only.
            </p>
          </div>

          {/* docker push preview */}
          {name && (
            <div className="p-3 bg-slate-950 rounded border border-slate-800 font-mono text-xs text-slate-400">
              docker push your-domain.com/v2/
              <span className="text-indigo-400">{name}</span>
              /image:tag
            </div>
          )}

          {error && (
            <p className="text-sm text-red-400 bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2">
              {error}
            </p>
          )}

          <div className="flex justify-end gap-3 pt-1">
            <button type="button" onClick={onClose} className="btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={isPending} className="btn-primary">
              {isPending ? (
                <><Loader2 size={14} className="animate-spin" /> Creating…</>
              ) : (
                <><Plus size={14} /> Create Namespace</>
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  )
}

// ─── Page ─────────────────────────────────────────────────────────────────────
export default function NamespacesPage() {
  const [search, setSearch] = useState('')
  const [showModal, setShowModal] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['projects', 'registry'],
    queryFn: () => projectsApi.list('registry').then((r) => r.data),
  })

  const namespaces: Project[] = data?.data || []
  const filtered = namespaces.filter((p) => p.name.toLowerCase().includes(search.toLowerCase()))

  return (
    <div className="flex flex-col min-h-screen">
      {showModal && <CreateNamespaceModal onClose={() => setShowModal(false)} />}

      <Header title="Registry Namespaces" subtitle="Isolated environments for your Docker images" />

      <div className="p-6 space-y-5">
        <div className="flex items-center gap-3">
          <div className="relative flex-1 max-w-sm">
            <Search size={14} className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" />
            <input
              type="text"
              placeholder="Search namespaces..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="input pl-8"
            />
          </div>
          <button onClick={() => setShowModal(true)} className="btn-primary ml-auto">
            <Plus size={15} />
            New Namespace
          </button>
        </div>

        {isLoading ? (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {[...Array(3)].map((_, i) => (
              <div key={i} className="card animate-pulse">
                <div className="h-4 bg-slate-700 rounded w-2/3 mb-3" />
                <div className="h-3 bg-slate-800 rounded w-full mb-4" />
                <div className="h-8 bg-slate-800 rounded" />
              </div>
            ))}
          </div>
        ) : filtered.length === 0 ? (
          <div className="card text-center py-16">
            <FolderGit2 size={48} className="mx-auto text-slate-700 mb-4" />
            <h3 className="text-white font-semibold mb-2">No namespaces found</h3>
            <p className="text-slate-400 text-sm mb-5">
              Create a namespace to start pushing and pulling Docker images.
            </p>
            <button onClick={() => setShowModal(true)} className="btn-primary inline-flex">
              <Plus size={15} />
              Create Namespace
            </button>
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
            {filtered.map((ns) => (
              <div key={ns.id} className="card hover:border-slate-600 transition-colors">
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className="p-2 bg-indigo-500/10 rounded-lg text-indigo-400">
                      <Server size={20} />
                    </div>
                    <div>
                      <h3 className="text-sm font-medium text-white">{ns.name}</h3>
                      <p className="text-xs text-slate-400">ID: {ns.id}</p>
                    </div>
                  </div>
                </div>

                <div className="p-3 bg-slate-900 rounded border border-slate-800 font-mono text-xs text-slate-300 break-all">
                  docker push your-domain.com/v2/{ns.name}/image:tag
                </div>

                <div className="mt-4 flex justify-end">
                  <Link
                    href={`/dashboard/registry/repos?project_id=${ns.id}`}
                    className="btn-secondary text-xs py-1.5"
                  >
                    View Repositories
                  </Link>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
