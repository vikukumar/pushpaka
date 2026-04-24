import type { Metadata } from 'next'
import { Inter, JetBrains_Mono } from 'next/font/google'
import { ThemeProvider } from '@/lib/theme'
import { Toaster } from 'react-hot-toast'
import { QueryProvider } from '@/components/providers/QueryProvider'
import { AuthGuard } from '@/components/providers/AuthGuard'
import './globals.css'

const inter = Inter({ subsets: ['latin'], variable: '--font-inter' })
const jetbrainsMono = JetBrains_Mono({
  subsets: ['latin'],
  variable: '--font-mono',
  weight: ['400', '500'],
})

export const metadata: Metadata = {
  title: {
    default: 'Pushpaka -- Carry your code to the cloud',
    template: '%s | Pushpaka',
  },
  description:
    'Pushpaka is a modern self-hosted cloud platform that allows developers to deploy applications directly from Git repositories.',
  keywords: ['deployment', 'cloud', 'self-hosted', 'git', 'devops', 'platform'],
  openGraph: {
    title: 'Pushpaka',
    description: 'Carry your code to the cloud effortlessly.',
    images: ['/og-image.svg'],
    type: 'website',
  },
  icons: {
    icon: '/favicon.svg',
  },
}

export default function RootLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return (
    <html lang="en" suppressHydrationWarning className="dark">
      <body className={`${inter.variable} ${jetbrainsMono.variable} font-sans antialiased`}>
        <ThemeProvider>
          <QueryProvider>
            <AuthGuard>
              {children}
            </AuthGuard>
            <Toaster
              position="top-right"
              toastOptions={{
                style: {
                  background: 'var(--toast-bg, #1e293b)',
                  color: 'var(--toast-color, #e2e8f0)',
                  border: '1px solid var(--toast-border, #334155)',
                },
              }}
            />
          </QueryProvider>
        </ThemeProvider>
      </body>
    </html>
  )
}
