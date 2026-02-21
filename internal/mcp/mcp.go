package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"tazlab/mnemosyne-mcp-server/internal/logic"
	"time"
)

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
	clients    map[string]chan string
	mu         sync.RWMutex
}

func NewServer(ctrl *logic.Controller) *Server {
	return &Server{
		controller: ctrl,
		clients:    make(map[string]chan string),
	}
}

// Handler per l'endpoint SSE (/sse)
func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = "default"
	}

	messageChan := make(chan string, 10)
	s.mu.Lock()
	s.clients[sessionID] = messageChan
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, sessionID)
		s.mu.Unlock()
		close(messageChan)
	}()

	// Invia l'URL per i messaggi (endpoint richiesto dal protocollo MCP SSE)
	fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=%s\n\n", sessionID)
	w.(http.Flusher).Flush()

	for msg := range messageChan {
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		w.(http.Flusher).Flush()
	}
}

// Handler per l'endpoint dei messaggi (/message)
func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = "default"
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	// Processa la richiesta e ottiene la risposta
	resp := s.processRequest(req)
	
	// Invia la risposta al client tramite il canale SSE
	s.mu.RLock()
	clientChan, ok := s.clients[sessionID]
	s.mu.RUnlock()

	if ok {
		respJSON, _ := json.Marshal(resp)
		clientChan <- string(respJSON)
	}

	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) processRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities":    map[string]interface{}{},
				"serverInfo": map[string]string{
					"name":    "mnemosyne-mcp",
					"version": "1.0.0",
				},
			},
		}
	case "tools/list":
		tools := []map[string]interface{}{
			{
				"name":        "ingest_memory",
				"description": "Saves a detailed technical chronicle into semantic memory.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content":   map[string]string{"type": "string", "description": "The full markdown chronicle"},
						"timestamp": map[string]string{"type": "string", "description": "RFC3339 date"},
					},
					"required": []string{"content", "timestamp"},
				},
			},
			{
				"name":        "retrieve_memories",
				"description": "Search semantic memory for past solutions.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query":     map[string]string{"type": "string", "description": "Search query"},
						"limit":     map[string]string{"type": "integer"},
						"days_back": map[string]string{"type": "integer"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name":        "list_memories",
				"description": "List the most recent memories with their IDs.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": map[string]string{"type": "integer", "description": "Number of memories to list (default 10)"},
					},
				},
			},
			{
				"name":        "delete_memory",
				"description": "Delete a specific memory by its ID.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": map[string]string{"type": "integer", "description": "The numeric ID of the memory to delete"},
					},
					"required": []string{"id"},
				},
			},
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": tools}}
	
	case "tools/call":
		return s.handleToolCall(req)
	}

	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32601, "message": "Method not found"}}
}

func (s *Server) handleToolCall(req JSONRPCRequest) JSONRPCResponse {
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
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "✅ Memory ingested."}},
		}}

	case "retrieve_memories":
		query, _ := params.Arguments["query"].(string)
		limit, _ := params.Arguments["limit"].(float64)
		daysBack, _ := params.Arguments["days_back"].(float64)

		memories, err := s.controller.SearchMemories(query, int(limit), int(daysBack), "", "")
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("\n--- MEMORY [%d] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}

	case "list_memories":
		limitFloat, _ := params.Arguments["limit"].(float64)
		limit := int(limitFloat)
		memories, err := s.controller.ListMemories(limit)
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("ID: %d | Date: %s | Preview: %s...\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content[:100])
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}

	case "delete_memory":
		idFloat, _ := params.Arguments["id"].(float64)
		id := int64(idFloat)
		err := s.controller.DeleteMemory(id)
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("✅ Memory %d deleted.", id)}},
		}}
	}
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32601, "message": "Tool not found"}}
}
