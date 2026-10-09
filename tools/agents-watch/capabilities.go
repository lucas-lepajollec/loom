package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

const capabilitiesDir = "internal/loom/harness/capabilities"

type probeResult struct {
	Message      string         `json:"message"`
	Error        string         `json:"error,omitempty"`
	Capabilities map[string]any `json:"capabilities,omitempty"`
}

// Retain unknown announcement fields. Strip runtime identity/state, not stable
// command, authentication or mode IDs. Metadata is an extension-key inventory:
// its values may contain machine/account data and are not a public contract.
func normalizeCapability(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, child := range v {
			switch strings.ToLower(k) {
			case "messagecount", "pendingmessagecount", "sessionname", "sessionid", "sessionfile", "sessionpath", "cwd", "directory", "timestamp", "createdat", "updatedat", "created_at", "updated_at", "version", "useragent", "modelcount", "model_count", "platformfamily", "platformos", "nextcursor", "currentmodelid", "currentmodeid", "currentvalue", "default", "connected":
				continue
			}
			if k == "options" && (v["category"] == "model" || v["id"] == "model") {
				out[k] = modelCapabilities(child)
			} else if k == "_meta" {
				keys := map[string]any{}
				if meta, ok := child.(map[string]any); ok {
					for name := range meta {
						keys[name] = true
					}
				}
				out[k] = keys
			} else if k == "models" || k == "availableModels" || k == "model" {
				if wrapper, ok := child.(map[string]any); ok && wrapper["availableModels"] != nil {
					out[k] = normalizeCapability(wrapper, k)
				} else if k == "model" {
					if _, ok := child.(map[string]any); ok {
						out[k] = modelCapabilities([]any{child})
					} else {
						out[k] = child
					}
				} else {
					switch child.(type) {
					case map[string]any, []any:
						out[k] = modelCapabilities(child)
					default:
						out[k] = child
					}
				}
			} else {
				out[k] = normalizeCapability(child, k)
			}
		}
		return out
	case []any:
		// Stable identifiers make added/removed commands, modes and config choices
		// visible as paths, rather than treating every list edit as one replacement.
		named := false
		switch key {
		case "authMethods", "availableCommands", "commands", "configOptions", "availableModes", "options":
			named = true
		}
		if named {
			items := map[string]any{}
			for _, child := range v {
				item, ok := child.(map[string]any)
				if !ok {
					named = false
					break
				}
				identity := ""
				for _, field := range []string{"id", "value", "name"} {
					if text, ok := item[field].(string); ok && text != "" {
						identity = text
						break
					}
				}
				if identity == "" || items[identity] != nil {
					named = false
					break
				}
				items[identity] = normalizeCapability(child, key)
			}
			if named {
				return items
			}
		}
		unique := map[string]any{}
		for _, child := range v {
			normalized := normalizeCapability(child, key)
			data, _ := json.Marshal(normalized)
			unique[string(data)] = normalized
		}
		keys := make([]string, 0, len(unique))
		for k := range unique {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, unique[k])
		}
		return out
	default:
		if text, ok := value.(string); ok {
			lower := strings.ToLower(key)
			if strings.HasSuffix(lower, "home") || strings.HasSuffix(lower, "path") || (lower == "pattern" && filepath.IsAbs(text)) {
				return "<path>"
			}
		}
		return value
	}
}

// A catalog is account-dependent. Record the union of advertised field shapes,
// effort choices and flags rather than model identity, prices or catalog size.
func modelCapabilities(value any) any {
	fields := map[string]map[string]any{}
	record := func(path string, val any) {
		if fields[path] == nil {
			fields[path] = map[string]any{}
		}
		b, _ := json.Marshal(val)
		fields[path][string(b)] = val
	}
	var walk func(string, any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			if len(x) == 0 {
				record(path, "object")
			}
			for k, child := range x {
				walk(path+"/"+pointerKey(k), child)
			}
		case []any:
			if len(x) == 0 {
				record(path, "array")
			}
			for _, child := range x {
				walk(path+"/*", child)
			}
		default:
			val := any(fmt.Sprintf("%T", v))
			if _, ok := v.(json.Number); ok {
				val = "number"
			}
			if v == nil {
				val = "null"
			}
			if _, ok := v.(bool); ok {
				val = v
			}
			if text, ok := v.(string); ok {
				leaf := path[strings.LastIndex(path, "/")+1:]
				switch strings.ToLower(leaf) {
				case "id", "modelid", "model", "name", "displayname", "description", "provider", "family", "url", "baseurl", "npm", "value", "upgrade", "message", "migrationmarkdown", "retirementat", "upgradecopy", "modellink":
				default:
					val = text
				}
			}
			record(path, val)
		}
	}
	// Maps under models are usually keyed by model ID (OpenCode); a single
	// selected model is a descriptor. All other catalogs are arrays.
	switch x := value.(type) {
	case []any:
		for _, model := range x {
			walk("", model)
		}
	case map[string]any:
		if _, ok := x["id"]; ok {
			walk("", x)
		} else {
			for _, model := range x {
				walk("", model)
			}
		}
	}
	out := map[string]any{}
	for path, values := range fields {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		list := make([]any, 0, len(keys))
		for _, k := range keys {
			list = append(list, values[k])
		}
		out[path] = list
	}
	return out
}

func normalizeCapabilities(raw map[string]any) ([]byte, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var value any
	if err := decodeSchema(data, &value); err != nil {
		return nil, err
	}
	data, err = json.MarshalIndent(normalizeCapability(value, ""), "", "  ")
	return append(data, '\n'), err
}
func pointerKey(key string) string {
	return strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
}

func capabilityDiff(before, after []byte) (string, error) {
	var old, next map[string]any
	if err := decodeSchema(before, &old); err != nil {
		return "", err
	}
	if err := decodeSchema(after, &next); err != nil {
		return "", err
	}
	changes := []string{}
	var walk func(string, any, any)
	walk = func(path string, a, b any) {
		if reflect.DeepEqual(a, b) {
			return
		}
		x, xok := a.(map[string]any)
		y, yok := b.(map[string]any)
		if !xok || !yok {
			changes = append(changes, "- changed `"+path+"`")
			return
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		ordered := make([]string, 0, len(keys))
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			p := path + "/" + pointerKey(k)
			a, existsA := x[k]
			b, existsB := y[k]
			if !existsA {
				changes = append(changes, "- added `"+p+"`")
			} else if !existsB {
				changes = append(changes, "- removed `"+p+"`")
			} else {
				walk(p, a, b)
			}
		}
	}
	walk("", old, next)
	if len(changes) == 0 {
		return "unchanged", nil
	}
	return strings.Join(changes, "\n"), nil
}

// Always emit candidate artifacts; only an explicit local refresh writes the
// checkout. Publishing copies baselines/updates into its isolated review tree.
func checkCapabilities(id string, raw map[string]any, baselineDir, outDir string, update bool) (string, []byte, error) {
	if len(raw) == 0 {
		return "unavailable (no handshake announcement)", nil, nil
	}
	data, err := normalizeCapabilities(raw)
	if err != nil {
		return "", nil, err
	}
	if err := os.WriteFile(filepath.Join(outDir, id+"-capabilities.json"), data, 0644); err != nil {
		return "", nil, err
	}
	path := filepath.Join(baselineDir, id+".json")
	before, err := os.ReadFile(path)
	summary := "baseline created"
	if err == nil {
		summary, err = capabilityDiff(before, data)
	} else if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return "", nil, err
	}
	if summary != "unchanged" && summary != "baseline created" {
		if err := os.WriteFile(filepath.Join(outDir, id+"-capabilities.diff"), []byte(summary+"\n"), 0644); err != nil {
			return "", nil, err
		}
	}
	if update {
		if err := saveCapabilitySnapshot(baselineDir, id, data); err != nil {
			return "", nil, err
		}
	}
	if strings.HasPrefix(summary, "- ") {
		lines := strings.Split(summary, "\n")
		if len(lines) > 80 {
			summary = strings.Join(lines[:80], "\n") + "\n- … additional paths in " + id + "-capabilities.diff"
		}
	}
	return summary, data, nil
}
func saveCapabilitySnapshot(dir, id string, data []byte) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".json"), data, 0644)
}
