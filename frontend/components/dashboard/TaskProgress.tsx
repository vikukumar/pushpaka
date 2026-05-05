'use client'

import { ProjectTask, TaskType } from '@/types'
import { CheckCircle2, Circle, Clock, Loader2, XCircle, RefreshCw, Package, CheckSquare, ExternalLink } from 'lucide-react'
import { TaskModal } from './TaskModal'
import { useState } from 'react'
import { formatDistanceToNow } from 'date-fns'
import { tasksApi } from '@/lib/api'
import toast from 'react-hot-toast'

interface TaskProgressProps {
  tasks: ProjectTask[]
  onRefresh?: () => void
}

const taskIcons: Record<TaskType, any> = {
  sync: RefreshCw,
  fetch: RefreshCw,
  build: Package,
  test: CheckSquare,
  deploy: Package,
  aifix: RefreshCw,
}

const taskLabels: Record<TaskType, string> = {
  sync: 'Sync Source',
  fetch: 'Fetch Metadata',
  build: 'Build Project',
  test: 'Run Tests',
  deploy: 'Deployment',
  aifix: 'AI Repair',
}

export function TaskProgress({ tasks, onRefresh }: TaskProgressProps) {
  const steps: TaskType[] = ['sync', 'build', 'test', 'deploy']
  
  // Find the latest task for each type
  const [selectedTask, setSelectedTask] = useState<ProjectTask | null>(null)

  const getTaskStatus = (type: TaskType) => {
    const task = [...tasks].reverse().find(t => t.type === type)
    if (!task) return 'pending'
    return task.status
  }

  const getTask = (type: TaskType) => {
    return [...tasks].reverse().find(t => t.type === type)
  }

  const getTaskError = (type: TaskType) => {
    const task = getTask(type)
    return task?.error || (task?.status === 'failed' ? 'Task failed without explicit error' : null)
  }

  return (
    <div className="card space-y-4">
      <h3 className="text-sm font-semibold text-slate-400 uppercase tracking-wider">
        Automation Pipeline
      </h3>
      
      <div className="flex flex-col md:flex-row md:items-center justify-between gap-4">
        {steps.map((type, idx) => {
          const status = getTaskStatus(type)
          const task = getTask(type)
          const Icon = taskIcons[type]
          
          return (
            <div key={type} className="flex-1 flex flex-col items-center group relative">
              {/* Connector line */}
              {idx < steps.length - 1 && (
                <div className="hidden md:block absolute top-5 left-1/2 w-full h-[2px] bg-slate-800 -z-10">
                  <div 
                    className={`h-full bg-brand-500 transition-all duration-500 ${status === 'completed' ? 'w-full' : 'w-0'}`} 
                  />
                </div>
              )}
              
              <button 
                onClick={() => task && setSelectedTask(task)}
                disabled={!task}
                className={`
                  w-10 h-10 rounded-full flex items-center justify-center border-2 transition-all duration-300 relative
                  ${status === 'completed' ? 'bg-brand-500/10 border-brand-500 text-brand-500 hover:bg-brand-500/20' : 
                    status === 'running' ? 'bg-brand-500/20 border-brand-500 animate-pulse text-brand-500 hover:bg-brand-500/30' :
                    status === 'failed' ? 'bg-red-500/10 border-red-500 text-red-500 hover:bg-red-500/20' :
                    'bg-slate-900 border-slate-700 text-slate-500 cursor-not-allowed'}
                `}
              >
                {status === 'running' ? (
                  <Loader2 size={18} className="animate-spin" />
                ) : (
                  <Icon size={18} />
                )}
                
                {task && (
                  <div className="absolute -top-1 -right-1 opacity-0 group-hover:opacity-100 transition-opacity bg-slate-800 rounded-full p-0.5 border border-slate-700">
                    <ExternalLink size={8} className="text-slate-400" />
                  </div>
                )}
              </button>
              
              <div className="mt-2 text-center cursor-pointer" onClick={() => task && setSelectedTask(task)}>
                <div className={`text-xs font-medium ${status === 'pending' ? 'text-slate-500' : 'text-slate-200'}`}>
                  {taskLabels[type]}
                </div>
                <div className={`text-[10px] uppercase tracking-tighter ${
                  status === 'completed' ? 'text-brand-500' : 
                  status === 'running' ? 'text-brand-400' :
                  status === 'failed' ? 'text-red-500' :
                  'text-slate-600'
                }`}>
                  {status}
                </div>
              </div>

              {status === 'failed' && onRefresh && (
                <button 
                  onClick={async (e) => {
                    e.stopPropagation()
                    if (task) {
                      const toastId = toast.loading(`Restarting ${taskLabels[type]}...`)
                      try {
                        await tasksApi.restart(task.id)
                        toast.success(`Task ${taskLabels[type]} restarting...`)
                        onRefresh()
                      } catch (err: any) {
                        toast.error(err.response?.data?.error || `Failed to restart ${taskLabels[type]}`)
                      } finally {
                        toast.dismiss(toastId)
                      }
                    }
                  }}
                  className="mt-2 flex items-center gap-1.5 px-2 py-1 rounded bg-red-500/10 hover:bg-red-500/20 text-red-400 border border-red-500/20 transition-colors text-[10px] font-bold uppercase"
                >
                  <RefreshCw size={10} />
                  Retry {type}
                </button>
              )}
            </div>
          )
        })}
      </div>

      {selectedTask && (
        <TaskModal 
          task={selectedTask}
          isOpen={!!selectedTask}
          onClose={() => setSelectedTask(null)}
          onRestart={onRefresh} // Trigger refresh to load the new pending task row
        />
      )}
    </div>
  )
}
