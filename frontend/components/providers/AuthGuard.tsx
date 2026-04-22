'use client'

import { useEffect } from 'react'
import { useRouter, usePathname } from 'next/navigation'
import { useAuthStore } from '@/lib/auth'

interface AuthGuardProps {
  children: React.ReactNode
}

// PUBLIC_PATHS are accessible without authentication
const PUBLIC_PATHS = ['/login', '/register']

export function AuthGuard({ children }: AuthGuardProps) {
  const router   = useRouter()
  const pathname = usePathname()
  const { isAuthenticated, _hasHydrated } = useAuthStore()

  useEffect(() => {
    if (!_hasHydrated) return // Wait until Zustand has rehydrated from localStorage

    const isPublic = PUBLIC_PATHS.some(p => pathname.startsWith(p))

    if (!isAuthenticated && !isPublic) {
      router.replace('/login')
    }
  }, [isAuthenticated, _hasHydrated, pathname, router])

  // While Zustand is hydrating, render nothing to avoid flash
  if (!_hasHydrated) {
    return (
      <div className="min-h-screen bg-[var(--bg-base)] flex items-center justify-center">
        <div className="flex flex-col items-center gap-3">
          <div className="w-8 h-8 rounded-full border-2 border-indigo-500 border-t-transparent animate-spin" />
          <p className="text-sm text-slate-500">Loading…</p>
        </div>
      </div>
    )
  }

  return <>{children}</>
}
