export interface User {
  id: string
  email: string
  name: string
  role: string
  is_active: boolean
  avatar_url: string
  oauth_provider: string
  created_at: string
  updated_at: string
}

// SafeUser matches the backend SafeUser struct (no password hash, has extra fields)
export type SafeUser = User

export interface UpdateUserRoleRequest {
  role: 'admin' | 'user' | 'viewer'
  is_active?: boolean
}

export interface UsersListResponse {
  data: SafeUser[]
  total: number
  limit: number
  offset: number
}


export interface Project {
  id: string
  user_id: string
  name: string
  repo_url: string
  branch: string
  install_command: string
  build_command: string
  start_command: string
  run_dir: string
  port: number
  framework: string
  status: 'active' | 'inactive' | 'building' | 'ready'
  task_status?: string
  current_task_id?: string
  latest_task_id?: string
  is_private: boolean
  cpu_limit: string
  memory_limit: string
  restart_policy: string
  deploy_target: string
  k8s_namespace: string
  // Auto-sync fields
  auto_sync_enabled: boolean
  sync_interval_secs: number
  // Git metadata
  latest_commit_sha: string
  latest_commit_msg: string
  latest_commit_at: string
  language: string
  package_manager: string
  created_at: string
  updated_at: string
}

export interface AuditLog {
  id: string
  user_id: string
  action: string
  resource: string
  resource_id: string
  metadata: string
  ip_addr: string
  user_agent: string
  created_at: string
}

export interface WebhookConfig {
  id: string
  project_id: string
  provider: string
  branch: string
  webhook_url: string
  created_at: string
}

export type DeploymentStatus = 'queued' | 'building' | 'running' | 'failed' | 'stopped'

export interface Deployment {
  id: string
  project_id: string
  user_id: string
  commit_sha: string
  commit_msg: string
  branch: string
  status: DeploymentStatus
  image_tag: string
  container_id: string
  url: string
  is_default: boolean
  error_msg: string
  started_at: string | null
  finished_at: string | null
  created_at: string
  updated_at: string
}

export interface DeploymentLog {
  id: string
  deployment_id: string
  level: 'info' | 'error' | 'debug' | 'warn'
  message: string
  stream: 'stdout' | 'stderr' | 'system'
  created_at: string
}

export interface Domain {
  id: string
  project_id: string
  user_id: string
  domain: string
  verified: boolean
  ssl_enabled: boolean
  created_at: string
}

export interface EnvVar {
  id: string
  project_id: string
  key: string
  has_value: boolean
  created_at: string
}

export interface AuthResponse {
  token: string
  user: User
}

export interface ApiResponse<T> {
  data?: T
  error?: string
}

export interface SystemInfo {
  docker: {
    available: boolean
    host: string
  }
  git: {
    available: boolean
    version: string
  }
  workers: {
    total: number
    active_jobs: number
    idle: number
    sync: number
    sync_active: number
    build: number
    build_active: number
    test: number
    test_active: number
    ai: number
    ai_active: number
    deploy: number
    deploy_active: number
    queue_mode: 'redis' | 'in-process'
    /** false when workers run as separate Redis-connected processes (untracked by API) */
    tracked: boolean
  }
  runtime: {
    os: string
    arch: string
    in_container: boolean
  }
  load: {
    cpu_percent: number
    ram_total: number
    ram_used: number
    ram_percent: number
    hostname: string
    ip: string
  }
}

export interface WorkerNode {
  id: string
  name: string
  type: 'integrated' | 'vaahan' | 'hybrid'
  status: 'active' | 'offline' | 'disconnected'
  ip_address: string
  os: string
  architecture: string
  go_version: string
  docker_version: string
  node_version: string
  memory_total: number
  cpu_count: number
  last_seen_at: string | null
  created_at: string
}

export type TaskType = 'sync' | 'fetch' | 'build' | 'test' | 'deploy' | 'aifix'
export type TaskState = 'pending' | 'running' | 'completed' | 'failed'

export interface ProjectTask {
  id: string
  project_id: string
  type: TaskType
  status: TaskState
  commit_sha?: string
  log?: string
  error?: string
  worker_id?: string
  started_at: string | null
  finished_at: string | null
  created_at: string
}
