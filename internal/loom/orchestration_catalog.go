package loom

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Dynamic catalog of available Codeman CLIs, discovered models, effort levels,
// and active permission settings. Surfaced via `loom_subagent_catalog`,
// `loom_overview`, MCP handshake instructions, and GET /api/orchestration/catalog
// so both orchestrating agents and the UI always have live, accurate metadata.

type CatalogModel struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type SubagentCLICatalogItem struct {
	ID             string         `json:"id"`
	Label          string         `json:"label"`
	Installed      bool           `json:"installed"`
	Enabled        bool           `json:"enabled"`
	Version        string         `json:"version,omitempty"`
	Models         []CatalogModel `json:"models,omitempty"`
	Efforts        []string       `json:"efforts,omitempty"`
	PermissionMode string         `json:"permission_mode,omitempty"`
}

type SubagentCatalog struct {
	CodemanConnected bool                     `json:"codeman_connected"`
	CLIs             []SubagentCLICatalogItem `json:"clis"`
	Settings         OrchestrationSettings    `json:"settings"`
	Summary          string                   `json:"summary"`
}

func readLocalCodexModels() []CatalogModel {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return nil
	}
	codexDir := filepath.Join(home, ".codex")
	root, err := os.OpenRoot(codexDir)
	if err != nil {
		return nil
	}
	defer root.Close()
	f, err := root.Open("models_cache.json")
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20))
	if err != nil {
		return nil
	}
	var doc struct {
		Models []struct {
			Slug        string `json:"slug"`
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			Name        string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	out := make([]CatalogModel, 0, len(doc.Models))
	seen := map[string]bool{}
	for _, m := range doc.Models {
		id := strings.TrimSpace(m.Slug)
		if id == "" {
			id = strings.TrimSpace(m.ID)
		}
		if id == "" || seen[id] || strings.Contains(id, "auto-review") {
			continue
		}
		seen[id] = true
		label := strings.TrimSpace(m.DisplayName)
		if label == "" {
			label = strings.TrimSpace(m.Name)
		}
		if label == "" {
			label = id
		}
		out = append(out, CatalogModel{ID: id, Label: label})
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func defaultModelsForCLI(cliID string) ([]CatalogModel, []string) {
	switch cliID {
	case "claude":
		return []CatalogModel{
			{ID: "opus", Label: "Opus"},
			{ID: "sonnet", Label: "Sonnet"},
			{ID: "haiku", Label: "Haiku"},
		}, []string{"low", "medium", "high", "xhigh", "max"}
	case "codex":
		models := readLocalCodexModels()
		if len(models) == 0 {
			models = []CatalogModel{
				{ID: "gpt-5.4", Label: "gpt-5.4"},
				{ID: "gpt-5.3-codex", Label: "gpt-5.3-codex"},
			}
		}
		return models, []string{"low", "medium", "high", "xhigh"}
	default:
		return nil, nil
	}
}

// Overview and catalog share a short-lived observation. The permission
// broker and session creation always consult the authoritative settings.
var catalogObservation struct {
	sync.Mutex
	home     string
	config   codemanConfig
	settings OrchestrationSettings
	until    time.Time
	value    SubagentCatalog
}

func buildSubagentCatalog(ctx context.Context) SubagentCatalog {
	settings := loadOrchestrationSettings()
	out := SubagentCatalog{Settings: settings, CLIs: []SubagentCLICatalogItem{}}
	saved := savedCodemanConfig()
	if saved == nil || saved.URL == "" {
		out.Summary = "Codeman not connected"
		return out
	}
	home := LoomHome()
	catalogObservation.Lock()
	if catalogObservation.home == home && catalogObservation.config == *saved && catalogObservation.settings == settings && time.Now().Before(catalogObservation.until) {
		cached := catalogObservation.value
		catalogObservation.Unlock()
		return cached
	}
	catalogObservation.Unlock()
	cctx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	settingsResult := make(chan map[string]any, 1)
	go func() { v, _ := codemanGetSettings(cctx); settingsResult <- v }()
	clis, err := codemanListCLIs(cctx)
	cmSettings := <-settingsResult
	if err != nil {
		out.Summary = "Codeman unreachable"
		return out
	}
	out.CodemanConnected = true

	claudeMode, _ := cmSettings["claudeMode"].(string)
	codexBypass, _ := cmSettings["codexDangerouslyBypassApprovals"].(bool)

	var readySummaries []string
	for _, c := range clis {
		if c.Kind == "shell" {
			continue
		}
		installed := c.Installed == nil || *c.Installed
		enabled := c.Enabled == nil || *c.Enabled
		models, efforts := defaultModelsForCLI(c.ID)
		perm := ""
		if c.ID == "claude" && claudeMode != "" {
			perm = claudeMode
		} else if c.ID == "codex" {
			if codexBypass {
				perm = "bypass"
			} else {
				perm = "normal"
			}
		}
		item := SubagentCLICatalogItem{
			ID:             c.ID,
			Label:          c.Label,
			Installed:      installed,
			Enabled:        enabled,
			Version:        c.Version,
			Models:         models,
			Efforts:        efforts,
			PermissionMode: perm,
		}
		out.CLIs = append(out.CLIs, item)
		if installed && enabled {
			s := c.ID
			var meta []string
			if len(models) > 0 {
				top := make([]string, 0, min(4, len(models)))
				for i := 0; i < len(models) && i < 4; i++ {
					top = append(top, models[i].ID)
				}
				meta = append(meta, "models: "+strings.Join(top, ", "))
			}
			if len(efforts) > 0 {
				meta = append(meta, "effort: "+strings.Join(efforts, "/"))
			}
			if perm != "" {
				meta = append(meta, "perm: "+perm)
			}
			if len(meta) > 0 {
				s += " (" + strings.Join(meta, "; ") + ")"
			}
			readySummaries = append(readySummaries, s)
		}
	}
	if len(readySummaries) > 0 {
		out.Summary = "Available subagent CLIs: " + strings.Join(readySummaries, " | ") +
			" | subagent_permissions=" + settings.SubagentPermissions
	} else {
		out.Summary = "No enabled agent CLIs reported by Codeman"
	}
	catalogObservation.Lock()
	catalogObservation.home, catalogObservation.config, catalogObservation.settings = home, *saved, settings
	catalogObservation.value, catalogObservation.until = out, time.Now().Add(10*time.Second)
	catalogObservation.Unlock()
	return out
}
