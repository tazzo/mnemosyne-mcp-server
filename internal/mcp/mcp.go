package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
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

type Job struct {
	SessionID string
	Request   JSONRPCRequest
}

type Server struct {
	controller *logic.Controller
	clients    map[string]chan string
	jobChan    chan Job
	mu         sync.RWMutex
}

func NewServer(ctrl *logic.Controller) *Server {
	s := &Server{
		controller: ctrl,
		clients:    make(map[string]chan string),
		jobChan:    make(chan Job, 100),
	}
	// Avvio del background worker
	go s.worker()
	return s
}

func (s *Server) worker() {
	for job := range s.jobChan {
		fmt.Fprintf(os.Stderr, "👷 Worker: Processing job for session %s (Method: %s)\n", job.SessionID, job.Request.Method)
		// Processa la richiesta (una alla volta)
		resp := s.processRequest(job.Request)
		
		// Invia la risposta al client specifico tramite il suo canale SSE
		s.mu.RLock()
		clientChan, ok := s.clients[job.SessionID]
		s.mu.RUnlock()

		if ok {
			fmt.Fprintf(os.Stderr, "📤 Worker: Sending response to session %s\n", job.SessionID)
			respJSON, _ := json.Marshal(resp)
			select {
			case clientChan <- string(respJSON):
				fmt.Fprintf(os.Stderr, "✅ Worker: Response delivered to %s\n", job.SessionID)
			case <-time.After(10 * time.Second):
				fmt.Fprintf(os.Stderr, "⚠️ Worker: Timeout sending response to session %s\n", job.SessionID)
			}
		} else {
			fmt.Fprintf(os.Stderr, "❌ Worker: Client session %s not found for response\n", job.SessionID)
		}
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

	fmt.Fprintf(os.Stderr, "🔌 New SSE connection: sessionId=%s\n", sessionID)

	messageChan := make(chan string, 10)
	s.mu.Lock()
	s.clients[sessionID] = messageChan
	s.mu.Unlock()

	defer func() {
		fmt.Fprintf(os.Stderr, "🔌 SSE connection closed: sessionId=%s\n", sessionID)
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
		fmt.Fprintf(os.Stderr, "❌ Invalid JSON-RPC from %s: %v\n", sessionID, err)
		http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(os.Stderr, "📥 Message received: sessionId=%s, method=%s\n", sessionID, req.Method)

	// Mettiamo la richiesta in coda e rispondiamo subito 202
	s.jobChan <- Job{
		SessionID: sessionID,
		Request:   req,
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
			{
				"name":        "update_blueprint",
				"description": "Update the extraction protocol rules (blueprint) for future memories.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content": map[string]string{"type": "string", "description": "The new extraction blueprint in Markdown"},
					},
					"required": []string{"content"},
				},
			},
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": tools}}
	
	case "resources/list":
		resources := []map[string]interface{}{
			{
				"uri":         "resource://mnemosyne/blueprint",
				"name":        "Extraction Blueprint",
				"description": "Current TAZLAB protocol for memory extraction",
				"mimeType":    "text/markdown",
			},
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"resources": resources}}

	case "resources/read":
		var params struct {
			URI string `json:"uri"`
		}
		json.Unmarshal(req.Params, &params)
		if params.URI == "resource://mnemosyne/blueprint" {
			blueprint, _ := s.controller.GetBlueprint()
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
				"contents": []map[string]interface{}{
					{
						"uri":      params.URI,
						"mimeType": "text/markdown",
						"text":     blueprint,
					},
				},
			}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32602, "message": "Resource not found"}}

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
			resultText += fmt.Sprintf("ID: %s | Date: %s | Preview: %s...\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content[:100])
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}

	case "delete_memory":
		id, _ := params.Arguments["id"].(string)
		if id == "" {
			// Fallback if client sends numeric ID by mistake (though unlikely with UUIDs)
			if idFloat, ok := params.Arguments["id"].(float64); ok {
				id = fmt.Sprintf("%.0f", idFloat)
			}
		}
		err := s.controller.DeleteMemory(id)
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("✅ Memory %s deleted.", id)}},
		}}

	case "update_blueprint":
		content, _ := params.Arguments["content"].(string)
		if content == "" {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32602, "message": "Content is required"}}
		}
		err := s.controller.UpdateBlueprint(content)
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "✅ Extraction blueprint updated in database."}},
		}}
	}
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32601, "message": "Tool not found"}}
}
