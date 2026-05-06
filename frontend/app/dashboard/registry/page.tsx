'use client';

import React, { useState, useEffect } from 'react';
import { 
  Database, 
  Package, 
  RefreshCw, 
  Plus, 
  Search, 
  ExternalLink, 
  Shield, 
  Activity,
  Box,
  FileCode,
  Download,
  Trash2,
  AlertCircle
} from 'lucide-react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { registryApi, projectsApi } from '@/lib/api';
import { toast } from 'react-hot-toast';

export default function RegistryPage() {
  const queryClient = useQueryClient();
  const [activeTab, setActiveTab] = useState<'docker' | 'helm' | 'binary'>('docker');
  const [searchQuery, setSearchQuery] = useState('');
  const [isModalOpen, setIsModalOpen] = useState(false);
  const [selectedProjectId, setSelectedProjectId] = useState<string>('');

  // Form State
  const [newRepoName, setNewRepoName] = useState('');
  const [newRepoDesc, setNewRepoDesc] = useState('');
  const [newRepoPublic, setNewRepoPublic] = useState(false);

  // Fetch Projects
  const { data: projectsData, isLoading: projectsLoading } = useQuery({
    queryKey: ['projects'],
    queryFn: () => projectsApi.list().then(r => r.data.data),
    select: (data: any[]) => data.filter(p => p.type === 'registry'),
  });

  useEffect(() => {
    if (projectsData && projectsData.length > 0) {
      // If current selected project is not in registry list, select the first one
      if (!selectedProjectId || !projectsData?.find((p: any) => p.id === selectedProjectId)) {
        setSelectedProjectId(projectsData?.[0]?.id || '');
      }
    } else {
      setSelectedProjectId('');
    }
  }, [projectsData, selectedProjectId]);

  // Fetch Repositories
  const { data: repos, isLoading } = useQuery({
    queryKey: ['registry', 'repos', selectedProjectId],
    queryFn: () => registryApi.listRepos(selectedProjectId).then(r => r.data),
    enabled: !!selectedProjectId,
  });

  // Create Repo Mutation
  const createMutation = useMutation({
    mutationFn: (data: any) => registryApi.createRepo(data),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registry', 'repos'] });
      setIsModalOpen(false);
      setNewRepoName('');
      setNewRepoDesc('');
      toast.success('Repository created successfully');
    },
    onError: (err: any) => {
      toast.error('Failed to create repository: ' + (err.response?.data?.error || err.message));
    }
  });

  // Delete Repo Mutation
  const deleteMutation = useMutation({
    mutationFn: (id: string) => registryApi.deleteRepo(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['registry', 'repos'] });
      toast.success('Repository deleted');
    }
  });

  const filteredRepos = (repos || []).filter((r: any) => 
    r.type === activeTab && 
    r.name.toLowerCase().includes(searchQuery.toLowerCase())
  );

  const handleCreateRepo = (e: React.FormEvent) => {
    e.preventDefault();
    if (!selectedProjectId) return toast.error('Please select a project');
    createMutation.mutate({
      project_id: selectedProjectId,
      name: newRepoName,
      type: activeTab,
      description: newRepoDesc,
      is_public: newRepoPublic
    });
  };

  return (
    <div className="p-6 space-y-8 animate-in fade-in duration-700">
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-blue-400 to-purple-500">
            Vahan Registry
          </h1>
          <p className="text-slate-400 mt-1">Manage your Docker images, Helm charts, and application binaries in one place.</p>
        </div>
        
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2">
            <select 
              className="bg-slate-900 border border-slate-800 rounded-lg px-3 py-2 text-sm focus:ring-2 focus:ring-blue-500/50 outline-none min-w-[150px]"
              value={selectedProjectId}
              onChange={(e) => setSelectedProjectId(e.target.value)}
              disabled={!projectsData || projectsData.length === 0}
            >
              {projectsData?.length === 0 && <option value="">No Registry Projects</option>}
              {projectsData?.map((p: any) => (
                <option key={p.id} value={p.id}>{p.name}</option>
              ))}
            </select>
            <button 
              onClick={() => window.location.href = '/dashboard/projects/new?type=registry'}
              className="p-2 bg-slate-800 hover:bg-slate-700 text-slate-300 rounded-lg border border-slate-700 transition-all"
              title="Create New Registry Project"
            >
              <Plus size={18} />
            </button>
          </div>
          
          <button 
            disabled={!selectedProjectId}
            onClick={() => setIsModalOpen(true)}
            className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 disabled:cursor-not-allowed transition-all rounded-lg font-medium shadow-lg shadow-blue-500/20"
          >
            <Plus size={18} />
            Create {activeTab.charAt(0).toUpperCase() + activeTab.slice(1)} Repo
          </button>
        </div>
      </div>

      {/* Tabs & Search */}
      <div className="flex flex-col md:flex-row gap-4 items-center justify-between bg-slate-900/30 p-2 rounded-xl border border-slate-800/50">
        <div className="flex gap-2 p-1">
          {(['docker', 'helm', 'binary'] as const).map((tab) => (
            <button
              key={tab}
              onClick={() => setActiveTab(tab)}
              className={`px-6 py-2 rounded-lg capitalize transition-all ${
                activeTab === tab 
                  ? 'bg-blue-600 text-white shadow-md' 
                  : 'text-slate-400 hover:text-slate-200 hover:bg-slate-800'
              }`}
            >
              {tab}
            </button>
          ))}
        </div>
        
        <div className="relative w-full md:w-64 mr-2">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500" size={16} />
          <input 
            type="text"
            placeholder="Search repositories..."
            className="w-full pl-10 pr-4 py-2 bg-slate-950 border border-slate-800 rounded-lg focus:outline-none focus:ring-2 focus:ring-blue-500/50 transition-all text-sm"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
        </div>
      </div>

      {/* Repository Grid */}
      {isLoading ? (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {[1, 2, 3, 4].map(i => (
            <div key={i} className="h-40 bg-slate-900/50 animate-pulse rounded-2xl border border-slate-800" />
          ))}
        </div>
      ) : filteredRepos.length === 0 ? (
        <div className="flex flex-col items-center justify-center py-20 bg-slate-900/20 border border-dashed border-slate-800 rounded-3xl">
          <Database size={48} className="text-slate-700 mb-4" />
          <h3 className="text-xl font-semibold text-slate-400">No repositories found</h3>
          <p className="text-slate-500 mt-2">Create your first {activeTab} repository to get started.</p>
        </div>
      ) : (
        <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
          {filteredRepos.map((repo: any) => (
            <div key={repo.id} className="group relative bg-slate-900/40 border border-slate-800/50 hover:border-blue-500/30 transition-all rounded-2xl overflow-hidden backdrop-blur-xl">
              <div className="p-6">
                <div className="flex items-start justify-between">
                  <div className="flex items-center gap-4">
                    <div className={`p-3 rounded-2xl ${
                      activeTab === 'docker' ? 'bg-blue-500/10 text-blue-400' :
                      activeTab === 'helm' ? 'bg-purple-500/10 text-purple-400' :
                      'bg-emerald-500/10 text-emerald-400'
                    }`}>
                      {activeTab === 'docker' ? <Box size={24} /> : activeTab === 'helm' ? <FileCode size={24} /> : <Package size={24} />}
                    </div>
                    <div>
                      <h3 className="text-lg font-semibold text-slate-100 group-hover:text-blue-400 transition-colors">
                        {repo.name}
                      </h3>
                      <p className="text-sm text-slate-500 line-clamp-1">{repo.description || 'No description provided'}</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    {repo.is_public && (
                      <span className="px-2 py-0.5 text-[10px] font-bold uppercase tracking-tighter bg-emerald-500/10 text-emerald-400 border border-emerald-500/20 rounded-full">
                        Public
                      </span>
                    )}
                    <button 
                      onClick={() => deleteMutation.mutate(repo.id)}
                      className="p-2 text-slate-600 hover:text-red-400 hover:bg-red-400/5 rounded-lg transition-all"
                    >
                      <Trash2 size={16} />
                    </button>
                  </div>
                </div>

                <div className="mt-6 flex items-center justify-between">
                  <div className="flex items-center gap-4 text-xs text-slate-500">
                    <div className="flex items-center gap-1">
                      <Download size={12} /> {repo.download_count || 0} pulls
                    </div>
                    <div>Created {new Date(repo.created_at).toLocaleDateString()}</div>
                  </div>
                  
                  <div className="flex items-center gap-3">
                    <button 
                      onClick={() => {
                        registryApi.triggerSync(repo.id);
                        toast.success('Sync triggered');
                      }}
                      className="p-2 text-slate-500 hover:text-blue-400 hover:bg-blue-400/5 rounded-lg transition-all" 
                      title="Sync Now"
                    >
                      <RefreshCw size={18} />
                    </button>
                    <button className="flex items-center gap-2 px-4 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm transition-all">
                      Details
                      <ExternalLink size={14} />
                    </button>
                  </div>
                </div>
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Modals */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm">
          <div className="bg-slate-900 border border-slate-800 w-full max-w-md rounded-2xl p-6 shadow-2xl shadow-blue-500/10">
            <h2 className="text-xl font-bold mb-4 flex items-center gap-2">
              <Plus className="text-blue-400" />
              Create New Repository
            </h2>
            <form onSubmit={handleCreateRepo} className="space-y-4">
              <div>
                <label className="block text-sm text-slate-400 mb-1">Repository Name</label>
                <input 
                  autoFocus
                  required
                  type="text"
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-2 text-sm focus:ring-2 focus:ring-blue-500/50 outline-none"
                  placeholder="e.g. backend-api"
                  value={newRepoName}
                  onChange={e => setNewRepoName(e.target.value)}
                />
              </div>
              <div>
                <label className="block text-sm text-slate-400 mb-1">Description</label>
                <textarea 
                  className="w-full bg-slate-950 border border-slate-800 rounded-lg px-4 py-2 text-sm focus:ring-2 focus:ring-blue-500/50 outline-none min-h-[80px]"
                  placeholder="Optional description..."
                  value={newRepoDesc}
                  onChange={e => setNewRepoDesc(e.target.value)}
                />
              </div>
              <div className="flex items-center gap-2 py-2">
                <input 
                  type="checkbox"
                  id="isPublic"
                  checked={newRepoPublic}
                  onChange={e => setNewRepoPublic(e.target.checked)}
                  className="w-4 h-4 rounded border-slate-800 bg-slate-950 text-blue-600 focus:ring-blue-500/50"
                />
                <label htmlFor="isPublic" className="text-sm text-slate-300">Public visibility</label>
              </div>
              
              <div className="flex gap-3 mt-6">
                <button 
                  type="button"
                  onClick={() => setIsModalOpen(false)}
                  className="flex-1 px-4 py-2 bg-slate-800 hover:bg-slate-700 rounded-lg font-medium transition-all"
                >
                  Cancel
                </button>
                <button 
                  type="submit"
                  disabled={createMutation.isPending}
                  className="flex-1 px-4 py-2 bg-blue-600 hover:bg-blue-500 disabled:opacity-50 rounded-lg font-medium transition-all shadow-lg shadow-blue-500/20"
                >
                  {createMutation.isPending ? 'Creating...' : 'Create'}
                </button>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Pull Instruction Tip */}
      <div className="bg-blue-500/5 border border-blue-500/10 p-4 rounded-2xl flex items-start gap-4">
        <AlertCircle className="text-blue-400 shrink-0" size={24} />
        <div>
          <h4 className="text-sm font-semibold text-blue-300">Registry Access</h4>
          <p className="text-xs text-blue-300/60 mt-1 leading-relaxed">
            To pull from this registry: <code className="bg-slate-950 px-2 py-0.5 rounded text-blue-400">docker pull {typeof window !== 'undefined' ? window.location.host : 'localhost'}/registry/oci/&lt;project_id&gt;/&lt;repo_name&gt;</code>.
            Use your platform credentials or a Personal Access Token for authentication.
          </p>
        </div>
      </div>
    </div>
  );
}
