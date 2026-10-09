// Package policy evaluates capability authorization without owning executors,
// credentials, prompts or user interactions. Loom supplies the legacy default
// and resolves confirm through its canonical request broker.
package policy

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Decision string

const (
	Allow   Decision = "allow"
	Confirm Decision = "confirm"
	Deny    Decision = "deny"
)

type Conditions struct {
	InsideWorkdir    bool     `json:"inside_workdir,omitempty"`
	CommandPrefixes  []string `json:"command_prefixes,omitempty"`
	MaxCost          *float64 `json:"max_cost,omitempty"`
	ProviderID       string   `json:"provider_id,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	Endpoint         string   `json:"endpoint,omitempty"`
	Model            string   `json:"model,omitempty"`
	DiscussionID     string   `json:"discussion_id,omitempty"`
	Operation        string   `json:"operation,omitempty"`
	LegacyPermission string   `json:"legacy_permission,omitempty"`
}
type Rule struct {
	ID         string     `json:"id"`
	Scope      string     `json:"scope"`
	ScopeID    string     `json:"scope_id,omitempty"`
	Subject    string     `json:"subject"`
	Decision   Decision   `json:"decision"`
	Conditions Conditions `json:"conditions,omitempty"`
	Migrated   bool       `json:"migrated,omitempty"`
}
type Document struct {
	Version  int    `json:"version"`
	Migrated bool   `json:"migrated"`
	Rules    []Rule `json:"rules"`
}

// Input contains only decision metadata. Cost is an observed monthly spend in
// the provider's reported currency; nil means unknown. Commands/paths are never
// copied into the audit log. Fallback is supplied by trusted application code.
type Input struct {
	Subject          string   `json:"subject"`
	ProjectID        string   `json:"project_id,omitempty"`
	AgentID          string   `json:"agent_id,omitempty"`
	MachineID        string   `json:"machine_id,omitempty"`
	ProviderID       string   `json:"provider_id,omitempty"`
	Endpoint         string   `json:"endpoint,omitempty"`
	Model            string   `json:"model,omitempty"`
	DiscussionID     string   `json:"discussion_id,omitempty"`
	Operation        string   `json:"operation,omitempty"`
	Workdir          string   `json:"workdir,omitempty"`
	Path             string   `json:"path,omitempty"`
	Command          string   `json:"command,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
	LegacyPermission string   `json:"-"`
	Fallback         Decision `json:"-"`
	ConsentKey       string   `json:"-"` // opaque operation revision, never audited
}
type Result struct {
	Decision Decision `json:"decision"`
	RuleID   string   `json:"rule_id,omitempty"`
	Reason   string   `json:"reason"`
}

func validDecision(d Decision) bool { return d == Allow || d == Confirm || d == Deny }
func ValidSubject(s string) bool {
	if s == "" || len(s) > 240 || strings.ContainsAny(s, " \t\r\n\x00") {
		return false
	}
	return !strings.Contains(s, "*") || s == "*" || strings.HasSuffix(s, ".*") && strings.Count(s, "*") == 1 || strings.HasPrefix(s, "mcp.tool:") && strings.HasSuffix(s, "/*") && strings.Count(s, "*") == 1
}
func Validate(d Document) error {
	if d.Version != 1 || len(d.Rules) > 512 {
		return errors.New("policy version 1 and at most 512 rules required")
	}
	seen := map[string]bool{}
	for _, r := range d.Rules {
		if r.ID == "" || len(r.ID) > 120 || strings.ContainsAny(r.ID, "\r\n\x00") || seen[r.ID] || !ValidSubject(r.Subject) || !validDecision(r.Decision) {
			return errors.New("invalid or duplicate policy rule")
		}
		seen[r.ID] = true
		switch r.Scope {
		case "global":
			if r.ScopeID != "" {
				return errors.New("global scope has no id")
			}
		case "project", "agent", "machine":
			if r.ScopeID == "" || len(r.ScopeID) > 240 {
				return errors.New("scope id required")
			}
		default:
			return errors.New("invalid policy scope")
		}
		c := r.Conditions
		if c.MaxCost != nil && (*c.MaxCost < 0 || math.IsNaN(*c.MaxCost) || math.IsInf(*c.MaxCost, 0)) {
			return errors.New("invalid max cost")
		}
		if len(c.CommandPrefixes) > 32 {
			return errors.New("at most 32 command prefixes")
		}
		for _, p := range c.CommandPrefixes {
			if strings.TrimSpace(p) == "" || len(p) > 512 || strings.ContainsAny(p, "\r\n\x00") {
				return errors.New("invalid command prefix")
			}
		}
		if len(c.Endpoint) > 2048 || len(c.Model) > 240 || len(c.ProviderID) > 240 || len(c.AgentID) > 240 || len(c.DiscussionID) > 240 || len(c.Operation) > 80 {
			return errors.New("policy condition too long")
		}
	}
	return nil
}
func matches(subject, pattern string) bool {
	return subject == pattern || pattern == "*" || strings.HasSuffix(pattern, "*") && strings.HasPrefix(subject, strings.TrimSuffix(pattern, "*"))
}
func inside(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return false
	}
	// Resolve the nearest existing parent too, so a new file through a symlink
	// cannot qualify for an automatic approval.
	resolve := func(p string) (string, error) {
		tail := []string{}
		for {
			_, statErr := os.Lstat(p)
			if statErr == nil {
				resolved, e := filepath.EvalSymlinks(p)
				if e != nil {
					return "", e
				}
				for i := len(tail) - 1; i >= 0; i-- {
					resolved = filepath.Join(resolved, tail[i])
				}
				return resolved, nil
			}
			if !os.IsNotExist(statErr) {
				return "", statErr
			}
			parent := filepath.Dir(p)
			if parent == p {
				return "", statErr
			}
			tail = append(tail, filepath.Base(p))
			p = parent
		}
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	path, err = resolve(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
func Evaluate(rules []Rule, in Input) Result {
	fallback := in.Fallback
	if !validDecision(fallback) {
		fallback = Confirm
	}
	result := Result{Decision: fallback, Reason: "legacy_default"}
	best := -1000
	for _, r := range rules {
		rank := 0
		switch r.Scope {
		case "global":
		case "machine":
			if in.MachineID != r.ScopeID {
				continue
			}
			rank = 10
		case "agent":
			if in.AgentID != r.ScopeID {
				continue
			}
			rank = 20
		case "project":
			if in.ProjectID != r.ScopeID {
				continue
			}
			rank = 30
		default:
			continue
		}
		if !matches(in.Subject, r.Subject) {
			continue
		}
		if r.Subject == in.Subject {
			rank += 2
		} else if r.Subject != "*" {
			rank++
		}
		if r.Migrated {
			rank -= 100
		} // explicit user rules always beat migrated defaults
		c := r.Conditions
		if c.ProviderID != "" && c.ProviderID != in.ProviderID || c.AgentID != "" && c.AgentID != in.AgentID || c.Endpoint != "" && c.Endpoint != in.Endpoint || c.Model != "" && c.Model != in.Model || c.DiscussionID != "" && c.DiscussionID != in.DiscussionID || c.LegacyPermission != "" && c.LegacyPermission != in.LegacyPermission {
			continue
		}
		if c.Operation != "" && c.Operation != in.Operation {
			continue
		}
		if c.InsideWorkdir && !inside(in.Workdir, in.Path) {
			continue
		}
		if len(c.CommandPrefixes) > 0 {
			// Prefixes authorize simple argv commands only; never compound shell
			// programs, substitution, pipes or redirection.
			if strings.ContainsAny(in.Command, ";&|`$<>\r\n") {
				continue
			}
			found := false
			for _, p := range c.CommandPrefixes {
				p = strings.TrimSpace(p)
				found = found || in.Command == p || strings.HasPrefix(in.Command, p+" ")
			}
			if !found {
				continue
			}
		}
		decision, reason := r.Decision, "rule"
		if c.MaxCost != nil {
			if in.Cost == nil {
				continue
			} // unknown usage never becomes zero
			if math.IsNaN(*in.Cost) || math.IsInf(*in.Cost, 0) || *in.Cost < 0 {
				decision, reason = Deny, "invalid_cost"
			} else if *in.Cost >= *c.MaxCost {
				decision, reason = Deny, "cost_limit"
			}
		}
		if rank >= best {
			best = rank
			result = Result{decision, r.ID, reason}
		}
	}
	return result
}

type AuditEntry struct {
	ID       uint64   `json:"id"`
	At       int64    `json:"at"`
	Subject  string   `json:"subject"`
	Decision Decision `json:"decision"`
	RuleID   string   `json:"rule_id,omitempty"`
	Reason   string   `json:"reason"`
	DryRun   bool     `json:"dry_run"`
}
type Audit struct {
	mu       sync.Mutex
	seq      uint64
	entries  []AuditEntry
	Capacity int
}

func (a *Audit) Record(in Input, r Result, dry bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.seq++
	a.entries = append(a.entries, AuditEntry{a.seq, time.Now().UnixMilli(), in.Subject, r.Decision, r.RuleID, r.Reason, dry})
	capacity := a.Capacity
	if capacity <= 0 {
		capacity = 512
	}
	if len(a.entries) > capacity {
		a.entries = append([]AuditEntry(nil), a.entries[len(a.entries)-capacity:]...)
	}
}
func (a *Audit) Snapshot() []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]AuditEntry{}, a.entries...)
}
