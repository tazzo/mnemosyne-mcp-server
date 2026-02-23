package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"tazlab/mnemosyne-mcp-server/internal/logic"
)

const ServerVersion = "1.1.0-sdk"

type Server struct {
	mcp        *server.MCPServer
	controller *logic.Controller
}

func NewServer(ctrl *logic.Controller) *Server {
	s := server.NewMCPServer(
		"mnemosyne-mcp",
		ServerVersion,
		server.WithResourceCapabilities(true, false),
		server.WithToolCapabilities(true),
	)

	mcpServer := &Server{
		mcp:        s,
		controller: ctrl,
	}

	mcpServer.registerTools()
	return mcpServer
}

func (s *Server) registerTools() {
	// Tool: retrieve_memories
	retrieveTool := mcp.NewTool("retrieve_memories",
		mcp.WithDescription("Search semantic memory for past solutions and technical chronicles."),
		mcp.WithSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The technical query to search for",
				},
			},
			"required": []string{"query"},
		}),
	)

	s.mcp.AddTool(retrieveTool, s.handleRetrieve)

	// Tool: ingest_memory
	ingestTool := mcp.NewTool("ingest_memory",
		mcp.WithDescription("Save a detailed technical chronicle into semantic memory."),
		mcp.WithSchema(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Full markdown chronicle",
				},
				"timestamp": map[string]interface{}{
					"type":        "string",
					"description": "RFC3339 timestamp",
				},
			},
			"required": []string{"content", "timestamp"},
		}),
	)

	s.mcp.AddTool(ingestTool, s.handleIngest)
}

func (s *Server) handleRetrieve(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	query, ok := request.Params.Arguments["query"].(string)
	if !ok {
		return mcp.NewToolResultError("missing query argument"), nil
	}

	memories, err := s.controller.SearchMemories(query, 5, 0, "", "")
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("search failed: %v", err)), nil
	}

	var resultText string
	for _, m := range memories {
		resultText += fmt.Sprintf("\n--- MEMORY [%s] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
	}

	return mcp.NewToolResultText(resultText), nil
}

func (s *Server) handleIngest(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	content, _ := request.Params.Arguments["content"].(string)
	tsStr, _ := request.Params.Arguments["timestamp"].(string)
	
	ts, err := time.Parse(time.RFC3339, tsStr)
	if err != nil {
		ts = time.Now()
	}

	err = s.controller.IngestMemory(content, ts)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("ingestion failed: %v", err)), nil
	}

	return mcp.NewToolResultText("✅ Memory ingested successfully."), nil
}

// ServeStdio lancia il server in modalità Stdio (nativa per Gemini CLI locale)
func (s *Server) ServeStdio() {
	fmt.Fprintf(os.Stderr, "🚀 Mnemosyne MCP [%s] starting Stdio mode...\n", ServerVersion)
	if err := server.ServeStdio(s.mcp); err != nil {
		fmt.Fprintf(os.Stderr, "❌ Stdio server error: %v\n", err)
	}
}

// ServeSSE lancia il server in modalità SSE (per il cluster Kubernetes)
func (s *Server) ServeSSE(port string) {
	fmt.Fprintf(os.Stderr, "🚀 Mnemosyne MCP [%s] starting SSE mode on port %s...\n", ServerVersion, port)
	sseServer := server.NewSSEServer(s.mcp, "http://192.168.1.240:8004")
	
	http.HandleFunc("/sse", sseServer.HandleSSE)
	http.HandleFunc("/message", sseServer.HandleMessage)

	if err := http.ListenAndServe(":"+port, nil); err != nil {
		fmt.Fprintf(os.Stderr, "❌ SSE server error: %v\n", err)
	}
}

// StartWorker non è più necessario perché l'SDK gestisce la concorrenza internamente
func (s *Server) StartWorker() {}
