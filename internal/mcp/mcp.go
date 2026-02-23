package mcp

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tazzo/mnemosyne-mcp-server/internal/logic"
)

const ServerVersion = "1.1.1-sdk"

type Server struct {
	mcp        *server.MCPServer
	controller *logic.Controller
}

func NewServer(ctrl *logic.Controller) *Server {
	s := server.NewMCPServer(
		"mnemosyne-mcp",
		ServerVersion,
	)

	mcpServer := &Server{
		mcp:        s,
		controller: ctrl,
	}

	mcpServer.registerTools()
	return mcpServer
}

func (s *Server) registerTools() {
	// retrieve_memories
	retrieve := mcp.NewTool("retrieve_memories", mcp.WithDescription("Search semantic memory"))
	retrieve.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{"query": map[string]any{"type": "string"}},
		Required: []string{"query"},
	}
	s.mcp.AddTool(retrieve, s.handleRetrieve)

	// ingest_memory
	ingest := mcp.NewTool("ingest_memory", mcp.WithDescription("Save a technical chronicle"))
	ingest.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"content":   map[string]any{"type": "string"},
			"timestamp": map[string]any{"type": "string"},
		},
		Required: []string{"content", "timestamp"},
	}
	s.mcp.AddTool(ingest, s.handleIngest)

	// list_memories
	list := mcp.NewTool("list_memories", mcp.WithDescription("List recent memory IDs"))
	list.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{"limit": map[string]any{"type": "integer"}},
	}
	s.mcp.AddTool(list, s.handleList)

	// delete_memory
	del := mcp.NewTool("delete_memory", mcp.WithDescription("Delete a specific memory by UUID"))
	del.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{"id": map[string]any{"type": "string"}},
		Required: []string{"id"},
	}
	s.mcp.AddTool(del, s.handleDelete)
}

func (s *Server) handleRetrieve(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	query, _ := args["query"].(string)
	memories, err := s.controller.SearchMemories(query, 5, 0, "", "")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var res string
	for _, m := range memories {
		res += fmt.Sprintf("\n--- [%s] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
	}
	return mcp.NewToolResultText(res), nil
}

func (s *Server) handleIngest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	content, _ := args["content"].(string)
	tsStr, _ := args["timestamp"].(string)
	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil { ts = time.Now() }
	err = s.controller.IngestMemory(content, ts)
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	return mcp.NewToolResultText("✅ OK"), nil
}

func (s *Server) handleList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	limit := 10
	if l, ok := args["limit"].(float64); ok { limit = int(l) }
	memories, err := s.controller.ListMemories(limit)
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	var res string
	for _, m := range memories {
		res += fmt.Sprintf("ID: %s | Date: %s\n", m.ID, m.Timestamp.Format("2006-01-02"))
	}
	return mcp.NewToolResultText(res), nil
}

func (s *Server) handleDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	id, _ := args["id"].(string)
	err := s.controller.DeleteMemory(id)
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	return mcp.NewToolResultText(fmt.Sprintf("✅ Memory %s deleted.", id)), nil
}

func (s *Server) ServeStdio() {
	server.ServeStdio(s.mcp)
}

func (s *Server) ServeSSE(port string) {
	sse := server.NewSSEServer(s.mcp, server.WithBaseURL("http://192.168.1.240:8004"))
	http.Handle("/sse", sse.SSEHandler())
	http.Handle("/message", sse.MessageHandler())
	http.ListenAndServe(":"+port, nil)
}

func (s *Server) StartWorker() {}
