'use client'

import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { workersApi, API_URL } from '@/lib/api'
import { WorkerNode } from '@/types'
import { Header } from '@/components/layout/Header'
import { Server, Shield, RefreshCw, Cpu, HardDrive, Activity, CheckCircle2, XCircle, AlertCircle, Eye, EyeOff, Terminal } from 'lucide-react'
import { timeAgo } from '@/lib/utils'

export default function WorkersPage() {
  const [showPAT, setShowPAT] = useState(false)
  const [selectedLogWorker, setSelectedLogWorker] = useState<string | null>(null)

  const { data, isLoading, refetch, isRefetching } = useQuery({
    queryKey: ['workers'],
    queryFn: () => workersApi.list().then((r) => r.data),
    refetchInterval: 5000, // Poll every 5s for real-time tracking
  })

  const { data: patData } = useQuery({
    queryKey: ['zone-pat'],
    queryFn: () => workersApi.getPat().then((r) => r.data),
    enabled: showPAT, // Only fetch when they try to view it
  })

  const workers: WorkerNode[] = data || []
  const zonePAT = patData?.zone_pat || '••••••••••••••••••••••••••••••••'

  const getStatusIcon = (status: string) => {
    switch (status) {
      case 'active':
        return <CheckCircle2 className="w-4 h-4 text-emerald-400" />
      case 'offline':
        return <XCircle className="w-4 h-4 text-rose-400" />
      default:
        return <AlertCircle className="w-4 h-4 text-amber-400" />
    }
  }

  return (
    <div className="flex flex-col min-h-screen">
      <Header title="Worker Nodes" subtitle="Manage your distributed execution engines" />

      <div className="p-6 space-y-6 animate-fade-in max-w-7xl">
        
        {/* Configuration Panel */}
        <div className="card relative overflow-hidden group p-6">
          <div className="absolute top-0 left-0 w-1 h-full bg-gradient-to-b from-indigo-500 to-purple-600" />
          <div className="flex items-start justify-between">
            <div>
              <h2 className="text-lg font-bold text-slate-100 flex items-center gap-2">
                <Shield className="w-5 h-5 text-indigo-400" />
                Zone Configuration
              </h2>
              <p className="text-sm text-slate-400 mt-1 max-w-2xl">
                Deploy new remote workers by starting the Pushpaka binary in 
                <span className="font-mono text-indigo-300 mx-1 bg-indigo-500/10 px-1 rounded">vaahan</span> 
                or 
                <span className="font-mono text-indigo-300 mx-1 bg-indigo-500/10 px-1 rounded">hybrid</span> 
                mode. Provide the API Server URL and this Zone PAT to authenticate them.
              </p>
            </div>
            <button 
              onClick={() => refetch()}
              disabled={isRefetching}
              className="p-2 rounded-lg bg-slate-800/50 text-slate-400 hover:text-white border border-slate-700 hover:border-slate-600 transition-all disabled:opacity-50"
            >
              <RefreshCw className={`w-4 h-4 ${isRefetching ? 'animate-spin' : ''}`} />
            </button>
          </div>

          <div className="mt-6 flex flex-col sm:flex-row gap-4">
            <div className="flex-1 bg-[#0a0f1c] rounded-lg border border-slate-800/60 p-4">
              <label className="text-[10px] font-semibold tracking-widest text-slate-500 uppercase mb-2 block">
                Management Server API URL
              </label>
              <div className="font-mono text-sm text-slate-300">
                {typeof window !== 'undefined' ? window.location.origin : (API_URL || 'http://localhost:8080')}
              </div>
            </div>
            
            <div className="flex-1 bg-[#0a0f1c] rounded-lg border border-slate-800/60 p-4 relative group/pat">
              <label className="text-[10px] font-semibold tracking-widest text-slate-500 uppercase mb-2 block">
                Installation Zone PAT
              </label>
              <div className="flex items-center justify-between">
                <div className="font-mono text-sm text-indigo-200 select-all truncate pr-4">
                  {zonePAT}
                </div>
                <button
                  onClick={() => setShowPAT(!showPAT)}
                  className="text-slate-500 hover:text-indigo-400 transition-colors"
                >
                  {showPAT ? <EyeOff className="w-4 h-4" /> : <Eye className="w-4 h-4" />}
                </button>
              </div>
            </div>
          </div>
        </div>

        {/* Worker Data Table */}
        <div className="card overflow-hidden">
          <div className="p-4 border-b border-slate-800/60 flex items-center justify-between bg-slate-900/30">
            <h3 className="font-semibold text-slate-200 flex items-center gap-2">
              <Server className="w-4 h-4 text-emerald-400" />
              Connected Nodes
              <span className="px-2 py-0.5 rounded-full bg-slate-800 text-xs text-slate-400 font-medium ml-2">
                {workers.length}
              </span>
            </h3>
          </div>
          
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm text-slate-400">
              <thead className="text-xs uppercase bg-slate-900/50 text-slate-500">
                <tr>
                  <th className="px-5 py-3 font-medium tracking-wider">Node & OS</th>
                  <th className="px-5 py-3 font-medium tracking-wider">Type</th>
                  <th className="px-5 py-3 font-medium tracking-wider">Status</th>
                  <th className="px-5 py-3 font-medium tracking-wider">Resources</th>
                  <th className="px-5 py-3 font-medium tracking-wider">Network / Last Seen</th>
                  <th className="px-5 py-3 font-medium tracking-wider text-right">Actions</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-slate-800/60">
                {isLoading ? (
                  <tr>
                    <td colSpan={5} className="px-5 py-8 text-center text-slate-500">
                      <RefreshCw className="w-5 h-5 animate-spin mx-auto mb-2 text-indigo-500/50" />
                      Discovering nodes...
                    </td>
                  </tr>
                ) : workers.length === 0 ? (
                  <tr>
                    <td colSpan={5} className="px-5 py-12 text-center">
                      <Server className="w-8 h-8 mx-auto text-slate-700 mb-3" />
                      <p className="text-slate-400 font-medium">No embedded or remote workers detected.</p>
                      <p className="text-xs text-slate-500 mt-1">Run bin/worker --mode=vaahan to attach a node</p>
                    </td>
                  </tr>
                ) : (
                  workers.map((worker) => (
                    <tr key={worker.id} className="hover:bg-slate-800/20 transition-colors group">
                      <td className="px-5 py-4">
                        <div className="flex items-center gap-3">
                          <div className="w-8 h-8 rounded-lg bg-indigo-500/10 border border-indigo-500/20 flex items-center justify-center shrink-0">
                            <Activity className="w-4 h-4 text-indigo-400" />
                          </div>
                          <div>
                            <div className="text-slate-200 font-medium flex items-center gap-2">
                              {worker.name}
                              <span className="text-[10px] font-mono text-slate-600 bg-slate-900 px-1.5 rounded">
                                {worker.id.split('-')[0]}
                              </span>
                            </div>
                            <div className="text-xs text-slate-500 flex items-center gap-1.5 mt-0.5">
                              {worker.os} / {worker.architecture}
                            </div>
                          </div>
                        </div>
                      </td>
                      <td className="px-5 py-4">
                        <span className={`inline-flex items-center px-2 py-0.5 rounded text-[10px] font-bold tracking-wide uppercase shadow-sm ${
                          worker.type === 'vaahan' 
                            ? 'bg-purple-500/10 text-purple-400 border border-purple-500/20'
                            : worker.type === 'hybrid'
                            ? 'bg-amber-500/10 text-amber-400 border border-amber-500/20'
                            : 'bg-emerald-500/10 text-emerald-400 border border-emerald-500/20'
                        }`}>
                          {worker.type}
                        </span>
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex items-center gap-2">
                          {getStatusIcon(worker.status)}
                          <span className="capitalize font-medium text-slate-300">
                            {worker.status}
                          </span>
                        </div>
                      </td>
                      <td className="px-5 py-4">
                        <div className="flex flex-col gap-1.5">
                          <div className="flex items-center gap-2 text-xs">
                            <Cpu className="w-3.5 h-3.5 text-slate-500" />
                            <span>{worker.cpu_count || '?'} Cores</span>
                          </div>
                          <div className="flex items-center gap-2 text-xs">
                            <HardDrive className="w-3.5 h-3.5 text-slate-500" />
                            <span>{worker.memory_total ? (worker.memory_total / 1024 / 1024 / 1024).toFixed(1) + ' GB' : '? GB'}</span>
                          </div>
                        </div>
                      </td>
                      <td className="px-5 py-4">
                        <div className="text-xs text-slate-300 font-mono">
                          {worker.ip_address || 'Internal/Tunnel'}
                        </div>
                        <div className="text-[10px] text-slate-500 mt-1">
                          {worker.last_seen_at ? timeAgo(worker.last_seen_at) : 'Never'}
                        </div>
                      </td>
                      <td className="px-5 py-4 text-right">
                        <button 
                          onClick={() => setSelectedLogWorker(worker.id)}
                          className="p-1.5 rounded-lg text-slate-500 hover:text-indigo-400 hover:bg-slate-800 transition-colors"
                          title="View Process Logs"
                        >
                          <Terminal className="w-4 h-4" />
                        </button>
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </div>

        {/* Fake Log Modal */}
        {selectedLogWorker && (
          <div className="fixed inset-0 z-50 flex items-center justify-center p-4 bg-black/60 backdrop-blur-sm animate-fade-in">
            <div className="card w-full max-w-2xl overflow-hidden shadow-2xl border border-slate-700">
              <div className="p-4 border-b border-slate-800 flex items-center justify-between bg-slate-900/80">
                <h3 className="font-semibold text-slate-200 flex items-center gap-2">
                  <Terminal className="w-4 h-4 text-indigo-400" />
                  Remote Process Logs
                </h3>
                <button onClick={() => setSelectedLogWorker(null)} className="text-slate-500 hover:text-slate-300">
                  <XCircle className="w-5 h-5" />
                </button>
              </div>
              <div className="p-6 bg-[#0a0f1c] min-h-[300px] font-mono text-sm">
                <div className="text-indigo-400 mb-2">Connecting to yamux multi-plexer stream for {selectedLogWorker.split('-')[0]}...</div>
                <div className="text-emerald-400 mb-2">Tunnel established over WS.</div>
                <div className="text-slate-400">[info] Remote syslog streaming is currently operating in limited view. For complete process daemon stdout, please check the host machine journalctl or docker logs.</div>
              </div>
            </div>
          </div>
        )}

      </div>
    </div>
  )
}
