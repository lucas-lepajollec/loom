package brain

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Reader lets the Loom boundary check availability for every tool invocation.
type Reader interface {
	Search(SearchRequest) ([]Hit, error)
	Pack(PackRequest) (Pack, error)
	Read(ReadRequest) (Chunk, error)
}
type SearchResult struct {
	Hits []Hit `json:"hits"`
}

func MCPServer(reader Reader) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "loom-brain", Version: "1.0.0"}, nil)
	closed := false
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed}
	mcp.AddTool(s, &mcp.Tool{Name: "brain_search", Description: "Search local Brain context. Personal notes require personal=true AND their explicit source IDs. limit defaults to 10, maximum 100. Highlights are Unicode code point ranges.", Annotations: annotations}, func(ctx context.Context, req *mcp.CallToolRequest, args SearchRequest) (*mcp.CallToolResult, SearchResult, error) {
		hits, err := reader.Search(args)
		return nil, SearchResult{hits}, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "brain_pack", Description: "Build a cited context pack for a query. budget_tokens defaults to 1500, maximum 8000, including citation/header overhead; estimate is ceil(characters/4). Personal notes require personal=true AND their explicit source IDs.", Annotations: annotations}, func(ctx context.Context, req *mcp.CallToolRequest, args PackRequest) (*mcp.CallToolResult, Pack, error) {
		pack, err := reader.Pack(args)
		return nil, pack, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "brain_read", Description: "Read a full indexed chunk by chunk_id, or source + relative path + heading (array). Ambiguous headings require chunk_id. Personal notes require personal=true AND an explicit source ID in source or sources, even with a known chunk_id.", Annotations: annotations}, func(ctx context.Context, req *mcp.CallToolRequest, args ReadRequest) (*mcp.CallToolResult, Chunk, error) {
		chunk, err := reader.Read(args)
		return nil, chunk, err
	})
	return s
}
