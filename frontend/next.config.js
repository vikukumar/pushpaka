/** @type {import('next').NextConfig} */

// Static export is only used when building for the Go binary embedding.
// Set STATIC_EXPORT=1 to produce a fully static output that can be copied into
// backend/ui/dist and embedded in the Go binary.  During `next dev` and plain
// `pnpm build` the app runs as a standard Next.js server.
const isStaticExport = process.env.STATIC_EXPORT === '1'

const securityHeaders = [
  { key: 'X-DNS-Prefetch-Control', value: 'on' },
  { key: 'X-Frame-Options', value: 'SAMEORIGIN' },
  { key: 'X-Content-Type-Options', value: 'nosniff' },
  { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
  { key: 'Permissions-Policy', value: 'camera=(), microphone=(), geolocation=()' },
  {
    key: 'Content-Security-Policy',
    value: [
      "default-src 'self'",
      // Monaco editor and inline styles need unsafe-inline/unsafe-eval
      "script-src 'self' 'unsafe-inline' 'unsafe-eval' blob:",
      "style-src 'self' 'unsafe-inline'",
      "font-src 'self' data:",
      "img-src 'self' data: blob: https:",
      "connect-src 'self' ws: wss: https:",
      "worker-src blob:",
    ].join('; '),
  },
]

const nextConfig = {
  // Only apply static-export settings when explicitly requested.
  ...(isStaticExport ? { output: 'export', trailingSlash: true } : {}),
  images: {
    // unoptimized required for output: export; harmless in dev.
    unoptimized: true,
    remotePatterns: [
      { protocol: 'https', hostname: 'avatars.githubusercontent.com' },
    ],
  },
  env: {
    NEXT_PUBLIC_API_URL: process.env.NEXT_PUBLIC_API_URL,
    NEXT_PUBLIC_WS_URL: process.env.NEXT_PUBLIC_WS_URL,
  },
  // Security headers are only applied in server mode (not static export)
  ...(!isStaticExport ? {
    async headers() {
      return [{ source: '/(.*)', headers: securityHeaders }]
    },
  } : {}),
}

module.exports = nextConfig
