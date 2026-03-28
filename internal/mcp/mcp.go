package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/tazzo/mnemosyne-mcp-server/internal/logic"
)

const ServerVersion = "1.2.0-sdk" // Bumped version for new async API

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
	// retrieve_memories (T2.1: limit added)
	retrieve := mcp.NewTool("retrieve_memories", mcp.WithDescription("Search semantic memory"))
	retrieve.InputSchema = mcp.ToolInputSchema{
		Type: "object",
		Properties: map[string]any{
			"query": map[string]any{"type": "string"},
			"limit": map[string]any{"type": "integer", "description": "Optional: Max results (default 5)"},
		},
		Required: []string{"query"},
	}
	s.mcp.AddTool(retrieve, s.handleRetrieve)

	// get_memory (T2.2: new tool)
	getMem := mcp.NewTool("get_memory", mcp.WithDescription("Retrieve full text of a specific memory by ID"))
	getMem.InputSchema = mcp.ToolInputSchema{
		Type:       "object",
		Properties: map[string]any{"id": map[string]any{"type": "string"}},
		Required:   []string{"id"},
	}
	s.mcp.AddTool(getMem, s.handleGetMemory)

	// ingest_memory (T2.4: return JSON)
	ingest := mcp.NewTool("ingest_memory", mcp.WithDescription("Save a technical chronicle. Returns status object."))
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
		Type:       "object",
		Properties: map[string]any{"limit": map[string]any{"type": "integer"}},
	}
	s.mcp.AddTool(list, s.handleList)

	// delete_memory
	del := mcp.NewTool("delete_memory", mcp.WithDescription("Delete a specific memory by UUID"))
	del.InputSchema = mcp.ToolInputSchema{
		Type:       "object",
		Properties: map[string]any{"id": map[string]any{"type": "string"}},
		Required:   []string{"id"},
	}
	s.mcp.AddTool(del, s.handleDelete)

	// T2.3: blueprint tools removed
}

func generateTraceID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) handleRetrieve(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	traceID := generateTraceID()
	log := slog.With("trace_id", traceID, "tool", "retrieve_memories")
	log.Info("Handling request")

	args, _ := request.Params.Arguments.(map[string]any)
	query, _ := args["query"].(string)

	limit := 5
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	memories, err := s.controller.SearchMemories(query, limit, 0, "", "")
	if err != nil {
		log.Error("Search failed", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	var res string
	for _, m := range memories {
		res += fmt.Sprintf("\n--- [%s] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
	}
	log.Info("Search successful", "results", len(memories))
	return mcp.NewToolResultText(res), nil
}

func (s *Server) handleGetMemory(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	traceID := generateTraceID()
	log := slog.With("trace_id", traceID, "tool", "get_memory")

	args, _ := request.Params.Arguments.(map[string]any)
	id, _ := args["id"].(string)
	log.Info("Handling request", "id", id)

	content, err := s.controller.GetMemory(id)
	if err != nil {
		log.Error("Failed to get memory", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	log.Info("Memory retrieved")
	return mcp.NewToolResultText(content), nil
}

func (s *Server) handleIngest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	traceID := generateTraceID()
	log := slog.With("trace_id", traceID, "tool", "ingest_memory")
	log.Info("Handling request")

	args, _ := request.Params.Arguments.(map[string]any)
	content, _ := args["content"].(string)
	tsStr, _ := args["timestamp"].(string)
	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		ts = time.Now()
	}

	err = s.controller.IngestMemory(content, ts, traceID)
	if err != nil {
		log.Error("Ingestion rejected", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	response := map[string]string{
		"status":     "queued",
		"request_id": traceID,
	}
	jsonBytes, _ := json.Marshal(response)

	return mcp.NewToolResultText(string(jsonBytes)), nil
}

func (s *Server) handleList(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	traceID := generateTraceID()
	log := slog.With("trace_id", traceID, "tool", "list_memories")
	log.Info("Handling request")

	args, _ := request.Params.Arguments.(map[string]any)
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	memories, err := s.controller.ListMemories(limit)
	if err != nil {
		log.Error("List failed", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}

	var res string
	for _, m := range memories {
		title := extractTitle(m.Content)
		res += fmt.Sprintf("ID: %s | Date: %s | Title: %s\n", m.ID, m.Timestamp.Format("2006-01-02"), title)
	}
	log.Info("List successful", "results", len(memories))
	return mcp.NewToolResultText(res), nil
}

func (s *Server) handleDelete(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	traceID := generateTraceID()
	log := slog.With("trace_id", traceID, "tool", "delete_memory")

	args, _ := request.Params.Arguments.(map[string]any)
	id, _ := args["id"].(string)
	log.Info("Handling request", "id", id)

	err := s.controller.DeleteMemory(id)
	if err != nil {
		log.Error("Delete failed", "error", err)
		return mcp.NewToolResultError(err.Error()), nil
	}
	log.Info("Delete successful")
	return mcp.NewToolResultText("✅ Deleted"), nil
}

func (s *Server) ServeStdio() { server.ServeStdio(s.mcp) }

func (s *Server) ServeHTTP(port string) {
	streamable := server.NewStreamableHTTPServer(s.mcp, server.WithEndpointPath("/mcp"))
	http.Handle("/", streamable)
	http.ListenAndServe(":"+port, nil)
}

func extractTitle(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "TITLE: ") {
			return strings.TrimPrefix(line, "TITLE: ")
		}
	}
	title := content
	if len(content) > 50 {
		title = content[:50]
	}
	return strings.ReplaceAll(title, "\n", " ")
}
