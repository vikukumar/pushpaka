'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { registryReposApi, projectsApi } from '@/lib/api'
import { Header } from '@/components/layout/Header'
import { Container, Package2, ArrowDownToLine, Database } from 'lucide-react'

export default function ReposPage({ searchParams }: { searchParams: { project_id?: string } }) {
  const projectId = searchParams.project_id || ''
  const [selectedRepo, setSelectedRepo] = useState<string | null>(null)

  const { data: namespacesData } = useQuery({
    queryKey: ['projects', 'registry'],
    queryFn: () => projectsApi.list('registry').then((r) => r.data),
  })

  // We should ideally fetch all repos across all namespaces if no projectId is specified, 
  // but for now, if projectId is missing, we pick the first one available or show a message.
  const activeProjectId = projectId || (namespacesData?.data?.[0]?.id)

  const { data: reposData, isLoading: loadingRepos } = useQuery({
    queryKey: ['registry', 'repos', activeProjectId],
    queryFn: () => registryReposApi.listRepos(activeProjectId).then((r) => r.data),
    enabled: !!activeProjectId,
  })

  const repos = reposData || []
  const activeNamespace = namespacesData?.data?.find((n: any) => n.id === activeProjectId)

  return (
    <div className="flex flex-col min-h-screen">
      <Header title="Docker Repositories" subtitle="Manage your published images and artifacts" />

      <div className="p-6 flex gap-6 h-[calc(100vh-80px)]">
        {/* Left Sidebar: Repositories List */}
        <div className="w-1/3 bg-slate-900 border border-slate-800 rounded-xl overflow-hidden flex flex-col">
          <div className="p-4 border-b border-slate-800 bg-slate-800/30">
            <h3 className="font-semibold text-white flex items-center gap-2">
              <Container size={16} className="text-indigo-400" />
              Repositories
            </h3>
            {activeNamespace && (
              <p className="text-xs text-slate-400 mt-1">Namespace: {activeNamespace.name}</p>
            )}
          </div>
          
          <div className="flex-1 overflow-y-auto">
            {!activeProjectId && (
              <div className="p-6 text-center text-slate-400 text-sm">
                Select a namespace to view repositories.
              </div>
            )}
            
            {loadingRepos && activeProjectId && (
              <div className="p-4 space-y-3">
                <div className="h-10 bg-slate-800 rounded animate-pulse" />
                <div className="h-10 bg-slate-800 rounded animate-pulse" />
              </div>
            )}

            {repos.length === 0 && !loadingRepos && activeProjectId && (
              <div className="p-8 text-center text-slate-500">
                <Package2 size={32} className="mx-auto mb-2 opacity-30" />
                <p className="text-sm">No repositories found in this namespace.</p>
                <p className="text-xs mt-2 font-mono bg-slate-950 p-2 rounded text-slate-400">
                  docker push .../{activeNamespace?.name}/my-app:tag
                </p>
              </div>
            )}

            {repos.map((repo: any) => (
              <button
                key={repo.id}
                onClick={() => setSelectedRepo(repo.id)}
                className={`w-full text-left p-4 border-b border-slate-800 transition-colors ${
                  selectedRepo === repo.id ? 'bg-indigo-500/10 border-l-2 border-l-indigo-500' : 'hover:bg-slate-800/30'
                }`}
              >
                <div className="font-medium text-slate-200">{repo.name}</div>
                <div className="text-xs text-slate-500 mt-1 flex justify-between">
                  <span>{repo.is_public ? 'Public' : 'Private'}</span>
                  <span>{repo.download_count || 0} pulls</span>
                </div>
              </button>
            ))}
          </div>
        </div>

        {/* Right Content: Artifacts/Tags List */}
        <div className="flex-1 bg-slate-900 border border-slate-800 rounded-xl overflow-hidden flex flex-col">
          {selectedRepo ? (
            <RepoArtifacts repoId={selectedRepo} namespace={activeNamespace?.name} repoName={repos.find((r:any)=>r.id===selectedRepo)?.name} />
          ) : (
            <div className="flex-1 flex flex-col items-center justify-center text-slate-500">
              <Database size={48} className="mb-4 opacity-20" />
              <p>Select a repository to view its tags and images.</p>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

function RepoArtifacts({ repoId, namespace, repoName }: { repoId: string; namespace: string; repoName: string }) {
  const { data: artifacts, isLoading } = useQuery({
    queryKey: ['registry', 'artifacts', repoId],
    queryFn: () => registryReposApi.listArtifacts(repoId).then((r) => r.data),
  })

  return (
    <div className="flex flex-col h-full">
      <div className="p-5 border-b border-slate-800 flex justify-between items-center">
        <div>
          <h2 className="text-lg font-semibold text-white">{repoName}</h2>
          <p className="text-sm text-slate-400">docker pull your-domain.com/v2/{namespace}/{repoName}</p>
        </div>
      </div>

      <div className="flex-1 overflow-y-auto p-0">
        <table className="w-full text-sm text-left">
          <thead className="bg-slate-800/50 text-slate-400 text-xs uppercase sticky top-0">
            <tr>
              <th className="px-6 py-3 font-medium">Tag</th>
              <th className="px-6 py-3 font-medium">Digest</th>
              <th className="px-6 py-3 font-medium">Size</th>
              <th className="px-6 py-3 font-medium">Pulls</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-slate-800/50">
            {isLoading && (
              <tr><td colSpan={4} className="p-4 text-center">Loading tags...</td></tr>
            )}
            {!isLoading && artifacts?.length === 0 && (
              <tr><td colSpan={4} className="p-8 text-center text-slate-500">No tags pushed yet.</td></tr>
            )}
            {artifacts?.map((art: any) => (
              <tr key={art.id} className="hover:bg-slate-800/20">
                <td className="px-6 py-4">
                  <span className="px-2 py-1 bg-indigo-500/20 text-indigo-300 rounded font-mono text-xs">
                    {art.tag}
                  </span>
                </td>
                <td className="px-6 py-4 font-mono text-xs text-slate-400">
                  {art.digest.substring(0, 15)}...
                </td>
                <td className="px-6 py-4 text-slate-400">
                  {(art.size / 1024 / 1024).toFixed(2)} MB
                </td>
                <td className="px-6 py-4 text-slate-400 flex items-center gap-1">
                  <ArrowDownToLine size={14} />
                  {art.downloads}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
