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
  Download
} from 'lucide-react';

export default function RegistryPage() {
  const [activeTab, setActiveTab] = useState<'docker' | 'helm' | 'binary'>('docker');
  const [searchQuery, setSearchQuery] = useState('');

  return (
    <div className="p-6 space-y-8 animate-in fade-in duration-700">
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold bg-clip-text text-transparent bg-gradient-to-r from-blue-400 to-purple-500">
            Vahan Registry
          </h1>
          <p className="text-slate-400 mt-1">Manage your Docker images, Helm charts, and application binaries in one place.</p>
        </div>
        
        <button className="flex items-center gap-2 px-4 py-2 bg-blue-600 hover:bg-blue-500 transition-all rounded-lg font-medium shadow-lg shadow-blue-500/20">
          <Plus size={18} />
          Create Repository
        </button>
      </div>

      {/* Stats Overview */}
      <div className="grid grid-cols-1 md:grid-cols-4 gap-4">
        {[
          { label: 'Total Images', value: '124', icon: <Box className="text-blue-400" /> },
          { label: 'Helm Charts', value: '42', icon: <FileCode className="text-purple-400" /> },
          { label: 'Binaries', value: '89', icon: <Package className="text-emerald-400" /> },
          { label: 'Active Replications', value: '5', icon: <RefreshCw className="text-orange-400 animate-spin-slow" /> },
        ].map((stat, i) => (
          <div key={i} className="bg-slate-900/50 border border-slate-800 p-4 rounded-xl backdrop-blur-md">
            <div className="flex items-center gap-3">
              <div className="p-2 bg-slate-800 rounded-lg">{stat.icon}</div>
              <div>
                <p className="text-xs text-slate-500 uppercase tracking-wider">{stat.label}</p>
                <p className="text-xl font-bold text-slate-100">{stat.value}</p>
              </div>
            </div>
          </div>
        ))}
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
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Placeholder for real data mapping */}
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="group relative bg-slate-900/40 border border-slate-800/50 hover:border-blue-500/30 transition-all rounded-2xl overflow-hidden backdrop-blur-xl">
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
                      pushpaka-vahan/{activeTab}-repo-{i}
                    </h3>
                    <p className="text-sm text-slate-500 line-clamp-1">Last pushed 2 hours ago • 1.2 GB</p>
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  <span className="px-2 py-0.5 text-[10px] font-bold uppercase tracking-tighter bg-blue-500/10 text-blue-400 border border-blue-500/20 rounded-full">
                    Active
                  </span>
                </div>
              </div>

              <div className="mt-6 flex items-center justify-between">
                <div className="flex -space-x-2">
                  {[1, 2, 3].map(t => (
                    <div key={t} className="w-8 h-8 rounded-full border-2 border-slate-900 bg-slate-800 flex items-center justify-center text-[10px] font-bold text-slate-400">
                      v{t}
                    </div>
                  ))}
                  <div className="w-8 h-8 rounded-full border-2 border-slate-900 bg-slate-800 flex items-center justify-center text-[10px] font-bold text-slate-500">
                    +12
                  </div>
                </div>
                
                <div className="flex items-center gap-3">
                   <button className="p-2 text-slate-500 hover:text-blue-400 hover:bg-blue-400/5 rounded-lg transition-all" title="Replication Settings">
                    <RefreshCw size={18} />
                  </button>
                  <button className="flex items-center gap-2 px-4 py-1.5 bg-slate-800 hover:bg-slate-700 text-slate-200 rounded-lg text-sm transition-all">
                    Details
                    <ExternalLink size={14} />
                  </button>
                </div>
              </div>
            </div>
            
            {/* Replication Progress Overlay (Conditional) */}
            {i === 2 && (
              <div className="absolute bottom-0 left-0 right-0 h-1 bg-slate-800">
                <div className="h-full bg-blue-500 animate-pulse w-3/4"></div>
              </div>
            )}
          </div>
        ))}
      </div>

      {/* Pull Instruction Tip */}
      <div className="bg-blue-500/5 border border-blue-500/10 p-4 rounded-2xl flex items-start gap-4">
        <Shield className="text-blue-400 shrink-0" size={24} />
        <div>
          <h4 className="text-sm font-semibold text-blue-300">Registry Pull Instructions</h4>
          <p className="text-xs text-blue-300/60 mt-1 leading-relaxed">
            To pull images from this registry, use: <code className="bg-slate-950 px-2 py-0.5 rounded text-blue-400">docker pull {typeof window !== 'undefined' ? window.location.host : 'localhost'}/registry/oci/&lt;project&gt;/&lt;repo&gt;:&lt;tag&gt;</code>. 
            Ensure you have authenticated using your Pushpaka credentials or a Personal Access Token.
          </p>
        </div>
      </div>
    </div>
  );
}
