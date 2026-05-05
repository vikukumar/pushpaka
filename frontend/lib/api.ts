import axios from 'axios'
import { Project, Deployment, EnvVar, ApiResponse, ProjectTask, DeploymentLog } from '@/types'

export const API_URL = process.env.NEXT_PUBLIC_API_URL || ''

export const apiClient = axios.create({
  baseURL: `${API_URL}/api/v1`,
  timeout: 15000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Attach token to every request
apiClient.interceptors.request.use((config) => {
  if (typeof window !== 'undefined') {
    try {
      const raw = localStorage.getItem('pushpaka_auth')
      if (raw) {
        const { state } = JSON.parse(raw)
        if (state?.token) {
          config.headers.Authorization = `Bearer ${state.token}`
        }
      }
    } catch {
      // ignore malformed storage
    }
  }
  return config
})

// Handle 401 - redirect to login
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && typeof window !== 'undefined') {
      localStorage.removeItem('pushpaka_auth')
      window.location.href = '/login'
    }
    return Promise.reject(error)
  }
)

// Auth
export const authApi = {
  register: (data: { email: string; name: string; password: string }) =>
    apiClient.post('/auth/register', data),
  login: (data: { email: string; password: string }) =>
    apiClient.post('/auth/login', data),
}

// Projects
export const projectsApi = {
  list: () => apiClient.get('/projects'),
  get: (id: string) => apiClient.get(`/projects/${id}`),
  create: (data: {
    name: string
    repo_url: string
    branch?: string
    install_command?: string
    build_command?: string
    start_command?: string
    run_dir?: string
    port?: number
    framework?: string
    is_private?: boolean
    git_token?: string
    auto_sync_enabled?: boolean
    sync_interval_secs?: number
  }) => apiClient.post('/projects', data),
  update: (id: string, data: {
    name?: string
    repo_url?: string
    branch?: string
    install_command?: string
    build_command?: string
    start_command?: string
    run_dir?: string
    port?: number
    framework?: string
    is_private?: boolean
    git_token?: string
    cpu_limit?: string
    memory_limit?: string
    restart_policy?: string
    auto_sync_enabled?: boolean
    sync_interval_secs?: number
  }) => apiClient.put(`/projects/${id}`, data),
  delete: (id: string) => apiClient.delete(`/projects/${id}`),
  sync: (id: string) => apiClient.post(`/projects/${id}/sync`, {}),
}

// Tasks
export const tasksApi = {
  list: (projectId: string) => apiClient.get<ApiResponse<ProjectTask[]>>(`/tasks?project_id=${projectId}`),
  get: (id: string) => apiClient.get<ApiResponse<ProjectTask>>(`/tasks/${id}`),
  restart: (id: string) => apiClient.post(`/tasks/${id}/restart`, {}),
}

// Deployments
export const deploymentsApi = {
  list: (limit = 20, offset = 0, projectId?: string) => {
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) })
    if (projectId) params.set('project_id', projectId)
    return apiClient.get(`/deployments?${params}`)
  },
  stats: () => apiClient.get('/deployments/stats'),
  get: (id: string) => apiClient.get(`/deployments/${id}`),
  trigger: (data: { project_id: string; branch?: string; commit_sha?: string }) =>
    apiClient.post('/deployments', data),
  rollback: (id: string) => apiClient.post(`/deployments/${id}/rollback`),
  restart: (id: string) => apiClient.post(`/deployments/${id}/restart`),
  promote: (id: string) => apiClient.patch(`/deployments/${id}/promote`),
  delete: (id: string) => apiClient.delete(`/deployments/${id}`),
}

// Logs
export const logsApi = {
  get: (deploymentId: string) => apiClient.get<ApiResponse<DeploymentLog[]>>(`/logs/${deploymentId}`),
}

// Domains
export const domainsApi = {
  list: () => apiClient.get('/domains'),
  add: (data: { project_id: string; domain: string }) =>
    apiClient.post('/domains', data),
  delete: (id: string) => apiClient.delete(`/domains/${id}`),
}

// Env vars
export const envApi = {
  list: (projectId: string) => apiClient.get(`/env?project_id=${projectId}`),
  set: (data: { project_id: string; key: string; value: string }) =>
    apiClient.post('/env', data),
  delete: (data: { project_id: string; key: string }) =>
    apiClient.delete('/env', { data }),
}

// System capabilities (public  no auth required)
export const systemApi = {
  get: () => apiClient.get('/system'),
}

// Notifications
export const notificationsApi = {
  getConfig: () => apiClient.get('/notifications/config'),
  updateConfig: (data: {
    slack_webhook_url?: string
    discord_webhook_url?: string
    smtp_host?: string
    smtp_port?: number
    smtp_username?: string
    smtp_password?: string
    smtp_from?: string
    smtp_to?: string
    notify_on_success?: boolean
    notify_on_failure?: boolean
  }) => apiClient.put('/notifications/config', data),
}

// Webhooks
export const webhooksApi = {
  list: () => apiClient.get('/webhooks'),
  create: (data: { project_id: string; provider?: string; branch?: string }) =>
    apiClient.post('/webhooks', data),
  delete: (id: string) => apiClient.delete(`/webhooks/${id}`),
}

// Audit logs
export const auditApi = {
  list: (limit = 50, offset = 0) =>
    apiClient.get(`/audit?limit=${limit}&offset=${offset}`),
}

// AI log analysis
export const aiApi = {
  analyzeLogs: (deploymentId: string) =>
    apiClient.post(`/deployments/${deploymentId}/analyze`),
  chat: (message: string, deploymentId?: string) =>
    apiClient.post('/ai/chat', { message, deployment_id: deploymentId }),
  agentChat: (data: {
    messages: { role: string; content: string }[]
    project_id?: string
    autonomous: boolean
  }) => apiClient.post('/ai/agent', data),
  agentExecute: (data: {
    messages: { role: string; content: string }[]
    project_id?: string
    autonomous: boolean
    approved_tool_call: any
  }) => apiClient.post('/ai/agent/execute', data),
  getConfig: () => apiClient.get('/ai/config'),
  saveConfig: (data: {
    provider?: string
    api_key?: string
    model?: string
    base_url?: string
    system_prompt?: string
    monitoring_enabled?: boolean
    monitoring_interval?: number
    autonomous_agent?: boolean
  }) => apiClient.put('/ai/config', data),
  getUsage: () => apiClient.get('/ai/usage'),
}

// RAG knowledge base
export const ragApi = {
  list: () => apiClient.get('/ai/rag'),
  create: (data: { title: string; content: string }) => apiClient.post('/ai/rag', data),
  delete: (id: string) => apiClient.delete(`/ai/rag/${id}`),
}

// AI monitoring alerts
export const alertsApi = {
  list: (onlyUnresolved = false) =>
    apiClient.get(`/ai/alerts${onlyUnresolved ? '?unresolved=true' : ''}`),
  resolve: (id: string) => apiClient.put(`/ai/alerts/${id}/resolve`, {}),
}

// Container management
export const containerApi = {
  list: () => apiClient.get('/containers'),
  start: (id: string) => apiClient.post(`/containers/${id}/start`, {}),
  stop: (id: string) => apiClient.post(`/containers/${id}/stop`, {}),
  restart: (id: string) => apiClient.post(`/containers/${id}/restart`, {}),
  logs: (id: string, lines = 100) => apiClient.get(`/containers/${id}/logs?lines=${lines}`),
}

// Kubernetes management
export const k8sApi = {
  namespaces: () => apiClient.get('/k8s/namespaces'),
  pods: (namespace = 'default') => apiClient.get(`/k8s/pods?namespace=${namespace}`),
  deployments: (namespace = 'default') => apiClient.get(`/k8s/deployments?namespace=${namespace}`),
  services: (namespace = 'default') => apiClient.get(`/k8s/services?namespace=${namespace}`),
  rollout: (namespace: string, name: string) =>
    apiClient.post(`/k8s/deployments/${namespace}/${name}/rollout`, {}),
}

// In-browser code editor
export const filesApi = {
  list: (projectId: string) =>
    apiClient.get(`/projects/${projectId}/files`),
  read: (projectId: string, path: string) =>
    apiClient.get(`/projects/${projectId}/files${path}`),
  save: (projectId: string, path: string, content: string) =>
    apiClient.put(`/projects/${projectId}/files${path}`, { content }),
  createFile: (projectId: string, path: string) =>
    apiClient.post(`/projects/${projectId}/files${path}`, {}),
  createDirectory: (projectId: string, path: string) =>
    apiClient.post(`/projects/${projectId}/directories${path}`, {}),
  delete: (projectId: string, path: string) =>
    apiClient.delete(`/projects/${projectId}/files${path}`),
  rename: (projectId: string, path: string, newPath: string) =>
    apiClient.patch(`/projects/${projectId}/files${path}`, { newPath }),
  sync: (projectId: string) =>
    apiClient.post(`/projects/${projectId}/editor-sync`, {}),
}

// Git operations
export const gitApi = {
  status: (projectId: string) => apiClient.get(`/projects/${projectId}/git/status`),
  commit: (projectId: string, message: string) =>
    apiClient.post(`/projects/${projectId}/git/commit`, { message }),
  push: (projectId: string) => apiClient.post(`/projects/${projectId}/git/push`),
  pull: (projectId: string) => apiClient.post(`/projects/${projectId}/git/pull`),
}

// Editor State Persistence
export const editorStateApi = {
  get: (projectId: string) => apiClient.get(`/projects/${projectId}/editor/state`),
  save: (projectId: string, data: { open_tabs: string; active_tab: string; sidebar: string }) =>
    apiClient.post(`/projects/${projectId}/editor/state`, data),
}

// Global System File Management
export const systemFilesApi = {
  list: () => apiClient.get('/system/files'),
  read: (path: string) => apiClient.get(`/system/files${path}`),
  save: (path: string, content: string) => apiClient.put(`/system/files${path}`, { content }),
  createFile: (path: string) => apiClient.post(`/system/files${path}`, {}),
  createDirectory: (path: string) => apiClient.post(`/system/directories${path}`, {}),
  delete: (path: string) => apiClient.delete(`/system/files${path}`),
  rename: (path: string, newPath: string) => apiClient.patch(`/system/files${path}`, { newPath }),
}

// Workers Management
export const workersApi = {
  list: () => apiClient.get('/workers'),
  getPat: () => apiClient.post('/workers/pat', {}),
}

// Registry Management
export const registryApi = {
  listRepos: (projectId: string) => apiClient.get(`/registry/repos?project_id=${projectId}`),
  createRepo: (data: { project_id: string; name: string; type: string; description?: string; is_public?: boolean }) =>
    apiClient.post('/registry/repos', data),
  deleteRepo: (id: string) => apiClient.delete(`/registry/repos/${id}`),
  listArtifacts: (repoId: string) => apiClient.get(`/registry/repos/${repoId}/artifacts`),
  triggerSync: (repoId: string) => apiClient.post(`/registry/repos/${repoId}/sync`),
  createReplication: (data: { repo_id: string; source_url: string; schedule: string; concurrency?: number }) =>
    apiClient.post('/registry/replications', data),
}

