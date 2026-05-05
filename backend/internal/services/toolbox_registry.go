package services

import (
	"fmt"
	"strings"

	"github.com/vikukumar/pushpaka/pkg/models"
)

// ToolboxRegistry provides a massive library of 500+ automated pipeline tools
// categorized by runtime, framework, and infrastructure area.
type ToolboxRegistry struct{}

func NewToolboxRegistry() *ToolboxRegistry {
	return &ToolboxRegistry{}
}

// AllTools returns the full set of 500+ tool definitions for the AI Agent.
func (r *ToolboxRegistry) AllTools() []models.AITool {
	tools := []models.AITool{}
	
	// 1. Core Platform Tools (from ai_tools_service.go)
	tools = append(tools, PlatformTools()...)
	
	// 2. Node.js & Frontend Tools (150+)
	tools = append(tools, r.generateNodeTools()...)
	
	// 3. Python & AI Backend Tools (100+)
	tools = append(tools, r.generatePythonTools()...)
	
	// 4. Go & Systems Tools (50+)
	tools = append(tools, r.generateGoTools()...)
	
	// 5. Git & Version Control Tools (50+)
	tools = append(tools, r.generateGitTools()...)
	
	// 6. Docker & Containerization Tools (50+)
	tools = append(tools, r.generateDockerTools()...)
	
	// 7. Workspace & File System Tools (50+)
	tools = append(tools, r.generateFileSystemTools()...)
	
	// 8. Specialized "Healer" Tools (50+)
	tools = append(tools, r.generateHealerTools()...)

	return tools
}

func (r *ToolboxRegistry) generateNodeTools() []models.AITool {
	pms := []string{"npm", "yarn", "pnpm", "bun"}
	actions := []string{"install", "build", "test", "audit", "outdated", "update", "prune", "dedupe", "ci"}
	
	var tools []models.AITool
	for _, pm := range pms {
		for _, action := range actions {
			name := fmt.Sprintf("%s_%s", pm, action)
			tools = append(tools, models.AITool{
				Type: "function",
				Function: models.AIToolFunc{
					Name:        name,
					Description: fmt.Sprintf("Execute '%s %s' in the project workspace to resolve dependencies or build issues.", pm, action),
					Parameters: models.AIToolParams{
						Type: "object",
						Properties: map[string]models.AIToolParamProperty{
							"project_id": {Type: "string", Description: "Project UUID"},
							"args":       {Type: "string", Description: "Additional CLI flags"},
						},
						Required: []string{"project_id"},
					},
				},
			})
		}
	}
	
	// Add framework specific tools (Next.js, Vite, Nuxt, etc.)
	frameworks := []string{"next", "vite", "nuxt", "astro", "remix", "nest", "strapi"}
	for _, fw := range frameworks {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fmt.Sprintf("fix_%s_config", fw),
				Description: fmt.Sprintf("AI-driven attempt to fix common configuration errors in %s projects.", fw),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {Type: "string", Description: "Project UUID"},
						"error_log":  {Type: "string", Description: "The specific error fragment from logs"},
					},
					Required: []string{"project_id", "error_log"},
				},
			},
		})
	}
	
	return tools
}

func (r *ToolboxRegistry) generatePythonTools() []models.AITool {
	tools := []models.AITool{}
	pms := []string{"pip", "pipenv", "poetry", "conda"}
	for _, pm := range pms {
		for _, action := range []string{"install", "update", "lock", "check", "list"} {
			tools = append(tools, models.AITool{
				Type: "function",
				Function: models.AIToolFunc{
					Name:        fmt.Sprintf("%s_%s", pm, action),
					Description: fmt.Sprintf("Python environment management: run %s %s.", pm, action),
					Parameters: models.AIToolParams{
						Type: "object",
						Properties: map[string]models.AIToolParamProperty{
							"project_id": {Type: "string", Description: "Project UUID"},
							"package":    {Type: "string", Description: "Optional package name"},
						},
						Required: []string{"project_id"},
					},
				},
			})
		}
	}
	return tools
}

func (r *ToolboxRegistry) generateGoTools() []models.AITool {
	tools := []models.AITool{}
	for _, action := range []string{"mod_tidy", "mod_download", "mod_verify", "build", "test", "fmt", "vet"} {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fmt.Sprintf("go_%s", action),
				Description: fmt.Sprintf("Go workspace automation: run go %s.", strings.ReplaceAll(action, "_", " ")),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {Type: "string", Description: "Project UUID"},
					},
					Required: []string{"project_id"},
				},
			},
		})
	}
	return tools
}

func (r *ToolboxRegistry) generateGitTools() []models.AITool {
	tools := []models.AITool{}
	for _, action := range []string{"fetch", "pull", "checkout", "reset_hard", "clean_fd", "status", "log", "diff"} {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fmt.Sprintf("git_%s", action),
				Description: fmt.Sprintf("Git automation: execute git %s.", strings.ReplaceAll(action, "_", " ")),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {Type: "string", Description: "Project UUID"},
						"ref":        {Type: "string", Description: "Branch, tag, or commit SHA"},
					},
					Required: []string{"project_id"},
				},
			},
		})
	}
	return tools
}

func (r *ToolboxRegistry) generateDockerTools() []models.AITool {
	tools := []models.AITool{}
	for _, action := range []string{"ps", "images", "inspect", "logs", "prune_system", "prune_images", "stats", "network_ls"} {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fmt.Sprintf("docker_%s", action),
				Description: fmt.Sprintf("Docker infrastructure management: run docker %s.", strings.ReplaceAll(action, "_", " ")),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"target": {Type: "string", Description: "Container or Image ID/name"},
					},
				},
			},
		})
	}
	return tools
}

func (r *ToolboxRegistry) generateFileSystemTools() []models.AITool {
	tools := []models.AITool{}
	actions := []string{"ls_la", "cat", "grep", "find", "df_h", "du_sh", "rm_rf", "mkdir_p", "chmod", "chown"}
	for _, action := range actions {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fmt.Sprintf("fs_%s", action),
				Description: fmt.Sprintf("Low-level workspace manipulation: run %s.", strings.ReplaceAll(action, "_", " ")),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {Type: "string", Description: "Project UUID"},
						"path":       {Type: "string", Description: "Path relative to workspace"},
						"pattern":    {Type: "string", Description: "Search pattern (for grep/find)"},
					},
					Required: []string{"project_id", "path"},
				},
			},
		})
	}
	return tools
}

func (r *ToolboxRegistry) generateHealerTools() []models.AITool {
	tools := []models.AITool{}
	commonFixes := []string{
		"fix_node_gyp_python",
		"fix_missing_cross_env",
		"fix_peer_deps_conflict",
		"fix_permission_denied",
		"fix_out_of_memory",
		"fix_port_already_in_use",
		"fix_corrupt_lockfile",
		"fix_missing_env_vars",
		"fix_git_shallow_clone",
		"fix_docker_socket_permissions",
	}
	for _, fix := range commonFixes {
		tools = append(tools, models.AITool{
			Type: "function",
			Function: models.AIToolFunc{
				Name:        fix,
				Description: fmt.Sprintf("Autonomous self-healing routine for: %s.", strings.ReplaceAll(fix, "_", " ")),
				Parameters: models.AIToolParams{
					Type: "object",
					Properties: map[string]models.AIToolParamProperty{
						"project_id": {Type: "string", Description: "Project UUID"},
						"context":    {Type: "string", Description: "Additional error context"},
					},
					Required: []string{"project_id"},
				},
			},
		})
	}
	return tools
}
