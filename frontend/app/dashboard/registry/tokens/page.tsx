'use client'

import { useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { registryTokensApi } from '@/lib/api'
import { Header } from '@/components/layout/Header'
import { KeyRound, Plus, Trash2, Copy, Check } from 'lucide-react'
import { formatDistanceToNow } from 'date-fns'

interface PAT {
  id: string
  name: string
  description: string
  created_at: string
  expires_at: string | null
  last_used_at: string | null
  revoked: boolean
}

export default function TokensPage() {
  const queryClient = useQueryClient()
  const [showModal, setShowModal] = useState(false)
  const [name, setName] = useState('')
  const [expiresIn, setExpiresIn] = useState(30)
  const [newToken, setNewToken] = useState<string | null>(null)
  const [copied, setCopied] = useState(false)

  const { data, isLoading } = useQuery({
    queryKey: ['registry', 'tokens'],
    queryFn: () => registryTokensApi.list().then((r) => r.data),
  })

  const createMutation = useMutation({
    mutationFn: (data: { name: string; expires_in_days: number }) => registryTokensApi.create(data).then(r => r.data),
    onSuccess: (data) => {
      setNewToken(data.plain_token)
      queryClient.invalidateQueries({ queryKey: ['registry', 'tokens'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: string) => registryTokensApi.delete(id),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ['registry', 'tokens'] }),
  })

  const tokens: PAT[] = data?.data || []

  const handleCopy = () => {
    if (newToken) {
      navigator.clipboard.writeText(newToken)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    }
  }

  return (
    <div className="flex flex-col min-h-screen">
      <Header title="Personal Access Tokens" subtitle="Manage tokens for docker login" />

      <div className="p-6 space-y-5">
        <div className="flex justify-end">
          <button onClick={() => { setShowModal(true); setNewToken(null); setName('') }} className="btn-primary">
            <Plus size={15} />
            Generate New Token
          </button>
        </div>

        {newToken && (
          <div className="card border-green-500/30 bg-green-500/5 mb-6">
            <h3 className="text-green-400 font-medium mb-2 flex items-center gap-2">
              <Check size={16} /> Token Generated Successfully
            </h3>
            <p className="text-sm text-slate-300 mb-4">
              Make sure to copy your personal access token now. You won’t be able to see it again!
            </p>
            <div className="flex items-center gap-2">
              <code className="flex-1 p-3 bg-slate-900 rounded border border-slate-700 font-mono text-sm break-all">
                {newToken}
              </code>
              <button onClick={handleCopy} className="btn-secondary whitespace-nowrap">
                {copied ? <Check size={16} /> : <Copy size={16} />}
                {copied ? 'Copied' : 'Copy Token'}
              </button>
            </div>
            <div className="mt-4 p-3 bg-slate-900 rounded border border-slate-800">
              <p className="text-xs text-slate-400 font-mono">
                docker login demo-pushpaka.vikshro.in -u your_email -p {newToken}
              </p>
            </div>
          </div>
        )}

        <div className="card p-0 overflow-hidden">
          <table className="w-full text-sm text-left">
            <thead className="bg-slate-800/50 text-slate-400 text-xs uppercase">
              <tr>
                <th className="px-6 py-3 font-medium">Name</th>
                <th className="px-6 py-3 font-medium">Created</th>
                <th className="px-6 py-3 font-medium">Expires</th>
                <th className="px-6 py-3 font-medium">Last Used</th>
                <th className="px-6 py-3 font-medium text-right">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-slate-800/50">
              {tokens.length === 0 && !isLoading && (
                <tr>
                  <td colSpan={5} className="px-6 py-8 text-center text-slate-400">
                    <KeyRound size={24} className="mx-auto mb-2 opacity-50" />
                    No tokens found. Generate one to authenticate Docker CLI.
                  </td>
                </tr>
              )}
              {tokens.map((token) => (
                <tr key={token.id} className="hover:bg-slate-800/20">
                  <td className="px-6 py-4 font-medium text-white flex items-center gap-2">
                    <KeyRound size={14} className="text-indigo-400" />
                    {token.name}
                  </td>
                  <td className="px-6 py-4 text-slate-400">
                    {new Date(token.created_at).toLocaleDateString()}
                  </td>
                  <td className="px-6 py-4 text-slate-400">
                    {token.expires_at ? new Date(token.expires_at).toLocaleDateString() : 'Never'}
                  </td>
                  <td className="px-6 py-4 text-slate-400">
                    {token.last_used_at ? formatDistanceToNow(new Date(token.last_used_at), { addSuffix: true }) : 'Never'}
                  </td>
                  <td className="px-6 py-4 text-right">
                    <button
                      onClick={() => deleteMutation.mutate(token.id)}
                      className="text-red-400 hover:text-red-300 transition-colors p-2 rounded hover:bg-red-400/10"
                      title="Revoke Token"
                    >
                      <Trash2 size={16} />
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {showModal && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4">
          <div className="bg-slate-900 border border-slate-800 rounded-xl shadow-2xl w-full max-w-md overflow-hidden">
            <div className="p-6">
              <h3 className="text-lg font-semibold text-white mb-4">Generate Access Token</h3>
              
              <div className="space-y-4">
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1">Token Name</label>
                  <input
                    type="text"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    className="input w-full"
                    placeholder="e.g. CI/CD Pipeline"
                  />
                </div>
                
                <div>
                  <label className="block text-xs font-medium text-slate-400 mb-1">Expiration (Days)</label>
                  <select
                    value={expiresIn}
                    onChange={(e) => setExpiresIn(Number(e.target.value))}
                    className="input w-full"
                  >
                    <option value={7}>7 days</option>
                    <option value={30}>30 days</option>
                    <option value={90}>90 days</option>
                    <option value={365}>1 year</option>
                    <option value={0}>No expiration</option>
                  </select>
                </div>
              </div>
            </div>
            
            <div className="p-4 bg-slate-800/50 border-t border-slate-800 flex justify-end gap-3">
              <button onClick={() => setShowModal(false)} className="btn-secondary">
                Cancel
              </button>
              <button
                onClick={() => {
                  createMutation.mutate({ name, expires_in_days: expiresIn })
                  setShowModal(false)
                }}
                disabled={!name.trim() || createMutation.isPending}
                className="btn-primary"
              >
                {createMutation.isPending ? 'Generating...' : 'Generate Token'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
