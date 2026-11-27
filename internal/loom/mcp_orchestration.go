package loom

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// First-class Loom MCP orchestration tools (loom_subagent_* and loom_board_*).
//
// Any agent session launched via Loom automatically receives a scoped MCP token
// (LOOM_MCP_TOKEN) bound to its session_id, root_id and depth. Through these
// tools, an orchestrator spawns isolated sub-sessions (optionally in dedicated
// Git worktrees), delegates tasks across CLIs/models, reads compact summaries,
// coordinates via a shared tree board, and cleanly stops workers.

type subagentSpawnArgs struct {
	Role       string `json:"role" jsonschema:"short role label for this worker (e.g. 'backend-dev', 'reviewer', 'researcher')"`
	Task       string `json:"task" jsonschema:"clear, actionable initial task prompt delivered to the worker once started"`
	CLI        string `json:"cli,omitempty" jsonschema:"Codeman CLI mode: claude, codex, gemini, opencode, antigravity, pi, grok, deepseek, omp, copilot, shell (default: inherits parent CLI or claude)"`
	Model      string `json:"model,omitempty" jsonschema:"optional model override supported by the chosen CLI"`
	Effort     string `json:"effort,omitempty" jsonschema:"optional reasoning effort (low, medium, high, xhigh, max)"`
	Workspace  string `json:"workspace,omitempty" jsonschema:"inherit (default, shares parent working directory) or worktree (creates an isolated Git worktree and branch)"`
	Branch     string `json:"branch,omitempty" jsonschema:"optional Git branch name when workspace is worktree (default: loom/<slug>)"`
	Name       string `json:"name,omitempty" jsonschema:"optional session display name"`
	WorkingDir string `json:"working_dir,omitempty" jsonschema:"optional working directory (defaults to parent session working directory)"`
	ParentID   string `json:"parent_id,omitempty" jsonschema:"parent session id (inferred automatically from your scoped MCP token when omitted)"`
}

type subagentSpawnResult struct {
	SessionID    string   `json:"session_id"`
	ParentID     string   `json:"parent_id,omitempty"`
	RootID       string   `json:"root_id"`
	Depth        int      `json:"depth"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	CLI          string   `json:"cli"`
	Model        string   `json:"model,omitempty"`
	Workspace    string   `json:"workspace"`
	WorkingDir   string   `json:"working_dir"`
	WorktreePath string   `json:"worktree_path,omitempty"`
	Branch       string   `json:"branch,omitempty"`
	CopiedFiles  []string `json:"copied_files,omitempty"`
	TaskSent     bool     `json:"task_sent"`
}

type subagentListArgs struct {
	IncludeStopped bool   `json:"include_stopped,omitempty" jsonschema:"include stopped sub-sessions in the tree (default false)"`
	RootID         string `json:"root_id,omitempty" jsonschema:"optional root session id (inferred automatically from your scoped MCP token)"`
}

type subagentListItem struct {
	OrchestrationNode
	LiveStatus string `json:"live_status,omitempty"`
	IsWorking  bool   `json:"is_working,omitempty"`
}

type subagentListResult struct {
	CallerSessionID string             `json:"caller_session_id,omitempty"`
	RootID          string             `json:"root_id,omitempty"`
	Depth           int                `json:"depth"`
	MaxDepth        int                `json:"max_depth"`
	MaxChildren     int                `json:"max_children"`
	Nodes           []subagentListItem `json:"nodes"`
}

type subagentSendArgs struct {
	SessionID  string `json:"session_id" jsonschema:"target sub-session id inside your orchestration tree"`
	Message    string `json:"message" jsonschema:"message or follow-up instruction to deliver"`
	Wait       bool   `json:"wait,omitempty" jsonschema:"wait for the worker's turn to finish before returning (default false)"`
	TimeoutSec int    `json:"timeout_sec,omitempty" jsonschema:"wait timeout in seconds when wait=true (default 60, max 600)"`
}

type subagentSendResult struct {
	SessionID   string `json:"session_id"`
	Delivered   bool   `json:"delivered"`
	Signal      string `json:"signal,omitempty"`
	TimedOut    bool   `json:"timed_out,omitempty"`
	LimitPaused bool   `json:"limit_paused,omitempty"`
	Status      string `json:"status,omitempty"`
}

type subagentWaitArgs struct {
	SessionIDs []string `json:"session_ids" jsonschema:"one or more sub-session ids in your orchestration tree"`
	Mode       string   `json:"mode,omitempty" jsonschema:"any (return as soon as one finishes, default) or all (wait until every listed session finishes or times out)"`
	TimeoutSec int      `json:"timeout_sec,omitempty" jsonschema:"timeout in seconds (default 60, max 600)"`
}

type subagentWaitOutcome struct {
	SessionID   string `json:"session_id"`
	Role        string `json:"role,omitempty"`
	Signal      string `json:"signal,omitempty"`
	Status      string `json:"status,omitempty"`
	TimedOut    bool   `json:"timed_out,omitempty"`
	Ended       bool   `json:"ended,omitempty"`
	LimitPaused bool   `json:"limit_paused,omitempty"`
	Error       string `json:"error,omitempty"`
}

type subagentWaitResult struct {
	Mode     string                `json:"mode"`
	Outcomes []subagentWaitOutcome `json:"outcomes"`
}

type subagentReadArgs struct {
	SessionID   string `json:"session_id" jsonschema:"target sub-session id in your orchestration tree"`
	IncludeDiff bool   `json:"include_diff,omitempty" jsonschema:"include compact git status summary when available"`
}

type subagentReadResult struct {
	SessionID  string `json:"session_id"`
	Role       string `json:"role,omitempty"`
	CLI        string `json:"cli,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Source     string `json:"source,omitempty"` // "transcript" | "terminal"
	Response   string `json:"response"`
	GitSummary string `json:"git_summary,omitempty"`
}

type subagentStopArgs struct {
	SessionID       string `json:"session_id" jsonschema:"target sub-session id in your orchestration tree"`
	Mode            string `json:"mode,omitempty" jsonschema:"interrupt (Esc, keeps session alive) or close (deletes session and kills tmux, default)"`
	CleanupWorktree bool   `json:"cleanup_worktree,omitempty" jsonschema:"remove the isolated Git worktree if clean (default false)"`
	ForceWorktree   bool   `json:"force_worktree,omitempty" jsonschema:"force worktree removal even with uncommitted changes (default false)"`
}

type subagentStopResult struct {
	SessionID       string `json:"session_id"`
	Action          string `json:"action"`
	WorktreeRemoved bool   `json:"worktree_removed,omitempty"`
	WorktreeError   string `json:"worktree_error,omitempty"`
}

type boardPostArgs struct {
	Text      string `json:"text" jsonschema:"concise update, question, handoff or result to post on the shared tree board"`
	Kind      string `json:"kind,omitempty" jsonschema:"update (default), question, result, or handoff"`
	ToSession string `json:"to_session,omitempty" jsonschema:"optional target session id inside the same tree"`
	RootID    string `json:"root_id,omitempty" jsonschema:"optional root session id (inferred automatically from scoped MCP token)"`
}

type boardReadArgs struct {
	SinceID string `json:"since_id,omitempty" jsonschema:"only return messages posted after this message id"`
	Limit   int    `json:"limit,omitempty" jsonschema:"maximum messages to return (default 50, max 200)"`
	RootID  string `json:"root_id,omitempty" jsonschema:"optional root session id (inferred automatically from scoped MCP token)"`
}

type boardReadResult struct {
	RootID   string         `json:"root_id"`
	Messages []BoardMessage `json:"messages"`
}

func resolveCallerScope(ctx context.Context, explicitParent string) SessionScope {
	if sc, ok := sessionScopeFromContext(ctx); ok {
		return sc
	}
	if p := strings.TrimSpace(explicitParent); p != "" {
		return orchResolveScope(p)
	}
	return SessionScope{}
}

func applyCLIModelConfig(req *codemanCreateSessionReq, mode, model, effort string) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	model = strings.TrimSpace(model)
	effort = strings.TrimSpace(effort)
	if mode == "claude" {
		if model != "" {
			req.Model = model
		}
		if effort != "" {
			req.Effort = effort
		}
		return
	}
	cfg := map[string]any{}
	if model != "" {
		cfg["model"] = model
	}
	if mode == "codex" && effort != "" {
		cfg["reasoningEffort"] = effort
	}
	if len(cfg) == 0 {
		return
	}
	switch mode {
	case "codex":
		req.CodexConfig = cfg
	case "opencode":
		req.OpenCodeCfg = cfg
	case "gemini":
		req.GeminiCfg = cfg
	case "antigravity":
		req.AntigravCfg = cfg
	case "pi":
		req.PiCfg = cfg
	case "grok":
		req.GrokCfg = cfg
	case "copilot":
		req.CopilotCfg = cfg
	case "deepseek":
		req.DeepSeekCfg = cfg
	case "omp":
		req.OMPCfg = cfg
	}
}

func buildInitialWorkerPrompt(role, task, parentID, rootID, branch string) string {
	task = strings.TrimSpace(task)
	if task == "" {
		return ""
	}
	var hdr []string
	if role != "" {
		hdr = append(hdr, "Role: "+role)
	}
	if branch != "" {
		hdr = append(hdr, "Isolated branch: "+branch)
	}
	if parentID != "" {
		hdr = append(hdr, fmt.Sprintf("Orchestrator: %s (tree %s)", parentID, rootID))
	}
	hdr = append(hdr, "Coordination: use loom_board_post for concise milestones/handoffs and stay within your assigned scope. Never message sessions outside your tree.")
	return "[" + strings.Join(hdr, " | ") + "]\n\n" + task
}

// spawnLoomSubagent provisions an optional Git worktree, injects a scoped Loom
// MCP token into Codeman's envOverrides, starts the worker CLI, registers the
// lineage node in BoltDB, and sends the initial task prompt.
func spawnLoomSubagent(ctx context.Context, caller SessionScope, args subagentSpawnArgs) (subagentSpawnResult, error) {
	role := strings.TrimSpace(args.Role)
	if role == "" {
		return subagentSpawnResult{}, errors.New("role is required")
	}
	settings := loadOrchestrationSettings()

	parentID := strings.TrimSpace(caller.SessionID)
	if parentID == "" {
		parentID = strings.TrimSpace(args.ParentID)
	}
	rootID := strings.TrimSpace(caller.RootID)
	parentDepth := caller.Depth
	if parentID != "" {
		if pn, ok := orchGetNode(parentID); ok {
			if pn.RootID != "" {
				rootID = pn.RootID
			} else {
				rootID = pn.SessionID
			}
			parentDepth = pn.Depth
		} else if rootID == "" {
			rootID = parentID
		}
	}
	nextDepth := 0
	if parentID != "" {
		nextDepth = parentDepth + 1
	}
	if nextDepth > settings.MaxDepth {
		return subagentSpawnResult{}, fmt.Errorf("orchestration depth limit reached (%d/%d): finish this task directly instead of spawning deeper sub-sessions", nextDepth, settings.MaxDepth)
	}
	if rootID != "" {
		if active := orchActiveChildrenCount(rootID); active >= settings.MaxChildrenPerRoot {
			return subagentSpawnResult{}, fmt.Errorf("active sub-session limit reached for this tree (%d/%d): stop finished workers before spawning more", active, settings.MaxChildrenPerRoot)
		}
	}

	// Resolve parent session metadata from Codeman when available (for defaults).
	var parentLive *codemanSessionInfo
	if parentID != "" {
		if info, err := codemanGetSession(ctx, parentID); err == nil {
			parentLive = info
		}
	}

	// Ensure parent is registered as a root node so the sidebar builds a clean tree.
	if parentID != "" {
		if _, ok := orchGetNode(parentID); !ok {
			pNode := OrchestrationNode{
				SessionID: parentID,
				RootID:    rootID,
				Depth:     parentDepth,
				Status:    "active",
			}
			if parentLive != nil {
				pNode.Name = parentLive.Name
				pNode.CLI = parentLive.Mode
				pNode.WorkingDir = parentLive.WorkingDir
			}
			_, _ = orchRegisterNode(pNode)
		}
	}

	cli := strings.ToLower(strings.TrimSpace(args.CLI))
	if cli == "" {
		if parentLive != nil && parentLive.Mode != "" && parentLive.Mode != "shell" {
			cli = parentLive.Mode
		} else {
			cli = "claude"
		}
	}

	workingDir := strings.TrimSpace(args.WorkingDir)
	if workingDir == "" {
		if caller.WorkingDir != "" {
			workingDir = caller.WorkingDir
		} else if parentLive != nil && parentLive.WorkingDir != "" {
			workingDir = parentLive.WorkingDir
		}
	}
	if workingDir == "" {
		return subagentSpawnResult{}, errors.New("working_dir is required (could not infer from parent session)")
	}

	workspaceMode := strings.ToLower(strings.TrimSpace(args.Workspace))
	if workspaceMode == "" {
		workspaceMode = "inherit"
	}
	if workspaceMode != "inherit" && workspaceMode != "worktree" {
		return subagentSpawnResult{}, errors.New("workspace must be 'inherit' or 'worktree'")
	}

	var wtInfo WorktreeInfo
	if workspaceMode == "worktree" {
		var err error
		wtInfo, err = createLoomWorktree(ctx, workingDir, role, args.Branch, settings.AutoWriteWorktreeMCP)
		if err != nil {
			return subagentSpawnResult{}, fmt.Errorf("worktree setup failed: %w", err)
		}
		workingDir = wtInfo.WorktreePath
	}

	name := strings.TrimSpace(args.Name)
	if name == "" {
		base := filepath.Base(strings.TrimRight(workingDir, "/\\"))
		if wtInfo.Branch != "" {
			name = role + " · " + strings.TrimPrefix(wtInfo.Branch, "loom/")
		} else if base != "" && base != "." && base != "/" {
			name = role + " · " + base
		} else {
			name = role
		}
	}

	createReq := codemanCreateSessionReq{
		WorkingDir: workingDir,
		Mode:       cli,
		Name:       name,
	}
	applyCLIModelConfig(&createReq, cli, args.Model, args.Effort)

	created, err := codemanCreateSession(ctx, createReq, parentID)
	if err != nil {
		if wtInfo.WorktreePath != "" {
			_ = removeLoomWorktree(ctx, wtInfo.RepoRoot, wtInfo.WorktreePath, true)
		}
		return subagentSpawnResult{}, err
	}

	effectiveRoot := rootID
	if effectiveRoot == "" {
		effectiveRoot = created.ID
	}
	// Mint scoped session token and write owner-only bridge file BEFORE
	// POST /sessions/:id/interactive spawns the CLI process.
	if settings.AutoInjectMCP {
		if tok, err := mintSessionToken(SessionScope{
			SessionID:  created.ID,
			ParentID:   parentID,
			RootID:     effectiveRoot,
			Depth:      nextDepth,
			WorkingDir: workingDir,
		}); err == nil {
			_ = writeSessionBridgeFile(created.ID, gatewayURL(), tok)
		}
	}

	node, err := orchRegisterNode(OrchestrationNode{
		SessionID:     created.ID,
		ParentID:      parentID,
		RootID:        effectiveRoot,
		Depth:         nextDepth,
		Role:          role,
		Name:          name,
		CLI:           cli,
		Model:         strings.TrimSpace(args.Model),
		Effort:        strings.TrimSpace(args.Effort),
		WorkspaceMode: workspaceMode,
		WorkingDir:    workingDir,
		BaseRepo:      wtInfo.RepoRoot,
		WorktreePath:  wtInfo.WorktreePath,
		Branch:        wtInfo.Branch,
		Status:        "active",
	})
	if err != nil {
		return subagentSpawnResult{}, err
	}

	if err := codemanStartSession(ctx, created.ID, cli == "shell"); err != nil {
		orchMarkStopped(created.ID)
		return subagentSpawnResult{}, fmt.Errorf("session %s created but failed to start: %w", created.ID, err)
	}

	taskSent := false
	if promptText := buildInitialWorkerPrompt(role, args.Task, parentID, effectiveRoot, wtInfo.Branch); promptText != "" {
		codemanWaitReady(ctx, created.ID, cli, 10*time.Second)
		if _, err := codemanSendPrompt(ctx, created.ID, promptText, false, 0); err == nil {
			taskSent = true
		}
	}

	// Post an initial audit milestone on the shared tree board.
	_, _ = orchPostBoard(BoardMessage{
		RootID:      effectiveRoot,
		FromSession: parentID,
		FromRole:    "orchestrator",
		ToSession:   created.ID,
		Kind:        "handoff",
		Text:        fmt.Sprintf("Spawned %s (%s%s)", role, cli, func() string {
			if wtInfo.Branch != "" {
				return ", branch " + wtInfo.Branch
			}
			return ""
		}()),
	})

	return subagentSpawnResult{
		SessionID:    node.SessionID,
		ParentID:     node.ParentID,
		RootID:       node.RootID,
		Depth:        node.Depth,
		Name:         node.Name,
		Role:         node.Role,
		CLI:          node.CLI,
		Model:        node.Model,
		Workspace:    node.WorkspaceMode,
		WorkingDir:   node.WorkingDir,
		WorktreePath: node.WorktreePath,
		Branch:       node.Branch,
		CopiedFiles:  wtInfo.CopiedFiles,
		TaskSent:     taskSent,
	}, nil
}

func registerLoomOrchestrationTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_spawn",
		Description: "Spawn a tracked Loom sub-session under the current orchestrator in any Codeman CLI/model, with automatic scoped Loom MCP access and optional isolated Git worktree + branch.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentSpawnArgs) (*mcp.CallToolResult, subagentSpawnResult, error) {
		caller := resolveCallerScope(ctx, args.ParentID)
		res, err := spawnLoomSubagent(ctx, caller, args)
		return nil, res, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_list",
		Description: "List all sub-sessions in your current orchestration tree (role, CLI, branch/worktree, live status) along with active guardrails.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentListArgs) (*mcp.CallToolResult, subagentListResult, error) {
		caller := resolveCallerScope(ctx, args.RootID)
		rootID := caller.RootID
		if rootID == "" {
			rootID = strings.TrimSpace(args.RootID)
		}
		settings := loadOrchestrationSettings()
		liveMap := map[string]codemanSessionInfo{}
		if live, err := codemanListSessions(ctx); err == nil {
			liveIDs := make(map[string]bool, len(live))
			for _, s := range live {
				liveMap[s.ID] = s
				liveIDs[s.ID] = true
			}
			orchSyncWithLive(liveIDs)
		}
		var nodes []OrchestrationNode
		if rootID != "" {
			nodes = orchTreeForRoot(rootID, args.IncludeStopped)
		} else {
			for _, n := range orchAllNodes() {
				if args.IncludeStopped || n.Status != "stopped" {
					nodes = append(nodes, n)
				}
			}
		}
		items := make([]subagentListItem, 0, len(nodes))
		for _, n := range nodes {
			item := subagentListItem{OrchestrationNode: n}
			if live, ok := liveMap[n.SessionID]; ok {
				item.LiveStatus = live.Status
				item.IsWorking = live.IsWorking
			}
			items = append(items, item)
		}
		return nil, subagentListResult{
			CallerSessionID: caller.SessionID,
			RootID:          rootID,
			Depth:           caller.Depth,
			MaxDepth:        settings.MaxDepth,
			MaxChildren:     settings.MaxChildrenPerRoot,
			Nodes:           items,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_send",
		Description: "Send a follow-up prompt or peer handoff to a sub-session inside your own orchestration tree. Refuses to message unrelated sessions.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentSendArgs) (*mcp.CallToolResult, subagentSendResult, error) {
		caller := resolveCallerScope(ctx, "")
		if _, err := orchCanTarget(caller, args.SessionID); err != nil {
			return nil, subagentSendResult{}, err
		}
		timeoutMs := args.TimeoutSec * 1000
		if timeoutMs <= 0 {
			timeoutMs = 60000
		}
		wr, err := codemanSendPrompt(ctx, args.SessionID, args.Message, args.Wait, timeoutMs)
		if err != nil {
			return nil, subagentSendResult{}, err
		}
		delivered := true
		if wr.Delivered != nil {
			delivered = *wr.Delivered
		}
		return nil, subagentSendResult{
			SessionID:   args.SessionID,
			Delivered:   delivered,
			Signal:      wr.Wait.Signal,
			TimedOut:    wr.Wait.TimedOut,
			LimitPaused: wr.LimitPaused,
			Status:      wr.Status,
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_wait",
		Description: "Wait for one ('any', default) or all ('all') listed sub-sessions in your tree to finish their current turn, exit, or pause for human approval.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentWaitArgs) (*mcp.CallToolResult, subagentWaitResult, error) {
		caller := resolveCallerScope(ctx, "")
		if len(args.SessionIDs) == 0 {
			return nil, subagentWaitResult{}, errors.New("session_ids is required")
		}
		mode := strings.ToLower(strings.TrimSpace(args.Mode))
		if mode != "all" {
			mode = "any"
		}
		timeoutMs := args.TimeoutSec * 1000
		if timeoutMs < 1000 {
			timeoutMs = 60000
		}
		if timeoutMs > 600000 {
			timeoutMs = 600000
		}
		nodes := make([]OrchestrationNode, 0, len(args.SessionIDs))
		for _, id := range args.SessionIDs {
			n, err := orchCanTarget(caller, id)
			if err != nil {
				return nil, subagentWaitResult{}, err
			}
			nodes = append(nodes, n)
		}

		waitCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		outcomes := make([]subagentWaitOutcome, len(nodes))
		var firstOnce sync.Once
		var wg sync.WaitGroup
		for i, n := range nodes {
			wg.Add(1)
			go func(idx int, node OrchestrationNode) {
				defer wg.Done()
				wr, err := codemanWaitSession(waitCtx, node.SessionID, "stop,idle,blocked,exit", timeoutMs, false)
				if err != nil {
					outcomes[idx] = subagentWaitOutcome{SessionID: node.SessionID, Role: node.Role, Error: err.Error()}
					return
				}
				outcomes[idx] = subagentWaitOutcome{
					SessionID:   node.SessionID,
					Role:        node.Role,
					Signal:      wr.Wait.Signal,
					Status:      wr.Status,
					TimedOut:    wr.Wait.TimedOut,
					Ended:       wr.Wait.Ended,
					LimitPaused: wr.LimitPaused,
				}
				if mode == "any" && (wr.Wait.Signal != "" || wr.Wait.Ended) {
					firstOnce.Do(cancel)
				}
			}(i, n)
		}
		wg.Wait()
		return nil, subagentWaitResult{Mode: mode, Outcomes: outcomes}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_read",
		Description: "Read the latest structured response from a sub-session (transcript or ANSI-stripped terminal tail) plus an optional compact git status summary to keep orchestrator context small.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentReadArgs) (*mcp.CallToolResult, subagentReadResult, error) {
		caller := resolveCallerScope(ctx, "")
		node, err := orchCanTarget(caller, args.SessionID)
		if err != nil {
			return nil, subagentReadResult{}, err
		}
		txt, src, err := codemanReadLastResponse(ctx, args.SessionID)
		if err != nil {
			return nil, subagentReadResult{}, err
		}
		res := subagentReadResult{
			SessionID: node.SessionID,
			Role:      node.Role,
			CLI:       node.CLI,
			Branch:    node.Branch,
			Source:    src,
			Response:  txt,
		}
		if args.IncludeDiff && node.WorkingDir != "" {
			if out, err := gitStatusShort(ctx, node.WorkingDir); err == nil {
				res.GitSummary = out
			}
		}
		return nil, res, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_subagent_stop",
		Description: "Interrupt ('interrupt') or close ('close', default) a sub-session in your orchestration tree, with optional safe Git worktree cleanup. Fail-closed against self-deletion.",
		Annotations: &mcp.ToolAnnotations{DestructiveHint: ptr(true)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args subagentStopArgs) (*mcp.CallToolResult, subagentStopResult, error) {
		caller := resolveCallerScope(ctx, "")
		targetID := strings.TrimSpace(args.SessionID)
		if caller.SessionID != "" && (strings.HasPrefix(targetID, caller.SessionID) || strings.HasPrefix(caller.SessionID, targetID)) {
			return nil, subagentStopResult{}, errors.New("refusing to stop or delete your own session")
		}
		node, err := orchCanTarget(caller, targetID)
		if err != nil {
			return nil, subagentStopResult{}, err
		}
		mode := strings.ToLower(strings.TrimSpace(args.Mode))
		if mode == "interrupt" {
			if err := codemanInterruptSession(ctx, node.SessionID); err != nil {
				return nil, subagentStopResult{}, err
			}
			return nil, subagentStopResult{SessionID: node.SessionID, Action: "interrupted"}, nil
		}
		if err := codemanDeleteSession(ctx, node.SessionID, true); err != nil {
			// If Codeman already dropped it, still mark stopped locally.
			if !strings.Contains(strings.ToLower(err.Error()), "not_found") && !strings.Contains(strings.ToLower(err.Error()), "not found") {
				return nil, subagentStopResult{}, err
			}
		}
		orchMarkStopped(node.SessionID)
		res := subagentStopResult{SessionID: node.SessionID, Action: "closed"}
		if args.CleanupWorktree && node.WorktreePath != "" && node.BaseRepo != "" {
			if err := removeLoomWorktree(ctx, node.BaseRepo, node.WorktreePath, args.ForceWorktree); err != nil {
				res.WorktreeError = err.Error()
			} else {
				res.WorktreeRemoved = true
				orchClearWorktree(node.SessionID)
			}
		}
		return nil, res, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_board_post",
		Description: "Post a concise status update, question, handoff or final result on the shared coordination board of your orchestration tree.",
		Annotations: &mcp.ToolAnnotations{OpenWorldHint: ptr(false)},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args boardPostArgs) (*mcp.CallToolResult, BoardMessage, error) {
		caller := resolveCallerScope(ctx, args.RootID)
		rootID := caller.RootID
		if rootID == "" {
			rootID = strings.TrimSpace(args.RootID)
		}
		if rootID == "" {
			return nil, BoardMessage{}, errors.New("could not determine orchestration tree root_id")
		}
		role := "agent"
		if n, ok := orchGetNode(caller.SessionID); ok && n.Role != "" {
			role = n.Role
		} else if caller.Depth == 0 && caller.SessionID != "" {
			role = "orchestrator"
		}
		if strings.TrimSpace(args.ToSession) != "" {
			if _, err := orchCanTarget(caller, args.ToSession); err != nil {
				return nil, BoardMessage{}, err
			}
		}
		msg, err := orchPostBoard(BoardMessage{
			RootID:      rootID,
			FromSession: caller.SessionID,
			FromRole:    role,
			ToSession:   strings.TrimSpace(args.ToSession),
			Kind:        args.Kind,
			Text:        args.Text,
		})
		return nil, msg, err
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "loom_board_read",
		Description: "Read recent messages posted on the shared coordination board of your orchestration tree.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, args boardReadArgs) (*mcp.CallToolResult, boardReadResult, error) {
		caller := resolveCallerScope(ctx, args.RootID)
		rootID := caller.RootID
		if rootID == "" {
			rootID = strings.TrimSpace(args.RootID)
		}
		if rootID == "" {
			return nil, boardReadResult{}, errors.New("could not determine orchestration tree root_id")
		}
		msgs := orchReadBoard(rootID, args.SinceID, args.Limit)
		return nil, boardReadResult{RootID: rootID, Messages: msgs}, nil
	})
}
