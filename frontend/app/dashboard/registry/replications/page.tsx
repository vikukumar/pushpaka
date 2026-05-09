'use client'

import { Header } from '@/components/layout/Header'
import { Activity, RefreshCcw, DownloadCloud } from 'lucide-react'

export default function ReplicationsPage() {
  return (
    <div className="flex flex-col min-h-screen">
      <Header title="Registry Replications" subtitle="Manage pull-through cache rules and upstream syncing" />

      <div className="p-6">
        <div className="card text-center py-16">
          <DownloadCloud size={48} className="mx-auto text-slate-700 mb-4" />
          <h3 className="text-white font-semibold mb-2">Pull-Through Proxy Cache</h3>
          <p className="text-slate-400 text-sm mb-5 max-w-lg mx-auto">
            Replications and upstream proxy caching are currently configured via the backend engine. 
            When you pull an image that isn't available locally, Pushpaka automatically proxies it from Docker Hub 
            and caches it to your local registry using the new multi-worker concurrent downloader.
          </p>
          
          <div className="inline-flex items-center gap-2 px-4 py-2 bg-indigo-500/10 text-indigo-400 rounded-lg border border-indigo-500/20 text-sm">
            <Activity size={16} />
            Proxy Cache Engine is Active
          </div>
          
          <div className="mt-8 grid grid-cols-1 md:grid-cols-3 gap-4 text-left max-w-4xl mx-auto">
            <div className="p-4 bg-slate-800/30 rounded-lg border border-slate-700/50">
              <h4 className="font-medium text-slate-200 mb-1">Docker Hub Fallback</h4>
              <p className="text-xs text-slate-400">Unrecognized namespaces automatically fallback to Docker Hub.</p>
            </div>
            <div className="p-4 bg-slate-800/30 rounded-lg border border-slate-700/50">
              <h4 className="font-medium text-slate-200 mb-1">Multi-Worker Downloads</h4>
              <p className="text-xs text-slate-400">Large layers are split into segments and downloaded concurrently.</p>
            </div>
            <div className="p-4 bg-slate-800/30 rounded-lg border border-slate-700/50">
              <h4 className="font-medium text-slate-200 mb-1">Format Agnostic</h4>
              <p className="text-xs text-slate-400">Maintains exact OCI, v1, and v2 manifest media types.</p>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
