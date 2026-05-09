import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { registryApi } from '@/lib/api'
import { RegistryRepo } from '@/types'
import { 
  Package, Trash2, ExternalLink, RefreshCw, Plus, 
  Settings, Loader2, Info, Lock, Globe, Shield, Archive, Database
} from 'lucide-react'
import toast from 'react-hot-toast'
import { timeAgo } from '@/lib/utils'
import Link from 'next/link'

interface RegistryRepoListProps {
  projectId: string
}

export function RegistryRepoList({ projectId }: RegistryRepoListProps) {
  const queryClient = useQueryClient()
  const [isCreating, setIsCreating] = useState(false)
  const [newRepo, setNewRepo] = useState({
    name: '',
    type: 'docker' as 'docker' | 'helm' | 'binary',
    description: '',
    is_public: false
  })

  const { data: repos, isLoading } = useQuery({
    queryKey: ['registry', 'repos', projectId],
    queryFn: () => registryApi.listRepos(projectId).then((r: any) => r.data as RegistryRepo[])
  })

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    const loading = toast.loading('Creating repository...')
    try {
      await registryApi.createRepo({
        project_id: projectId,
        ...newRepo
      })
      toast.dismiss(loading)
      toast.success('Repository created!')
      setIsCreating(false)
      setNewRepo({ name: '', type: 'docker', description: '', is_public: false })
      queryClient.invalidateQueries({ queryKey: ['registry', 'repos', projectId] })
    } catch (err: any) {
      toast.dismiss(loading)
      toast.error(err.response?.data?.error || 'Failed to create repository')
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('Are you sure you want to delete this repository and all its artifacts?')) return
    try {
      await registryApi.deleteRepo(id)
      toast.success('Repository deleted')
      queryClient.invalidateQueries({ queryKey: ['registry', 'repos', projectId] })
    } catch {
      toast.error('Failed to delete repository')
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="animate-spin text-brand-400" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-white flex items-center gap-2">
          <Database size={18} className="text-brand-400" />
          Repositories
        </h2>
        <button 
          onClick={() => setIsCreating(true)}
          className="btn-primary"
        >
          <Plus size={16} />
          Add Repository
        </button>
      </div>

      {isCreating && (
        <div className="card border-brand-500/30 bg-brand-500/5 animate-in fade-in slide-in-from-top-4 duration-300">
          <form onSubmit={handleCreate} className="space-y-4">
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-400">Repository Name</label>
                <input 
                  autoFocus
                  required
                  placeholder="my-awesome-app"
                  className="input-base"
                  value={newRepo.name}
                  onChange={e => setNewRepo({ ...newRepo, name: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-slate-400">Type</label>
                <select 
                  className="input-base"
                  value={newRepo.type}
                  onChange={e => setNewRepo({ ...newRepo, type: e.target.value as any })}
                >
                  <option value="docker">Docker Container</option>
                  <option value="helm">Helm Chart</option>
                  <option value="binary">Generic Binary</option>
                </select>
              </div>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-slate-400">Description</label>
              <textarea 
                placeholder="Repository for production builds..."
                className="input-base min-h-[80px]"
                value={newRepo.description}
                onChange={e => setNewRepo({ ...newRepo, description: e.target.value })}
              />
            </div>
            <div className="flex items-center gap-2">
              <input 
                type="checkbox"
                id="is_public"
                checked={newRepo.is_public}
                onChange={e => setNewRepo({ ...newRepo, is_public: e.target.checked })}
                className="w-4 h-4 rounded border-slate-700 bg-slate-800 text-brand-500 focus:ring-brand-500"
              />
              <label htmlFor="is_public" className="text-sm text-slate-300">Public Access (anonymous pull allowed)</label>
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <button type="button" onClick={() => setIsCreating(false)} className="btn-secondary">Cancel</button>
              <button type="submit" className="btn-primary">Create Repository</button>
            </div>
          </form>
        </div>
      )}

      <div className="grid grid-cols-1 gap-3">
        {repos?.length === 0 ? (
          <div className="card py-12 text-center">
            <Archive size={48} className="mx-auto text-slate-700 mb-4" />
            <h3 className="text-slate-300 font-medium mb-1">No repositories found</h3>
            <p className="text-slate-500 text-sm max-w-xs mx-auto">
              Start by adding your first repository to this registry project.
            </p>
          </div>
        ) : (
          repos?.map((repo: RegistryRepo) => (
            <div key={repo.id} className="group card hover:border-brand-500/50 transition-all">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-4">
                  <div className={`p-3 rounded-xl ${
                    repo.type === 'docker' ? 'bg-blue-500/10 text-blue-400' :
                    repo.type === 'helm' ? 'bg-indigo-500/10 text-indigo-400' :
                    'bg-emerald-500/10 text-emerald-400'
                  }`}>
                    {repo.type === 'docker' ? <Database size={20} /> : 
                     repo.type === 'helm' ? <Shield size={20} /> : <Package size={20} />}
                  </div>
                  <div>
                    <div className="flex items-center gap-2">
                      <h3 className="font-semibold text-slate-200">{repo.name}</h3>
                      <span className="text-[10px] px-1.5 py-0.5 rounded bg-slate-800 text-slate-500 uppercase font-bold border border-white/5">
                        {repo.type}
                      </span>
                      {repo.is_public ? (
                        <Globe size={12} className="text-emerald-500" />
                      ) : (
                        <Lock size={12} className="text-slate-600" />
                      )}
                    </div>
                    <p className="text-xs text-slate-500 mt-0.5 line-clamp-1">{repo.description || 'No description provided'}</p>
                  </div>
                </div>
                
                <div className="flex items-center gap-2 opacity-0 group-hover:opacity-100 transition-opacity">
                  <button 
                    onClick={() => handleDelete(repo.id)}
                    className="p-2 text-slate-500 hover:text-red-400 hover:bg-red-500/10 rounded-lg transition-colors"
                  >
                    <Trash2 size={16} />
                  </button>
                  <Link 
                    href={`/dashboard/registry/repos/${repo.id}`}
                    className="p-2 text-slate-500 hover:text-brand-400 hover:bg-brand-500/10 rounded-lg transition-colors"
                  >
                    <ExternalLink size={16} />
                  </Link>
                </div>
              </div>
              
              <div className="flex items-center gap-6 mt-4 pt-4 border-t border-white/5 text-[11px]">
                <div className="flex items-center gap-1.5 text-slate-400">
                  <Package size={12} className="text-brand-400" />
                  <span className="font-medium text-slate-200">{repo.artifact_count}</span> Artifacts
                </div>
                <div className="flex items-center gap-1.5 text-slate-400">
                  <RefreshCw size={12} className="text-brand-400" />
                  <span className="font-medium text-slate-200">{repo.download_count}</span> Pulls
                </div>
                <div className="flex items-center gap-1.5 text-slate-400">
                  <Settings size={12} className="text-brand-400" />
                  Created {timeAgo(repo.created_at)}
                </div>
              </div>
            </div>
          ))
        )}
      </div>
      
      <div className="card bg-slate-900/50 border-slate-800/50">
        <div className="flex items-start gap-3">
          <Info size={16} className="text-brand-400 mt-0.5 shrink-0" />
          <div className="text-xs text-slate-500 leading-relaxed">
            <p className="font-medium text-slate-400 mb-1">Pushpaka Registry Access</p>
            You can push images to this registry using: <code className="text-brand-400">docker push {typeof window !== 'undefined' ? window.location.host : 'registry.pushpaka.dev'}/registry/oci/{projectId}/[repo_name]</code>. 
            Authentication is required unless the repository is marked as public. Use your Pushpaka credentials or a Service Token.
          </div>
        </div>
      </div>
    </div>
  )
}
