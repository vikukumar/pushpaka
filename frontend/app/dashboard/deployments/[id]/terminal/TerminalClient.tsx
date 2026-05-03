'use client'

import { useEffect, useRef } from 'react'
import { useAuthStore } from '@/lib/auth'
import { API_URL } from '@/lib/api'

interface Props {
  deploymentId: string
}

export default function TerminalClient({ deploymentId }: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<import('@xterm/xterm').Terminal | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const { token } = useAuthStore()

  useEffect(() => {
    if (!containerRef.current) return
    if (termRef.current) return // already mounted

    let term: import('@xterm/xterm').Terminal
    let fitAddon: import('@xterm/addon-fit').FitAddon

    async function init() {
      const { Terminal } = await import('@xterm/xterm')
      const { FitAddon } = await import('@xterm/addon-fit')
      const { WebLinksAddon } = await import('@xterm/addon-web-links')

      // Import xterm CSS dynamically (only once)
      if (!document.getElementById('xterm-css')) {
        const link = document.createElement('link')
        link.id = 'xterm-css'
        link.rel = 'stylesheet'
        link.href = '/_next/static/chunks/node_modules/@xterm/xterm/css/xterm.css'
        document.head.appendChild(link)
      }

      term = new Terminal({
        cursorBlink: true,
        fontSize: 13,
        fontFamily: '"JetBrains Mono", "Fira Code", Menlo, monospace',
        theme: {
          background: '#0d0f14',
          foreground: '#e2e8f0',
          cursor: '#818cf8',
          selectionBackground: 'rgba(99,102,241,0.3)',
          black: '#1e293b',
          red: '#f87171',
          green: '#4ade80',
          yellow: '#fbbf24',
          blue: '#818cf8',
          magenta: '#c084fc',
          cyan: '#22d3ee',
          white: '#e2e8f0',
          brightBlack: '#475569',
          brightRed: '#fca5a5',
          brightGreen: '#86efac',
          brightYellow: '#fde68a',
          brightBlue: '#a5b4fc',
          brightMagenta: '#d8b4fe',
          brightCyan: '#67e8f9',
          brightWhite: '#f8fafc',
        },
      })

      fitAddon = new FitAddon()
      term.loadAddon(fitAddon)
      term.loadAddon(new WebLinksAddon())
      term.open(containerRef.current!)
      fitAddon.fit()
      termRef.current = term

      term.writeln('\x1b[1;34mConnecting to container…\x1b[0m')

      // Build WebSocket URL
      const wsBase = API_URL.replace(/^http/, 'ws')
      const wsUrl = `${wsBase}/api/v1/deployments/${deploymentId}/terminal?token=${token}`
      const ws = new WebSocket(wsUrl)
      wsRef.current = ws

      ws.onopen = () => {
        term.writeln('\x1b[1;32mConnected\x1b[0m\r\n')
        // Send initial terminal size
        ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }))
      }

      ws.onmessage = (e) => {
        if (typeof e.data === 'string') {
          term.write(e.data)
        } else {
          e.data.text().then((t: string) => term.write(t))
        }
      }

      ws.onerror = () => {
        term.writeln('\r\n\x1b[1;31mWebSocket error — is the container running?\x1b[0m')
      }

      ws.onclose = (e) => {
        term.writeln(`\r\n\x1b[33mConnection closed (${e.code})\x1b[0m`)
      }

      term.onData((data) => {
        if (ws.readyState === WebSocket.OPEN) ws.send(data)
      })

      term.onResize(({ cols, rows }) => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'resize', cols, rows }))
        }
      })

      const handleResize = () => fitAddon.fit()
      window.addEventListener('resize', handleResize)

      return () => {
        window.removeEventListener('resize', handleResize)
      }
    }

    const cleanup = init()

    return () => {
      cleanup.then((fn) => fn?.())
      wsRef.current?.close()
      termRef.current?.dispose()
      termRef.current = null
      wsRef.current = null
    }
  }, [deploymentId, token])

  return (
    <div
      ref={containerRef}
      className="w-full h-full"
      style={{ minHeight: '400px' }}
    />
  )
}
