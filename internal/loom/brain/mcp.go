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
type ContextReader interface {
	SearchContext(context.Context, SearchRequest) ([]Hit, error)
	PackContext(context.Context, PackRequest) (Pack, error)
}
type SearchResult struct {
	Hits []Hit `json:"hits"`
}

func MCPServer(reader Reader) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{Name: "loom-brain", Version: "1.0.0"}, nil)
	closed := false
	open := true
	searchAnnotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &open}
	annotations := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: &closed}
	mcp.AddTool(s, &mcp.Tool{Name: "brain_search", Description: "Search Brain context with BM25 and optional, explicitly enabled semantic embeddings (local by default; a cloud source requires prior consent). Optional project_id restricts sources and conversation paths to that project’s explicit selections. Personal notes require personal=true AND their explicit source IDs. limit defaults to 10, maximum 100. Highlights are Unicode code point ranges.", Annotations: searchAnnotations}, func(ctx context.Context, req *mcp.CallToolRequest, args SearchRequest) (*mcp.CallToolResult, SearchResult, error) {
		var hits []Hit
		var err error
		if contextual, ok := reader.(ContextReader); ok {
			hits, err = contextual.SearchContext(ctx, args)
		} else {
			hits, err = reader.Search(args)
		}
		return nil, SearchResult{hits}, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "brain_pack", Description: "Build a cited context pack for a query. budget_tokens defaults to 1500, maximum 8000, including citation/header overhead; estimate is ceil(characters/4). Optional project_id restricts sources and conversation paths to that project’s explicit selections. Personal notes require personal=true AND their explicit source IDs.", Annotations: searchAnnotations}, func(ctx context.Context, req *mcp.CallToolRequest, args PackRequest) (*mcp.CallToolResult, Pack, error) {
		var pack Pack
		var err error
		if contextual, ok := reader.(ContextReader); ok {
			pack, err = contextual.PackContext(ctx, args)
		} else {
			pack, err = reader.Pack(args)
		}
		return nil, pack, err
	})
	mcp.AddTool(s, &mcp.Tool{Name: "brain_read", Description: "Read a full indexed chunk by chunk_id, or source + relative path + heading (array). Ambiguous headings require chunk_id. Optional project_id restricts sources and conversation paths to that project’s explicit selections. Personal notes require personal=true AND an explicit source ID in source or sources, even with a known chunk_id.", Annotations: annotations}, func(ctx context.Context, req *mcp.CallToolRequest, args ReadRequest) (*mcp.CallToolResult, Chunk, error) {
		chunk, err := reader.Read(args)
		return nil, chunk, err
	})
	return s
}
