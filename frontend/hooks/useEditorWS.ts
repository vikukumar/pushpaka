'use client'

import { useEffect, useRef, useCallback } from 'react'
import { useAuthStore } from '@/lib/auth'
import { API_URL } from '@/lib/api'

export interface WSMessage {
  type: string
  workspace: string
  path?: string
  content?: string
  user?: string
  cursor?: any
}

export function useEditorWS(workspaceId: string, onMessage: (msg: WSMessage) => void) {
  const ws = useRef<WebSocket | null>(null)
  const reconnectTimer = useRef<NodeJS.Timeout | null>(null)
  const { token } = useAuthStore()
  const onMessageRef = useRef(onMessage)

  useEffect(() => {
    onMessageRef.current = onMessage
  }, [onMessage])

  const connect = useCallback(() => {
    if (typeof window === 'undefined' || !token) return
    
    // Safety check: don't even try if token is obviously expired
    try {
      const payload = JSON.parse(atob(token.split('.')[1]))
      if (payload.exp * 1000 < Date.now()) {
        console.warn('[WS] Token expired, skipping connection')
        return
      }
    } catch {
      return
    }

    // Cleanup existing connection before creating a new one
    if (ws.current) {
      ws.current.close()
    }

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const apiUrl = API_URL
    const host = apiUrl.replace(/^https?:\/\//, '') || window.location.host
    const url = `${protocol}//${host}/api/v1/editor/ws`
    
    console.log(`[WS] Connecting to ${url}`)
    ws.current = new WebSocket(`${url}?token=${token}`)

    ws.current.onopen = () => {
      console.log('[WS] Connected to Editor Sync')
      ws.current?.send(JSON.stringify({ type: 'join', workspace: workspaceId }))
    }

    ws.current.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        onMessageRef.current(msg)
      } catch (e) {
        console.error('[WS] Failed to parse message', e)
      }
    }

    ws.current.onclose = (event) => {
      console.log(`[WS] Disconnected (code: ${event.code}). Reconnecting in 3s...`)
      if (token && !event.wasClean) {
        reconnectTimer.current = setTimeout(connect, 3000)
      }
    }

    ws.current.onerror = (err) => {
      console.warn('[WS] Error', err)
      ws.current?.close()
    }
  }, [workspaceId, token]) // Removed onMessage from dependencies

  useEffect(() => {
    connect()
    return () => {
      if (reconnectTimer.current) clearTimeout(reconnectTimer.current)
      if (ws.current) {
        ws.current.onclose = null // Prevent reconnect loop on unmount
        ws.current.close()
      }
    }
  }, [connect])

  const sendMessage = useCallback((msg: Partial<WSMessage>) => {
    if (ws.current?.readyState === WebSocket.OPEN) {
      ws.current.send(JSON.stringify({ ...msg, workspace: workspaceId }))
    }
  }, [workspaceId])

  return { sendMessage }
}
