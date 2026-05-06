import { Info, Terminal, Key, Shield, Globe, Copy, Check } from 'lucide-react'
import { useState } from 'react'
import toast from 'react-hot-toast'

interface RegistrySettingsProps {
  projectId: string
}

export function RegistrySettings({ projectId }: RegistrySettingsProps) {
  const [copied, setCopied] = useState<string | null>(null)
  
  const host = typeof window !== 'undefined' ? window.location.host : 'registry.pushpaka.dev'
  const registryUrl = `${host}/registry/oci/${projectId}`

  const copyToClipboard = (text: string, id: string) => {
    navigator.clipboard.writeText(text)
    setCopied(id)
    toast.success('Copied to clipboard')
    setTimeout(() => setCopied(null), 2000)
  }

  const sections = [
    {
      title: 'Login',
      icon: <Key size={16} className="text-brand-400" />,
      command: `docker login ${host}`,
      description: 'Use your Pushpaka email and password (or a Service Token) to authenticate.'
    },
    {
      title: 'Tag Image',
      icon: <Terminal size={16} className="text-brand-400" />,
      command: `docker tag my-image:latest ${registryUrl}/my-repo:latest`,
      description: 'Tag your local image with the Pushpaka registry path.'
    },
    {
      title: 'Push Image',
      icon: <Shield size={16} className="text-brand-400" />,
      command: `docker push ${registryUrl}/my-repo:latest`,
      description: 'Push the tagged image to your private repository.'
    },
    {
      title: 'Public Access',
      icon: <Globe size={16} className="text-brand-400" />,
      command: `docker pull ${registryUrl}/my-repo:latest`,
      description: 'If your repository is marked as Public, anyone can pull without authentication.'
    }
  ]

  return (
    <div className="space-y-6 animate-in fade-in slide-in-from-bottom-4 duration-500">
      <div className="flex items-center gap-2 mb-2">
        <Info size={18} className="text-brand-400" />
        <h2 className="text-lg font-semibold text-white">Registry Instructions</h2>
      </div>

      <div className="grid grid-cols-1 gap-4">
        {sections.map((s, idx) => (
          <div key={idx} className="card bg-slate-900/50 border-slate-800/80 group">
            <div className="flex items-start gap-4">
              <div className="p-2 rounded-lg bg-brand-500/10 shrink-0">
                {s.icon}
              </div>
              <div className="flex-1 min-w-0">
                <h3 className="text-sm font-medium text-slate-200 mb-1">{s.title}</h3>
                <p className="text-xs text-slate-500 mb-3">{s.description}</p>
                
                <div className="relative group/cmd">
                  <div className="bg-black/40 rounded-lg p-3 font-mono text-xs text-brand-300 border border-white/5 break-all pr-10">
                    {s.command}
                  </div>
                  <button 
                    onClick={() => copyToClipboard(s.command, s.title)}
                    className="absolute right-2 top-2 p-1.5 rounded-md bg-slate-800 text-slate-400 hover:text-white hover:bg-slate-700 transition-all opacity-0 group-hover/cmd:opacity-100"
                  >
                    {copied === s.title ? <Check size={12} className="text-emerald-400" /> : <Copy size={12} />}
                  </button>
                </div>
              </div>
            </div>
          </div>
        ))}
      </div>

      <div className="card border-amber-500/20 bg-amber-500/5">
        <div className="flex gap-3">
          <Info size={16} className="text-amber-500 shrink-0 mt-0.5" />
          <div className="text-xs text-amber-200/70 leading-relaxed">
            <span className="font-semibold text-amber-400">Pro Tip:</span> For CI/CD pipelines, we recommend creating a dedicated <span className="text-amber-300">Service Token</span> in the User Settings. This avoids using your personal password in automation scripts.
          </div>
        </div>
      </div>
    </div>
  )
}
