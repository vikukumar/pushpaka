'use client'

import { useState, useEffect, Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { authApi, API_URL } from '@/lib/api'
import { useAuthStore } from '@/lib/auth'
import { AuthResponse } from '@/types'
import toast from 'react-hot-toast'
import { Eye, EyeOff, Loader2 } from 'lucide-react'

function LoginContent() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const { setAuth } = useAuthStore()
  const [form, setForm] = useState({ email: '', password: '' })
  const [showPassword, setShowPassword] = useState(false)
  const [loading, setLoading] = useState(false)

  // Handle OAuth callback: ?token=...&oauth=1
  useEffect(() => {
    const token = searchParams.get('token')
    const isOAuth = searchParams.get('oauth') === '1'
    const error = searchParams.get('error')

    if (error) {
      toast.error('OAuth login failed: ' + error)
      return
    }

    if (token && isOAuth) {
      // Decode the JWT to extract user claims embedded by the backend
      try {
        const parts = token.split('.')
        const payload = JSON.parse(atob(parts[1]))
        const user = {
          id:             payload.sub   || '',
          email:          payload.email || '',
          name:           payload.name  || 'User',
          role:           payload.role  || 'user',
          is_active:      true,
          avatar_url:     '',
          oauth_provider: '',
          created_at:     '',
          updated_at:     '',
        }
        setAuth(user, token)
        router.push('/dashboard')
      } catch {
        toast.error('Failed to process OAuth token')
      }
    }
  }, [searchParams, setAuth, router])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setLoading(true)
    try {
      const { data } = await authApi.login(form)
      const auth: AuthResponse = data
      setAuth(auth.user, auth.token)
      router.push('/dashboard')
    } catch (err: unknown) {
      const error = err as { response?: { data?: { error?: string } } }
      toast.error(error?.response?.data?.error || 'Login failed')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-surface flex items-center justify-center p-4">
      <div className="w-full max-w-md">
        {/* Logo */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center gap-3 mb-4">
            <div className="w-10 h-10 bg-brand-600 rounded-xl flex items-center justify-center">
              <svg viewBox="0 0 32 32" className="w-6 h-6" fill="none">
                <rect x="3" y="15" width="26" height="10" rx="5" fill="white" fillOpacity="0.9"/>
                <circle cx="8" cy="15" r="4" fill="white" fillOpacity="0.9"/>
                <circle cx="16" cy="12" r="5" fill="white" fillOpacity="0.9"/>
                <circle cx="24" cy="14" r="4" fill="white" fillOpacity="0.9"/>
                <path d="M3 18 Q0 16 1 20 Q0 23 3 22" fill="#22d3ee" opacity="0.9"/>
                <path d="M29 18 Q32 16 31 20 Q32 23 29 22" fill="#22d3ee" opacity="0.9"/>
              </svg>
            </div>
            <span className="text-2xl font-bold text-white">Pushpaka</span>
          </div>
          <p className="text-slate-400 text-sm">Welcome back -- sign into your account</p>
        </div>

        <div className="card">
          {/* OAuth buttons */}
          <div className="space-y-2 mb-4">
            <a
              href={`${API_URL}/api/v1/auth/github`}
              className="flex items-center justify-center gap-2 w-full py-2.5 px-4 rounded-lg border border-slate-700 bg-slate-800 hover:bg-slate-700 text-sm text-slate-200 transition-colors"
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                <path d="M12 0C5.37 0 0 5.37 0 12c0 5.31 3.435 9.795 8.205 11.385.6.105.825-.255.825-.57 0-.285-.015-1.23-.015-2.235-3.015.555-3.795-.735-4.035-1.41-.135-.345-.72-1.41-1.23-1.695-.42-.225-1.02-.78-.015-.795.945-.015 1.62.87 1.845 1.23 1.08 1.815 2.805 1.305 3.495.99.105-.78.42-1.305.765-1.605-2.67-.3-5.46-1.335-5.46-5.925 0-1.305.465-2.385 1.23-3.225-.12-.3-.54-1.53.12-3.18 0 0 1.005-.315 3.3 1.23.96-.27 1.98-.405 3-.405s2.04.135 3 .405c2.295-1.56 3.3-1.23 3.3-1.23.66 1.65.24 2.88.12 3.18.765.84 1.23 1.905 1.23 3.225 0 4.605-2.805 5.625-5.475 5.925.435.375.81 1.095.81 2.22 0 1.605-.015 2.895-.015 3.3 0 .315.225.69.825.57A12.02 12.02 0 0 0 24 12c0-6.63-5.37-12-12-12z"/>
              </svg>
              Continue with GitHub
            </a>
            <a
              href={`${API_URL}/api/v1/auth/gitlab`}
              className="flex items-center justify-center gap-2 w-full py-2.5 px-4 rounded-lg border border-slate-700 bg-slate-800 hover:bg-slate-700 text-sm text-slate-200 transition-colors"
            >
              <svg className="w-4 h-4" viewBox="0 0 24 24" fill="currentColor">
                <path d="M22.65 14.39L12 22.13 1.35 14.39a.84.84 0 0 1-.3-.94l1.22-3.78 2.44-7.51A.42.42 0 0 1 4.82 2a.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.49h8.1l2.44-7.49a.42.42 0 0 1 .11-.18.43.43 0 0 1 .58 0 .42.42 0 0 1 .11.18l2.44 7.51L23 13.45a.84.84 0 0 1-.35.94z"/>
              </svg>
              Continue with GitLab
            </a>
          </div>

          <div className="relative my-4">
            <div className="absolute inset-0 flex items-center">
              <div className="w-full border-t border-slate-700" />
            </div>
            <div className="relative flex justify-center text-xs">
              <span className="px-2 bg-card text-slate-500">or sign in with email</span>
            </div>
          </div>

          <form onSubmit={handleSubmit} className="space-y-4">
            <div>
              <label className="label">Email</label>
              <input
                type="email"
                className="input"
                placeholder="you@example.com"
                value={form.email}
                onChange={(e) => setForm({ ...form, email: e.target.value })}
                required
                autoComplete="email"
              />
            </div>

            <div>
              <label className="label">Password</label>
              <div className="relative">
                <input
                  type={showPassword ? 'text' : 'password'}
                  className="input pr-10"
                  placeholder=""
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                  required
                  autoComplete="current-password"
                />
                <button
                  type="button"
                  onClick={() => setShowPassword(!showPassword)}
                  className="absolute right-3 top-1/2 -translate-y-1/2 text-slate-500 hover:text-slate-300"
                >
                  {showPassword ? <EyeOff size={16} /> : <Eye size={16} />}
                </button>
              </div>
            </div>

            <button
              type="submit"
              disabled={loading}
              className="btn-primary w-full justify-center py-2.5"
            >
              {loading ? (
                <>
                  <Loader2 size={16} className="animate-spin" />
                  Signing in...
                </>
              ) : (
                'Sign in'
              )}
            </button>
          </form>

          <p className="text-center text-sm text-slate-500 mt-4">
            Don&apos;t have an account?{' '}
            <Link href="/register" className="text-brand-400 hover:text-brand-300 font-medium">
              Create one
            </Link>
          </p>
        </div>

        <p className="text-center text-xs text-slate-600 mt-6">
          Pushpaka v1.0.0 -- Self-hosted deployment platform 
        </p>
      </div>
    </div>
  )
}

export default function LoginPage() {
  return (
    <Suspense>
      <LoginContent />
    </Suspense>
  )
}

