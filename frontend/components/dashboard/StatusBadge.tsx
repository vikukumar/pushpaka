'use client'

import { cn } from '@/lib/utils'
import { DeploymentStatus } from '@/types'

interface StatusBadgeProps {
  status: DeploymentStatus | string
  className?: string
  showDot?: boolean
}

const PULSE_STATUSES = new Set(['building', 'queued'])

export function StatusBadge({ status, className, showDot = true }: StatusBadgeProps) {
  const badgeClass = `badge-base badge-${status}`

  return (
    <span className={cn(badgeClass, className)}>
      {showDot && (
        <span
          className={cn(
            'inline-block w-1.5 h-1.5 rounded-full',
            PULSE_STATUSES.has(status) && 'animate-pulse'
          )}
          style={{ background: 'currentColor' }}
          aria-hidden="true"
        />
      )}
      {status}
    </span>
  )
}
