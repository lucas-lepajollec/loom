// Package openai owns the bounded OpenAI-compatible Chat Completions text
// protocol: endpoint validation, request construction, SSE parsing and reported
// token presence, plus manual-price cost arithmetic. Loom supplies resolved
// provider settings, credentials and an HTTP client through small interfaces.
// It has no runtime globals, persistence, registration or application HTTP actions.
package openai
