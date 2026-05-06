'use client'

import { useState } from 'react'
import { useRouter } from 'next/navigation'
import { useQueryClient } from '@tanstack/react-query'
import { projectsApi } from '@/lib/api'
import { Header } from '@/components/layout/Header'
import toast from 'react-hot-toast'
import { Loader2, GitBranch, Terminal, Globe, Lock, Eye, EyeOff, Zap, Rocket, Database } from 'lucide-react'
import { Select } from '@/components/ui/Select'

const FRAMEWORKS = [
  { value: '', label: 'Auto-detect' },
  { value: 'nextjs', label: 'Next.js' },
  { value: 'react', label: 'React (Vite/CRA)' },
  { value: 'vue', label: 'Vue.js' },
  { value: 'nodejs', label: 'Node.js' },
  { value: 'python', label: 'Python (Flask/FastAPI/Django)' },
  { value: 'go', label: 'Go' },
  { value: 'rust', label: 'Rust' },
  { value: 'java', label: 'Java (Spring Boot)' },
  { value: 'tomcat', label: 'Apache Tomcat (Java Web)' },
  { value: 'php', label: 'PHP (Laravel/Symfony)' },
  { value: 'c', label: 'C (Make/CMake)' },
  { value: 'cpp', label: 'C++ (CMake)' },
  { value: 'csharp', label: 'C# / .NET' },
  { value: 'nginx', label: 'Nginx (Static/Proxy)' },
  { value: 'lua', label: 'Lua' },
  { value: 'typescript', label: 'TypeScript (Express/Fastify)' },
  { value: 'static', label: 'Static HTML/CSS/JS' },
  { value: 'docker', label: 'Docker (custom Dockerfile)' },
]

interface Template {
  name: string
  description: string
  icon: string
  framework: string
  repo_url: string
  branch: string
  build_command: string
  start_command: string
  port: number
}

const TEMPLATES: Template[] = [
  {
    name: 'Next.js Starter',
    description: 'Production-ready Next.js 14 app with App Router',
    icon: '⚡',
    framework: 'nextjs',
    repo_url: 'https://github.com/vercel/nextjs-portfolio-starter',
    branch: 'main',
    build_command: 'npm run build',
    start_command: 'npm start',
    port: 3000,
  },
  {
    name: 'React Vite App',
    description: 'Lightning-fast React SPA with Vite bundler',
    icon: '⚛️',
    framework: 'react',
    repo_url: 'https://github.com/safak/youtube-react-estate-app',
    branch: 'starter',
    build_command: 'npm run build',
    start_command: 'npx serve dist -p 3000',
    port: 3000,
  },
  {
    name: 'Node.js Express API',
    description: 'REST API starter with Express.js',
    icon: '🟢',
    framework: 'nodejs',
    repo_url: 'https://github.com/hagopj13/node-express-boilerplate',
    branch: 'master',
    build_command: '',
    start_command: 'node src/index.js',
    port: 3000,
  },
  {
    name: 'Python FastAPI',
    description: 'Async Python REST API with OpenAPI docs',
    icon: '🐍',
    framework: 'python',
    repo_url: 'https://github.com/zhanymkanov/fastapi_production_template',
    branch: 'master',
    build_command: 'pip install -r requirements.txt',
    start_command: 'uvicorn src.main:app --host 0.0.0.0 --port 8000',
    port: 8000,
  },
  {
    name: 'Go Gin API',
    description: 'High-performance Go REST API with Gin',
    icon: '🐹',
    framework: 'go',
    repo_url: 'https://github.com/vsouza/go-gin-boilerplate',
    branch: 'master',
    build_command: 'go build -o server .',
    start_command: './server',
    port: 8080,
  },
  {
    name: 'Rust Axum API',
    description: 'High-performance Rust REST API with Axum',
    icon: '🦀',
    framework: 'rust',
    repo_url: 'https://github.com/tokio-rs/axum',
    branch: 'main',
    build_command: 'cargo build --release --example hello-world',
    start_command: './target/release/examples/hello-world',
    port: 3000,
  },
  {
    name: 'Static Portfolio',
    description: 'Zero-dependency HTML/CSS/JS portfolio site',
    icon: '🌐',
    framework: 'static',
    repo_url: 'https://github.com/cobiwave/simplefolio',
    branch: 'master',
    build_command: '',
    start_command: 'npx serve . -p 3000',
    port: 3000,
  },
  {
    name: 'Vue.js SPA',
    description: 'Single-page Vue 3 app with Vite + Pinia',
    icon: '💚',
    framework: 'vue',
    repo_url: 'https://github.com/piniajs/example-vue-3-vite',
    branch: 'main',
    build_command: 'npm run build',
    start_command: 'npx serve dist -p 3000',
    port: 3000,
  },
  {
    name: 'PHP Laravel',
    description: 'Modern PHP framework for web artisans',
    icon: '🐘',
    framework: 'php',
    repo_url: 'https://github.com/laravel/laravel',
    branch: 'master',
    build_command: 'composer install',
    start_command: 'php artisan serve --host 0.0.0.0 --port 8000',
    port: 8000,
  },
  {
    name: 'C++ CMake',
    description: 'Native C++ application with CMake build system',
    icon: '⚙️',
    framework: 'cpp',
    repo_url: 'https://github.com/cpp-best-practices/cmake_template',
    branch: 'main',
    build_command: 'cmake . && make',
    start_command: './app',
    port: 8080,
  },
  {
    name: '.NET Web App',
    description: 'Enterprise-grade C# web application',
    icon: '🔷',
    framework: 'csharp',
    repo_url: 'https://github.com/dotnet/dotnet-docker/tree/main/samples/aspnetapp',
    branch: 'main',
    build_command: 'dotnet build',
    start_command: 'dotnet run',
    port: 5000,
  },
]

export default function NewProjectPage() {
  const router = useRouter()
  const queryClient = useQueryClient()
  const [loading, setLoading] = useState(false)
  const [showToken, setShowToken] = useState(false)
  const [activeTab, setActiveTab] = useState<'template' | 'manual'>('template')
  const [form, setForm] = useState({
    name: '',
    type: 'cd' as 'cd' | 'registry',
    repo_url: '',
    branch: 'main',
    build_command: '',
    start_command: '',
    port: 3000,
    framework: '',
    is_private: false,
    git_token: '',
  })

  const applyTemplate = (t: Template) => {
    setForm((f) => ({
      ...f,
      framework: t.framework,
      branch: t.branch,
      build_command: t.build_command,
      start_command: t.start_command,
      port: t.port,
      repo_url: t.repo_url,
    }))
    setActiveTab('manual')
    toast.success(`Template "${t.name}" applied — update the repo URL to your fork`)
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      await projectsApi.create(form)
      toast.success('Project created!')
      queryClient.invalidateQueries({ queryKey: ['projects'] })
      router.push('/dashboard/projects')
    } catch (err: unknown) {
      const error = err as { response?: { data?: { error?: string } } }
      toast.error(error?.response?.data?.error || 'Failed to create project')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="flex flex-col min-h-screen">
      <Header title="New Project" subtitle="Connect a Git repository to deploy" />
      <div className="p-6 max-w-2xl">
        {/* Tabs */}
        <div className="flex gap-1 mb-4 bg-surface-elevated border border-surface-border rounded-lg p-1">
          {(['template', 'manual'] as const).map((tab) => (
            <button
              key={tab}
              type="button"
              onClick={() => setActiveTab(tab)}
              className={`flex-1 py-2 px-4 text-sm font-medium rounded-md transition-all ${
                activeTab === tab
                  ? 'bg-brand-600 text-white'
                  : 'text-slate-400 hover:text-slate-200'
              }`}
            >
              {tab === 'template' ? (
                <span className="flex items-center justify-center gap-2"><Zap size={13} /> Use Template</span>
              ) : (
                <span className="flex items-center justify-center gap-2"><GitBranch size={13} /> Manual Setup</span>
              )}
            </button>
          ))}
        </div>

        {/* Template picker */}
        {activeTab === 'template' && (
          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-2">
            {TEMPLATES.map((t) => (
              <button
                key={t.name}
                type="button"
                onClick={() => applyTemplate(t)}
                className="card text-left hover:border-brand-500 transition-colors group"
              >
                <div className="flex items-start gap-3">
                  <span className="text-2xl shrink-0">{t.icon}</span>
                  <div className="min-w-0">
                    <div className="text-sm font-semibold text-slate-200 group-hover:text-brand-300 transition-colors">{t.name}</div>
                    <div className="text-xs text-slate-500 mt-0.5">{t.description}</div>
                    <span className="inline-block mt-1.5 text-xs px-2 py-0.5 rounded-full bg-brand-600/20 text-brand-300 font-mono">{t.framework || 'auto'}</span>
                  </div>
                </div>
              </button>
            ))}
          </div>
        )}

        {/* Manual form */}
        {activeTab === 'manual' && (
        <div className="card">
          <form onSubmit={handleSubmit} className="space-y-5">
            {/* Name */}
            <div>
              <label className="label">Project Name</label>
              <input
                type="text"
                className="input"
                placeholder="my-awesome-app"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
                required
                minLength={2}
                maxLength={64}
              />
            </div>

            {/* Project Type */}
            <div className="grid grid-cols-2 gap-4">
              <button
                type="button"
                onClick={() => setForm({ ...form, type: 'cd' })}
                className={`flex flex-col items-center gap-3 p-4 rounded-xl border transition-all ${
                  form.type === 'cd' 
                    ? 'bg-brand-500/10 border-brand-500 ring-1 ring-brand-500' 
                    : 'bg-slate-900/50 border-slate-800 hover:border-slate-700'
                }`}
              >
                <div className={`p-2 rounded-lg ${form.type === 'cd' ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-400'}`}>
                  <Rocket size={20} />
                </div>
                <div className="text-center">
                  <div className={`text-sm font-semibold ${form.type === 'cd' ? 'text-white' : 'text-slate-300'}`}>CD Project</div>
                  <div className="text-[10px] text-slate-500 mt-0.5">Build & Deploy Apps</div>
                </div>
              </button>

              <button
                type="button"
                onClick={() => setForm({ ...form, type: 'registry' })}
                className={`flex flex-col items-center gap-3 p-4 rounded-xl border transition-all ${
                  form.type === 'registry' 
                    ? 'bg-brand-500/10 border-brand-500 ring-1 ring-brand-500' 
                    : 'bg-slate-900/50 border-slate-800 hover:border-slate-700'
                }`}
              >
                <div className={`p-2 rounded-lg ${form.type === 'registry' ? 'bg-brand-500 text-white' : 'bg-slate-800 text-slate-400'}`}>
                  <Database size={20} />
                </div>
                <div className="text-center">
                  <div className={`text-sm font-semibold ${form.type === 'registry' ? 'text-white' : 'text-slate-300'}`}>Registry</div>
                  <div className="text-[10px] text-slate-500 mt-0.5">Store Images & Charts</div>
                </div>
              </button>
            </div>

            {/* Repository URL (Optional for Registry) */}
            <div>
              <label className="label">
                <Globe size={13} className="inline mr-1.5" />
                Git Repository URL {form.type === 'registry' && <span className="text-slate-600 font-normal ml-1">(Optional)</span>}
              </label>
              <input
                type="url"
                className="input"
                placeholder="https://github.com/user/repo"
                value={form.repo_url}
                onChange={(e) => setForm({ ...form, repo_url: e.target.value })}
                required={form.type === 'cd'}
              />
            </div>

            {/* Private repo toggle */}
            <div className="flex items-center gap-3 p-3 rounded-lg border border-surface-border bg-surface-elevated">
              <Lock size={15} className="text-slate-500 shrink-0" />
              <div className="flex-1">
                <div className="text-sm font-medium text-slate-300">Private Repository</div>
                <div className="text-xs text-slate-500">Enable to provide a Personal Access Token</div>
              </div>
              <button
                type="button"
                onClick={() => setForm({ ...form, is_private: !form.is_private, git_token: form.is_private ? '' : form.git_token })}
                className={`relative inline-flex h-6 w-11 items-center rounded-full transition-colors ${form.is_private ? 'bg-brand-600' : 'bg-slate-700'}`}
              >
                <span className={`inline-block h-4 w-4 transform rounded-full bg-white transition-transform ${form.is_private ? 'translate-x-6' : 'translate-x-1'}`} />
              </button>
            </div>

            {/* PAT field (only when private) */}
            {form.is_private && (
              <div>
                <label className="label">
                  <Lock size={13} className="inline mr-1.5" />
                  Personal Access Token (PAT)
                </label>
                <div className="relative">
                  <input
                    type={showToken ? 'text' : 'password'}
                    className="input pr-10"
                    placeholder="ghp_xxxxxxxxxxxxxxxxxxxx"
                    value={form.git_token}
                    onChange={(e) => setForm({ ...form, git_token: e.target.value })}
                    autoComplete="off"
                  />
                  <button
                    type="button"
                    onClick={() => setShowToken(!showToken)}
                    className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                  >
                    {showToken ? <EyeOff size={14} /> : <Eye size={14} />}
                  </button>
                </div>
                <p className="text-xs text-slate-600 mt-1">
                  GitHub: Settings {'->'} Developer settings {'->'} Personal access tokens {'->'} Grant <code className="text-slate-400">repo</code> scope.
                  The token is stored securely and never shown again.
                </p>
              </div>
            )}

            {/* Advanced fields (Hidden for simple Registry) */}
            {form.type === 'cd' && (
              <>
                {/* Branch */}
                <div>
                  <label className="label">
                    <GitBranch size={13} className="inline mr-1.5" />
                    Branch
                  </label>
                  <input
                    type="text"
                    className="input"
                    placeholder="main"
                    value={form.branch}
                    onChange={(e) => setForm({ ...form, branch: e.target.value })}
                  />
                </div>

                {/* Framework */}
                <div>
                  <label className="label">Framework / Runtime</label>
                  <Select
                    value={form.framework}
                    onChange={(v) => setForm({ ...form, framework: v })}
                    options={FRAMEWORKS}
                    placeholder="Auto-detect"
                  />
                </div>

                {/* Build & Start commands */}
                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="label">
                      <Terminal size={13} className="inline mr-1.5" />
                      Build Command
                    </label>
                    <input
                      type="text"
                      className="input"
                      placeholder="npm run build"
                      value={form.build_command}
                      onChange={(e) => setForm({ ...form, build_command: e.target.value })}
                    />
                  </div>
                  <div>
                    <label className="label">Start Command</label>
                    <input
                      type="text"
                      className="input"
                      placeholder="npm start"
                      value={form.start_command}
                      onChange={(e) => setForm({ ...form, start_command: e.target.value })}
                    />
                  </div>
                </div>

                {/* Port */}
                <div>
                  <label className="label">Port</label>
                  <input
                    type="number"
                    className="input"
                    placeholder="3000"
                    value={form.port}
                    onChange={(e) => setForm({ ...form, port: parseInt(e.target.value) || 3000 })}
                    min={1}
                    max={65535}
                  />
                </div>
              </>
            )}

            <div className="flex gap-3 pt-2">
              <button
                type="button"
                onClick={() => router.back()}
                className="btn-secondary"
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={loading}
                className="btn-primary"
              >
                {loading ? (
                  <>
                    <Loader2 size={15} className="animate-spin" />
                    Creating...
                  </>
                ) : (
                  'Create Project'
                )}
              </button>
            </div>
          </form>
        </div>
        )}
      </div>
    </div>
  )
}

