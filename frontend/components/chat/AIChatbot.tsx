'use client'

import { useState, useRef, useEffect, useCallback } from 'react'
import { aiApi } from '@/lib/api'
import { Sparkles, X, Send, Loader2, Bot, User, ChevronDown, Minimize2 } from 'lucide-react'
import DOMPurify from 'dompurify'

interface Message {
  role: 'user' | 'assistant' | 'tool'
  content: string
  error?: boolean
  tool_calls?: any[]
  tool_call_id?: string
}

interface PendingToolCall {
  tool_call_id: string
  tool_name: string
  args: Record<string, any>
}

interface AIChatbotProps {
  deploymentId?: string
  projectId?: string
}

const WELCOME = `Hi! I'm **Pushpaka Assistant** — your AI co-pilot for cloud deployments. ✨

I can help you with:
- 🔍 Diagnosing build failures and errors
- 🐳 Docker container configuration
- 🌿 Branch/webhook setup
- ⚙️ Environment variables & secrets
- 📊 Performance and log analysis

Ask me anything about your deployments!`

function renderMarkdown(text: string): string {
  // First escape HTML to prevent injection
  let escaped = text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
  
  // Apply markdown replacements on escaped text
  let html = escaped
    .replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
    .replace(/`([^`]+)`/g, '<code class="bg-white/10 px-1 py-0.5 rounded text-xs font-mono text-cyan-300">$1</code>')
    .replace(/```([\s\S]*?)```/g, (match, code) => {
      const sanitized = code.replace(/</g, '&lt;').replace(/>/g, '&gt;')
      return `<pre class="bg-black/40 rounded-lg p-3 text-xs font-mono text-green-300 overflow-x-auto my-2">${sanitized}</pre>`
    })
    .replace(/^- (.+)$/gm, '<li class="ml-3 list-disc">$1</li>')
    .replace(/^(#{1,3}) (.+)$/gm, '<div class="font-semibold text-white mt-2">$2</div>')
    .replace(/\n\n/g, '<div class="h-2"></div>')
    .replace(/\n/g, '<br/>')
    .replace(/🔍|🐳|🌿|⚙️|📊|✨/g, (m) => `<span>${m}</span>`)
  
  // Use DOMPurify to sanitize the final HTML with a restrictive configuration
  if (typeof window !== 'undefined') {
    return DOMPurify.sanitize(html, { 
      ALLOWED_TAGS: ['strong', 'code', 'pre', 'li', 'div', 'span', 'br', 'ul', 'ol', 'p'],
      ALLOWED_ATTR: ['class'],
      // Strictly forbid sensitive tags and attributes to prevent XSS bypass
      FORBID_TAGS: ['script', 'style', 'iframe', 'form', 'object', 'base', 'embed', 'link', 'meta', 'svg', 'math'],
      FORBID_ATTR: ['onclick', 'onerror', 'onmouseover', 'onload', 'onfocus', 'onblur', 'style', 'action', 'formaction'],
      // Enforce safe configuration
      USE_PROFILES: { html: true },
      SANITIZE_DOM: true,
      KEEP_CONTENT: false,
    })
  }
  return html
}

export default function AIChatbot({ deploymentId, projectId }: AIChatbotProps) {
  const [open, setOpen] = useState(false)
  const [minimized, setMinimized] = useState(false)
  const [messages, setMessages] = useState<Message[]>([
    { role: 'assistant', content: WELCOME },
  ])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [pulse, setPulse] = useState(false)
  const [pendingCall, setPendingCall] = useState<PendingToolCall | null>(null)
  const [autonomous, setAutonomous] = useState(false)
  const bottomRef = useRef<HTMLDivElement>(null)
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    aiApi.getConfig().then(res => {
      if (res.data?.autonomous_agent) {
        setAutonomous(true)
      }
    }).catch(() => {})
  }, [])

  useEffect(() => {
    if (open && !minimized) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
      setTimeout(() => inputRef.current?.focus(), 100)
    }
  }, [messages, open, minimized, pendingCall])

  // Subtle pulse on the button when closed to draw attention
  useEffect(() => {
    if (!open) {
      const t = setInterval(() => setPulse((v) => !v), 4000)
      return () => clearInterval(t)
    }
  }, [open])

  const send = useCallback(async () => {
    const msg = input.trim()
    if (!msg || loading) return

    setInput('')
    const newMessages: Message[] = [...messages, { role: 'user', content: msg }]
    setMessages(newMessages)
    setLoading(true)
    setPendingCall(null)

    try {
      const res = await aiApi.agentChat({
        messages: newMessages,
        project_id: projectId,
        autonomous: autonomous,
      })
      
      const { reply, pending_tool_call, messages: updatedMessages } = res.data
      
      if (updatedMessages) {
        setMessages(updatedMessages)
      } else if (reply) {
        setMessages((prev) => [...prev, { role: 'assistant', content: reply }])
      }
      
      if (pending_tool_call) {
        setPendingCall(pending_tool_call)
      }
    } catch (e: unknown) {
      const err = e as { response?: { data?: { error?: string } } }
      const errMsg = err?.response?.data?.error || 'AI service unavailable.'
      setMessages((prev) => [...prev, {
        role: 'assistant',
        content: errMsg,
        error: true,
      }])
    } finally {
      setLoading(false)
    }
  }, [input, loading, projectId, autonomous, messages])

  const executeTool = async () => {
    if (!pendingCall || loading) return
    setLoading(true)
    const callToExecute = pendingCall
    setPendingCall(null)

    try {
      // Simulate adding the tool call approval object structure that OpenAI expects
      const res = await aiApi.agentExecute({
        messages,
        project_id: projectId,
        autonomous: autonomous,
        approved_tool_call: {
          id: callToExecute.tool_call_id,
          type: 'function',
          function: {
            name: callToExecute.tool_name,
            arguments: JSON.stringify(callToExecute.args)
          }
        }
      })
      
      const { reply, pending_tool_call, messages: updatedMessages } = res.data
      
      if (updatedMessages) {
        setMessages(updatedMessages)
      } else if (reply) {
        setMessages((prev) => [...prev, { role: 'assistant', content: reply }])
      }
      
      if (pending_tool_call) {
        setPendingCall(pending_tool_call)
      }
    } catch (e: unknown) {
      const err = e as { response?: { data?: { error?: string } } }
      const errMsg = err?.response?.data?.error || 'Tool execution failed.'
      setMessages((prev) => [...prev, {
        role: 'assistant',
        content: errMsg,
        error: true,
      }])
    } finally {
      setLoading(false)
    }
  }

  const cancelTool = () => {
    setPendingCall(null)
    setMessages((prev) => [...prev, {
      role: 'assistant',
      content: 'I cancelled the operation.',
    }])
  }

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  const quickActions = deploymentId
    ? ['Why did my build fail?', 'Show key errors', 'How to fix this?']
    : ['How do I set up webhooks?', 'How to configure resource limits?', 'Best practices for Dockerfile?']

  return (
    <>
      {/* Floating toggle button */}
      <button
        onClick={() => { setOpen((v) => !v); setMinimized(false) }}
        className="fixed bottom-6 right-6 z-50 w-14 h-14 rounded-full flex items-center justify-center shadow-2xl transition-all duration-300 hover:scale-110 active:scale-95"
        style={{
          background: open
            ? 'linear-gradient(135deg, #4f46e5, #7c3aed)'
            : 'linear-gradient(135deg, #4338ca, #6366f1, #818cf8)',
          boxShadow: open
            ? '0 8px 32px rgba(99,102,241,0.6), 0 0 0 1px rgba(255,255,255,0.1)'
            : pulse
            ? '0 8px 32px rgba(99,102,241,0.8), 0 0 40px rgba(99,102,241,0.4)'
            : '0 8px 32px rgba(99,102,241,0.5)',
        }}
        aria-label={open ? 'Close AI Assistant' : 'Open AI Assistant'}
      >
        {open
          ? <X size={22} className="text-white" />
          : <Sparkles size={22} className="text-white" />}

        {/* Unread indicator */}
        {!open && (
          <span
            className="absolute -top-0.5 -right-0.5 w-4 h-4 rounded-full bg-cyan-400 border-2 border-slate-900 text-[9px] font-bold flex items-center justify-center text-slate-900"
          >
            AI
          </span>
        )}
      </button>

      {/* Chat window */}
      {open && (
        <div
          className="fixed bottom-24 right-4 sm:right-6 z-50 flex flex-col rounded-2xl overflow-hidden shadow-2xl transition-all duration-300"
          style={{
            width: 'min(calc(100vw - 32px), 380px)',
            height: minimized ? '56px' : 'min(calc(100vh - 140px), 560px)',
            background: 'linear-gradient(175deg, #0e1c32 0%, #0b1524 100%)',
            border: '1px solid rgba(99,102,241,0.3)',
            boxShadow: '0 24px 80px rgba(0,0,0,0.7), 0 0 0 1px rgba(99,102,241,0.1)',
          }}
        >
          {/* Header */}
          <div
            className="flex items-center gap-3 px-4 py-3 shrink-0 cursor-pointer"
            style={{
              background: 'linear-gradient(90deg, rgba(99,102,241,0.18) 0%, rgba(124,58,237,0.12) 100%)',
              borderBottom: minimized ? 'none' : '1px solid rgba(99,102,241,0.2)',
            }}
            onClick={() => setMinimized((v) => !v)}
          >
            <div
              className="w-8 h-8 rounded-full flex items-center justify-center shrink-0"
              style={{
                background: 'linear-gradient(135deg, #4338ca, #6366f1)',
                boxShadow: '0 0 12px rgba(99,102,241,0.6)',
              }}
            >
              <Sparkles size={14} className="text-white" />
            </div>
            <div className="flex-1 min-w-0">
              <div className="text-sm font-semibold text-white">Pushpaka Assistant</div>
              <div className="text-[10px] text-cyan-400 flex items-center gap-1">
                <span className="w-1.5 h-1.5 rounded-full bg-green-400 animate-pulse" />
                AI-powered · Always on
              </div>
            </div>
            <div className="flex items-center gap-1">
              {minimized
                ? <ChevronDown size={14} className="text-slate-400 rotate-180" />
                : <Minimize2 size={14} className="text-slate-400" />}
            </div>
          </div>

          {!minimized && (
            <>
              {/* Messages */}
              <div className="flex-1 overflow-y-auto p-4 space-y-4">
                {messages.filter(m => m.role !== 'tool' && !m.tool_calls).map((msg, i) => (
                  <div
                    key={i}
                    className={`flex gap-2.5 ${msg.role === 'user' ? 'flex-row-reverse' : ''}`}
                  >
                    {/* Avatar */}
                    <div
                      className="w-7 h-7 rounded-full flex items-center justify-center shrink-0 mt-0.5"
                      style={{
                        background: msg.role === 'user'
                          ? 'linear-gradient(135deg, #0ea5e9, #06b6d4)'
                          : 'linear-gradient(135deg, #4338ca, #6366f1)',
                      }}
                    >
                      {msg.role === 'user'
                        ? <User size={12} className="text-white" />
                        : <Bot size={12} className="text-white" />}
                    </div>

                    {/* Bubble */}
                    <div
                      className={`max-w-[85%] rounded-2xl px-3 py-2 text-sm leading-relaxed ${
                        msg.role === 'user'
                          ? 'rounded-tr-sm'
                          : 'rounded-tl-sm'
                      } ${msg.error ? 'border border-red-500/30' : ''}`}
                      style={{
                        background: msg.role === 'user'
                          ? 'linear-gradient(135deg, rgba(14,165,233,0.25), rgba(6,182,212,0.2))'
                          : msg.error
                          ? 'rgba(239,68,68,0.1)'
                          : 'rgba(255,255,255,0.05)',
                        border: msg.role === 'user'
                          ? '1px solid rgba(14,165,233,0.3)'
                          : msg.error
                          ? '1px solid rgba(239,68,68,0.3)'
                          : '1px solid rgba(255,255,255,0.08)',
                        color: msg.error ? '#fca5a5' : 'var(--text-primary)',
                      }}
                    >
                      <div
                        dangerouslySetInnerHTML={{ __html: renderMarkdown(msg.content) }}
                      />
                    </div>
                  </div>
                ))}
                
                {pendingCall && !loading && (
                  <div className="flex gap-2.5">
                    <div className="w-7 h-7 rounded-full flex items-center justify-center shrink-0" style={{ background: 'linear-gradient(135deg, #4338ca, #6366f1)' }}>
                      <Bot size={12} className="text-white" />
                    </div>
                    <div className="rounded-2xl rounded-tl-sm px-4 py-3 border border-orange-500/30 bg-orange-500/10">
                      <p className="text-sm text-orange-200 mb-2">
                        I need your permission to run <code className="bg-black/20 px-1 py-0.5 rounded text-xs">{pendingCall.tool_name}</code>
                      </p>
                      <pre className="text-xs text-orange-300 font-mono bg-black/40 p-2 rounded mb-3">
                        {JSON.stringify(pendingCall.args, null, 2)}
                      </pre>
                      <div className="flex gap-2">
                        <button onClick={executeTool} className="btn-primary text-xs py-1.5 flex-1">Approve</button>
                        <button onClick={cancelTool} className="btn-secondary text-xs py-1.5 flex-1">Deny</button>
                      </div>
                    </div>
                  </div>
                )}

                {loading && (
                  <div className="flex gap-2.5">
                    <div
                      className="w-7 h-7 rounded-full flex items-center justify-center shrink-0"
                      style={{ background: 'linear-gradient(135deg, #4338ca, #6366f1)' }}
                    >
                      <Bot size={12} className="text-white" />
                    </div>
                    <div
                      className="rounded-2xl rounded-tl-sm px-4 py-3 flex items-center gap-2"
                      style={{ background: 'rgba(255,255,255,0.05)', border: '1px solid rgba(255,255,255,0.08)' }}
                    >
                      <Loader2 size={12} className="animate-spin text-brand-400" />
                      <span className="text-xs text-slate-400">Thinking…</span>
                    </div>
                  </div>
                )}
                <div ref={bottomRef} />
              </div>

              {/* Quick actions */}
              {messages.length === 1 && !loading && (
                <div className="px-4 pb-2 flex flex-wrap gap-1.5">
                  {quickActions.map((q) => (
                    <button
                      key={q}
                      onClick={() => { setInput(q); setTimeout(() => inputRef.current?.focus(), 50) }}
                      className="text-xs px-2.5 py-1 rounded-full transition-colors hover:text-white"
                      style={{
                        background: 'rgba(99,102,241,0.12)',
                        border: '1px solid rgba(99,102,241,0.25)',
                        color: '#a5b4fc',
                      }}
                    >
                      {q}
                    </button>
                  ))}
                </div>
              )}

              {/* Input */}
              <div
                className="shrink-0 p-3"
                style={{ borderTop: '1px solid rgba(255,255,255,0.07)' }}
              >
                <div
                  className="flex items-center gap-2 rounded-xl px-3 py-2"
                  style={{
                    background: 'rgba(255,255,255,0.04)',
                    border: '1px solid rgba(99,102,241,0.25)',
                  }}
                >
                  <input
                    ref={inputRef}
                    type="text"
                    value={input}
                    onChange={(e) => setInput(e.target.value)}
                    onKeyDown={handleKey}
                    placeholder="Ask anything about your deployment…"
                    className="flex-1 bg-transparent text-sm text-white placeholder-slate-600 outline-none"
                    disabled={loading}
                  />
                  <button
                    onClick={send}
                    disabled={loading || !input.trim()}
                    className="w-7 h-7 rounded-lg flex items-center justify-center transition-all disabled:opacity-40 disabled:cursor-not-allowed hover:scale-110"
                    style={{
                      background: 'linear-gradient(135deg, #4338ca, #6366f1)',
                      boxShadow: '0 2px 8px rgba(99,102,241,0.4)',
                    }}
                  >
                    <Send size={12} className="text-white" />
                  </button>
                </div>
                <p className="text-[10px] text-slate-700 text-center mt-1.5">
                  Powered by AI · Set AI_API_KEY to enable
                </p>
              </div>
            </>
          )}
        </div>
      )}
    </>
  )
}
