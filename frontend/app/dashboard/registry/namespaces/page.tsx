'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import Link from 'next/link'
import { projectsApi } from '@/lib/api'
import { Project } from '@/types'
import { Header } from '@/components/layout/Header'
import { FolderGit2, Plus, Search, Server } from 'lucide-react'

export default function NamespacesPage() {
  const [search, setSearch] = useState('')

  const { data, isLoading } = useQuery({
    queryKey: ['projects', 'registry'],
    queryFn: () => projectsApi.list('registry').then((r) => r.data),
  })

  const namespaces: Project[] = data?.data || []
  const filtered = namespaces.filter((p) => p.name.toLowerCase().includes(search.toLowerCase()))

  return (
    <div className="flex flex-col min-h-screen">
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
          <Link href="/dashboard/projects/new?type=registry" className="btn-primary ml-auto">
            <Plus size={15} />
            New Namespace
          </Link>
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
            <Link href="/dashboard/projects/new?type=registry" className="btn-primary inline-flex">
              <Plus size={15} />
              Create Namespace
            </Link>
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
                
                <div className="p-3 bg-slate-900 rounded border border-slate-800 font-mono text-xs text-slate-300">
                  docker push your-domain.com/v2/{ns.name}/image:tag
                </div>
                
                <div className="mt-4 flex justify-end">
                   <Link href={`/dashboard/registry/repos?project_id=${ns.id}`} className="btn-secondary text-xs py-1.5">
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
