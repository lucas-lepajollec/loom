package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

func validateRegistry(data []byte) error {
	var registry struct {
		Agents []struct {
			ID           string `json:"id"`
			Distribution struct {
				Binary map[string]struct {
					Archive string `json:"archive"`
				} `json:"binary"`
			} `json:"distribution"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return err
	}
	if len(registry.Agents) == 0 {
		return errors.New("empty ACP registry")
	}
	for _, a := range registry.Agents {
		if a.ID == "" {
			return errors.New("registry agent missing ID")
		}
		for _, b := range a.Distribution.Binary {
			u, err := url.Parse(b.Archive)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
				return errors.New("registry archive must use HTTPS: " + a.ID)
			}
		}
	}
	return nil
}

// Match the selected messages used by tools/generate-codex-types.py. All
// original definitions and unions are retained, without generating Go edits.
func mergeCodexSchema(dir, typesFile string) ([]byte, error) {
	file, err := parser.ParseFile(token.NewFileSet(), typesFile, nil, 0)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.TYPE {
			for _, spec := range gen.Specs {
				names = append(names, spec.(*ast.TypeSpec).Name.Name)
			}
		}
	}
	files := map[string][]string{}
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			files[d.Name()] = append(files[d.Name()], path)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	defs := map[string]any{}
	for _, name := range names {
		paths := files[name+".json"]
		if len(paths) != 1 {
			return nil, fmt.Errorf("Codex schema %s: expected one file, found %d", name, len(paths))
		}
		data, err := os.ReadFile(paths[0])
		if err != nil {
			return nil, err
		}
		var schema map[string]any
		if err := decodeSchema(data, &schema); err != nil {
			return nil, err
		}
		if embedded, ok := schema["definitions"].(map[string]any); ok {
			for k, v := range embedded {
				defs[k] = v
			}
		}
		delete(schema, "definitions")
		defs[name] = schema
	}
	return json.MarshalIndent(map[string]any{"$schema": "http://json-schema.org/draft-07/schema#", "definitions": defs}, "", "  ")
}

// Schema integers can exceed float64's exact range; retain their wire values.
func decodeSchema(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	return decoder.Decode(value)
}
func schemaSummary(before, after []byte) (string, error) {
	var a, b map[string]any
	if err := decodeSchema(before, &a); err != nil {
		return "", err
	}
	if err := decodeSchema(after, &b); err != nil {
		return "", err
	}
	if reflect.DeepEqual(a, b) {
		return "unchanged", nil
	}
	changes := []string{}
	var walk func(string, any, any)
	walk = func(path string, old, new any) {
		if reflect.DeepEqual(old, new) || len(changes) >= 20 {
			return
		}
		x, xok := old.(map[string]any)
		y, yok := new.(map[string]any)
		if !xok || !yok {
			changes = append(changes, path)
			return
		}
		keys := map[string]bool{}
		for k := range x {
			keys[k] = true
		}
		for k := range y {
			keys[k] = true
		}
		ordered := []string{}
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			walk(path+"/"+k, x[k], y[k])
		}
	}
	walk("", a, b)
	return "changed paths (first 20): " + strings.Join(changes, ", "), nil
}
func checkSchema(c candidate, env []string, outDir string) (string, error) {
	var data []byte
	var original string
	switch c.ID {
	case "codex":
		dir, err := os.MkdirTemp("", "loom-codex-schema-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(dir)
		out, err := run(time.Minute, env, c.Binary, "app-server", "generate-json-schema", "--out", dir)
		if err != nil {
			return "generation failed", fmt.Errorf("Codex schema generation failed: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		data, err = mergeCodexSchema(dir, "internal/loom/runtime/codexapp/types_generated.go")
		if err != nil {
			return "generation failed", err
		}
		original = "internal/loom/runtime/codexapp/schema/messages.json"
	case "opencode":
		// stderr is separate: OpenCode diagnostics must not corrupt its OpenAPI.
		var err error
		data, err = generateOpenAPI(c.Binary, env)
		if err != nil {
			return "generation failed", err
		}
		original = "internal/loom/runtime/opencodehttp/schema/openapi.json"
	default:
		return "no generated schema", nil
	}
	before, err := os.ReadFile(original)
	if err != nil {
		return "", err
	}
	candidatePath := filepath.Join(outDir, c.ID+"-schema.json")
	if err := os.WriteFile(candidatePath, append(data, '\n'), 0644); err != nil {
		return "", err
	}
	summary, err := schemaSummary(before, data)
	if err != nil {
		return "invalid schema", err
	}
	if summary != "unchanged" {
		diff, _ := run(15*time.Second, nil, "git", "diff", "--no-index", "--", original, candidatePath)
		_ = os.WriteFile(filepath.Join(outDir, c.ID+"-schema.diff"), diff, 0644)
		return summary, fmt.Errorf("schema %s", summary)
	}
	return summary, nil
}
