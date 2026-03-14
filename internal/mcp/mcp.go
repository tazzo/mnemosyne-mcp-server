package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tazzo/mnemosyne-mcp-server/internal/logic"
)

const ServerVersion = "1.1.3-sdk"

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
	list := mcp.NewTool("list_memories", mcp.WithDescription("List recent memory IDs and titles"))
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

	// update_blueprint
	updateBP := mcp.NewTool("update_blueprint", mcp.WithDescription("Update extraction protocol"))
	updateBP.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{"content": map[string]any{"type": "string"}},
		Required: []string{"content"},
	}
	s.mcp.AddTool(updateBP, s.handleUpdateBlueprint)

	// get_blueprint
	getBP := mcp.NewTool("get_blueprint", mcp.WithDescription("Retrieve current protocol"))
	getBP.InputSchema = mcp.ToolInputSchema{Type: "object"}
	s.mcp.AddTool(getBP, s.handleGetBlueprint)
}

func (s *Server) handleRetrieve(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	query, _ := args["query"].(string)
	memories, err := s.controller.SearchMemories(query, 5, 0, "", "")
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
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
		title := extractTitle(m.Content)
		res += fmt.Sprintf("ID: %s | Date: %s | Title: %s\n", m.ID, m.Timestamp.Format("2006-01-02"), title)
	}
	return mcp.NewToolResultText(res), nil
}

func (s *Server) handleDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	id, _ := args["id"].(string)
	err := s.controller.DeleteMemory(id)
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	return mcp.NewToolResultText("✅ Deleted"), nil
}

func (s *Server) handleUpdateBlueprint(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, _ := request.Params.Arguments.(map[string]any)
	content, _ := args["content"].(string)
	err := s.controller.UpdateBlueprint(content)
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	return mcp.NewToolResultText("✅ Blueprint updated."), nil
}

func (s *Server) handleGetBlueprint(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	bp, err := s.controller.GetBlueprint()
	if err != nil { return mcp.NewToolResultError(err.Error()), nil }
	return mcp.NewToolResultText(bp), nil
}

func (s *Server) ServeStdio() { server.ServeStdio(s.mcp) }

func (s *Server) ServeHTTP(port string) {
	streamable := server.NewStreamableHTTPServer(s.mcp)
	http.Handle("/mcp", streamable)
	http.ListenAndServe(":"+port, nil)
}

func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "TITLE: ") {
			return strings.TrimPrefix(line, "TITLE: ")
		}
	}
	// Fallback se il titolo non è nel formato V9/V10
	if len(content) > 50 {
		return content[:50] + "..."
	}
	return content
}

func (s *Server) StartWorker() {}
