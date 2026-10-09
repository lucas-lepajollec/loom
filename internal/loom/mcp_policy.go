package loom

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/lucas-lepajollec/loom/internal/loom/policy"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Launch-scoped tokens bind agent gateway calls to the live Loom discussion.
// They expire with that run, carry no provider credential, and are not portable.
func scopedGatewayToken(id string) string {
	body := base64.RawURLEncoding.EncodeToString([]byte(id))
	mac := hmac.New(sha256.New, []byte(sessionGatewayToken))
	mac.Write([]byte(body))
	return "session." + body + "." + hex.EncodeToString(mac.Sum(nil))
}
func gatewayPolicyContext(r *http.Request) (context.Context, bool) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "session" || len(token) > 1024 {
		return r.Context(), false
	}
	id, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return r.Context(), false
	}
	if !hmac.Equal([]byte(token), []byte(scopedGatewayToken(string(id)))) {
		return r.Context(), false
	}
	workspaceSessions.mu.Lock()
	run := workspaceSessions.runs[string(id)]
	var s RuntimeSession
	if run != nil {
		s = cloneRuntimeSession(run.session)
	}
	workspaceSessions.mu.Unlock()
	if run == nil || !usageVaultAccessStream() {
		return r.Context(), false
	}
	return withPolicySession(r.Context(), workspaceSessions, s), true
}
func gatewayPolicyMiddleware(subjects map[string]string) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "tools/call" {
				var params struct {
					Name      string `json:"name"`
					Arguments struct {
						Path    string `json:"path"`
						File    string `json:"file"`
						Command string `json:"command"`
					} `json:"arguments"`
				}
				raw, _ := json.Marshal(req.GetParams())
				_ = json.Unmarshal(raw, &params)
				subject := subjects[params.Name]
				if subject == "" {
					subject = "mcp.tool:loom/" + params.Name
				}
				m, session := policyTurn(ctx)
				in := sessionPolicyInput(session, subject, policy.Allow)
				in.Path, in.Command = params.Arguments.Path, params.Arguments.Command
				if in.Path == "" {
					in.Path = params.Arguments.File
				}
				if err := m.authorizePolicy(ctx, in, false); err != nil {
					return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil
				}
			}
			return next(ctx, method, req)
		}
	}
}
