package loom

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const intermediateLimit = 256 << 10
const finalLimit = 16 << 10

func valueAt(value any, path string) (any, bool) {
	if path == "" {
		return value, true
	}
	for part := range strings.SplitSeq(path, ".") {
		switch obj := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = obj[part]
			if !ok {
				return nil, false
			}
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(obj) {
				return nil, false
			}
			value = obj[i]
		default:
			return nil, false
		}
	}
	return value, true
}

func validFields(fields []string) error {
	if len(fields) > 32 {
		return errors.New("select at most 32 fields")
	}
	for _, field := range fields {
		if field == "" || len(field) > 256 || strings.HasPrefix(field, ".") || strings.HasSuffix(field, ".") || strings.Contains(field, "..") {
			return errors.New("invalid field path")
		}
	}
	return nil
}

func pickObject(value any, fields []string) (any, error) {
	if _, ok := value.(map[string]any); !ok {
		return nil, errors.New("projection requires objects")
	}
	out := map[string]any{}
	for _, field := range fields {
		v, ok := valueAt(value, field)
		if !ok {
			continue
		}
		// When both parent and child were selected, copying the parent
		// already includes the child. Avoid writing into the source object.
		parts := strings.Split(field, ".")
		dst := out
		for i, part := range parts {
			if i == len(parts)-1 {
				dst[part] = v
				break
			}
			child, ok := dst[part].(map[string]any)
			if !ok {
				child = map[string]any{}
				dst[part] = child
			}
			dst = child
		}
	}
	return out, nil
}

func projectFields(value any, fields []string) (any, error) {
	if len(fields) == 0 {
		return value, nil
	}
	if err := validFields(fields); err != nil {
		return nil, err
	}
	// A selected parent includes its descendants. Remove redundant paths
	// before projection so assigned source subtrees remain immutable.
	paths := slices.Sorted(slices.Values(fields))
	fields = nil
	for _, path := range paths {
		if !slices.ContainsFunc(fields, func(parent string) bool { return path == parent || strings.HasPrefix(path, parent+".") }) {
			fields = append(fields, path)
		}
	}
	if rows, ok := value.([]any); ok {
		out := make([]any, len(rows))
		for i, row := range rows {
			v, err := pickObject(row, fields)
			if err != nil {
				return nil, err
			}
			out[i] = v
		}
		return out, nil
	}
	return pickObject(value, fields)
}

// Decode text JSON using json.Number so a prior step's integer ID reaches the
// next tool unchanged. StructuredContent takes precedence over text mirrors.
func resultValue(result *mcp.CallToolResult, limit int) (any, error) {
	var raw []byte
	if result.StructuredContent != nil {
		if _, err := jsonSize(result.StructuredContent, limit); err != nil {
			return nil, err
		}
		var err error
		raw, err = json.Marshal(result.StructuredContent)
		if err != nil {
			return nil, err
		}
	} else {
		var parts []string
		size := 0
		for _, block := range result.Content {
			if t, ok := block.(*mcp.TextContent); ok {
				if len(parts) > 0 {
					size++
				}
				if len(t.Text) > limit-size {
					return nil, sizeError()
				}
				size += len(t.Text)
				parts = append(parts, t.Text)
			}
		}
		if len(parts) == 0 && len(result.Content) > 0 {
			return nil, errors.New("tool result has no JSON or text to project")
		}
		raw = []byte(strings.Join(parts, "\n"))
	}
	if len(raw) > limit {
		return nil, errors.New("MCP intermediate exceeds byte limit")
	}
	if !json.Valid(raw) {
		return string(raw), nil
	}
	var value any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	err := dec.Decode(&value)
	return value, err
}

func textResult(raw []byte, failed bool) *mcp.CallToolResult {
	return &mcp.CallToolResult{IsError: failed, Content: []mcp.Content{&mcp.TextContent{Text: string(raw)}}}
}

// A truncated result is still valid JSON. Count the escaped preview and its
// envelope against the final byte limit, including multibyte characters.
func boundedResult(raw []byte, limit int, failed bool) *mcp.CallToolResult {
	if len(raw) <= limit {
		return textResult(raw, failed)
	}
	preview := raw[:min(limit, len(raw))]
	type truncation struct {
		Trimmed bool   `json:"trimmed"`
		Total   int    `json:"total_bytes"`
		Preview string `json:"preview"`
	}
	for {
		for len(preview) > 0 && !utf8.Valid(preview) {
			preview = preview[:len(preview)-1]
		}
		b, _ := json.Marshal(truncation{true, len(raw), string(preview)})
		if len(b) <= limit || len(preview) == 0 {
			return textResult(b, failed)
		}
		preview = preview[:max(0, len(preview)-(len(b)-limit))]
	}
}

func compactCallResult(res *mcp.CallToolResult, fields []string, maxChars int) (*mcp.CallToolResult, error) {
	if maxChars == 0 {
		maxChars = 12000
	}
	if len(fields) == 0 {
		// Preserve small native MCP responses, including images, rich content
		// and upstream errors. Budget both text and structured representations.
		raw, err := json.Marshal(res)
		if err != nil {
			return nil, err
		}
		if utf8.RuneCount(raw) <= maxChars {
			return res, nil
		}
	}
	value, err := resultValue(res, 16<<20)
	if err != nil {
		return nil, err
	}
	if !res.IsError {
		value, err = projectFields(value, fields)
	}
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCount(raw) <= maxChars {
		return textResult(raw, res.IsError), nil
	}
	if maxChars < 64 {
		// Even the truncation envelope would exceed a very small text budget.
		return textResult([]byte(string([]rune(string(raw))[:maxChars-1])+"…"), res.IsError), nil
	}
	return boundedResult(raw, maxChars, res.IsError), nil
}

func sizeError() error {
	return fmt.Errorf("pipeline intermediates exceed %d bytes", intermediateLimit)
}

// JSON size with early exit. References can occur repeatedly in interpolated
// arguments. Never marshal a potentially exponential expansion to measure it.
func jsonSize(value any, limit int) (int, error) {
	used := 0
	var visit func(any) error
	visit = func(v any) error {
		switch x := v.(type) {
		case map[string]any:
			used += 2 + max(0, len(x)-1)
			for k, v := range x {
				if err := visit(k); err != nil {
					return err
				}
				used++
				if err := visit(v); err != nil {
					return err
				}
			}
		case []any:
			used += 2 + max(0, len(x)-1)
			for _, v := range x {
				if err := visit(v); err != nil {
					return err
				}
			}
		default:
			if s, ok := x.(string); ok && len(s) > limit-used {
				return sizeError()
			}
			b, err := json.Marshal(x)
			if err != nil {
				return err
			}
			used += len(b)
		}
		if used > limit {
			return sizeError()
		}
		return nil
	}
	err := visit(value)
	return used, err
}
