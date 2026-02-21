package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"tazlab/mnemosyne-mcp-server/internal/logic"
	"time"
)

// Definizione schemi e messaggi MCP
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   interface{} `json:"error,omitempty"`
}

type Server struct {
	controller *logic.Controller
}

func NewServer(ctrl *logic.Controller) *Server {
	return &Server{controller: ctrl}
}

func (s *Server) Serve() {
	decoder := json.NewDecoder(os.Stdin)
	for {
		var req JSONRPCRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF { break }
			continue
		}
		s.handleRequest(req)
	}
}

func (s *Server) handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		s.sendResponse(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"serverInfo": map[string]string{
				"name":    "mnemosyne-mcp",
				"version": "1.0.0",
			},
		})
	case "tools/list":
		s.handleListTools(req)
	case "tools/call":
		s.handleCallTool(req)
	}
}

func (s *Server) handleListTools(req JSONRPCRequest) {
	tools := []map[string]interface{}{
		{
			"name":        "ingest_memory",
			"description": "Saves a detailed technical chronicle into semantic memory.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"content":   map[string]string{"type": "string", "description": "The full markdown chronicle (Objective, Artifacts, Failures, etc.)"},
					"timestamp": map[string]string{"type": "string", "description": "RFC3339 formatted date (e.g. 2026-02-20T10:00:00Z)"},
				},
				"required": []string{"content", "timestamp"},
			},
		},
		{
			"name":        "retrieve_memories",
			"description": "Search semantic memory for past solutions, architecture decisions or discussions.",
			"inputSchema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":     map[string]string{"type": "string", "description": "Semantic search query (e.g. 'how to fix S3 400 errors')"},
					"limit":     map[string]string{"type": "integer", "description": "Number of results (default 5)"},
					"days_back": map[string]string{"type": "integer", "description": "Search only in the last X days"},
				},
				"required": []string{"query"},
			},
		},
	}
	s.sendResponse(req.ID, map[string]interface{}{"tools": tools})
}

func (s *Server) handleCallTool(req JSONRPCRequest) {
	var params struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	json.Unmarshal(req.Params, &params)

	switch params.Name {
	case "ingest_memory":
		content, _ := params.Arguments["content"].(string)
		tsStr, _ := params.Arguments["timestamp"].(string)
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if ts.IsZero() { ts = time.Now() }

		err := s.controller.IngestMemory(content, ts)
		if err != nil {
			s.sendError(req.ID, -32000, err.Error())
		} else {
			s.sendResponse(req.ID, map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": "✅ Memory successfully ingested."}},
			})
		}

	case "retrieve_memories":
		query, _ := params.Arguments["query"].(string)
		limitFloat, _ := params.Arguments["limit"].(float64)
		limit := int(limitFloat); if limit == 0 { limit = 5 }
		daysFloat, _ := params.Arguments["days_back"].(float64)
		daysBack := int(daysFloat)

		memories, err := s.controller.SearchMemories(query, limit, daysBack, "", "")
		if err != nil {
			s.sendError(req.ID, -32000, err.Error())
		} else {
			var resultText string
			for _, m := range memories {
				resultText += fmt.Sprintf("
--- MEMORY [%s] ---
%s
", m.Timestamp.Format("2006-01-02"), m.Content)
			}
			if resultText == "" { resultText = "No memories found for this query." }
			s.sendResponse(req.ID, map[string]interface{}{
				"content": []map[string]string{{"type": "text", "text": resultText}},
			})
		}
	}
}

func (s *Server) sendResponse(id interface{}, result interface{}) {
	resp := JSONRPCResponse{JSONRPC: "2.0", ID: id, Result: result}
	json.NewEncoder(os.Stdout).Encode(resp)
}

func (s *Server) sendError(id interface{}, code int, message string) {
	resp := JSONRPCResponse{JSONRPC: "2.0", ID: id, Error: map[string]interface{}{"code": code, "message": message}}
	json.NewEncoder(os.Stdout).Encode(resp)
}
