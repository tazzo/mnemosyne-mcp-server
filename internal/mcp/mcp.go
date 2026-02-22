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
	return &Server{
		controller: ctrl,
		clients:    make(map[string]chan string),
		jobChan:    make(chan Job, 100),
	}
}

func (s *Server) StartWorker() {
	go s.worker()
}

func (s *Server) worker() {
	fmt.Fprintf(os.Stderr, "🚀 Worker started and waiting for jobs...\n")
	for job := range s.jobChan {
		fmt.Fprintf(os.Stderr, "👷 Worker: PICKED UP job from session %s (Method: %s, ID: %v)\n", job.SessionID, job.Request.Method, job.Request.ID)
		
		// Processa la richiesta (una alla volta)
		start := time.Now()
		resp := s.processRequest(job.Request)
		duration := time.Since(start)
		
		fmt.Fprintf(os.Stderr, "🧠 Worker: logic.ProcessRequest finished in %v for session %s\n", duration, job.SessionID)
		
		// Invia la risposta al client specifico tramite il suo canale SSE
		s.mu.RLock()
		clientChan, ok := s.clients[job.SessionID]
		s.mu.RUnlock()

		if ok {
			fmt.Fprintf(os.Stderr, "📤 Worker: Attempting to send response to SSE channel for %s\n", job.SessionID)
			respJSON, _ := json.Marshal(resp)
			select {
			case clientChan <- string(respJSON):
				fmt.Fprintf(os.Stderr, "✅ Worker: Response successfully delivered to SSE channel for %s\n", job.SessionID)
			case <-time.After(15 * time.Second):
				fmt.Fprintf(os.Stderr, "⚠️ Worker: TIMEOUT (15s) while sending response to session %s - channel full?\n", job.SessionID)
			}
		} else {
			fmt.Fprintf(os.Stderr, "❌ Worker: ABORTED - Client session %s DISCONNECTED before response could be sent\n", job.SessionID)
		}
	}
}

// Handler per l'endpoint SSE (/sse)
func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = "default"
	}

	fmt.Fprintf(os.Stderr, "🔌 [SSE] Connection request: sessionId=%s, remoteAddr=%s\n", sessionID, r.RemoteAddr)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan string, 100) // Buffer più grande per evitare blocchi
	s.mu.Lock()
	s.clients[sessionID] = messageChan
	s.mu.Unlock()

	defer func() {
		fmt.Fprintf(os.Stderr, "🔌 [SSE] Connection CLOSED: sessionId=%s\n", sessionID)
		s.mu.Lock()
		delete(s.clients, sessionID)
		s.mu.Unlock()
		close(messageChan)
	}()

	// Invia l'URL per i messaggi (endpoint richiesto dal protocollo MCP SSE)
	// Torniamo all'URL relativo che è più standard per i proxy
	endpointURL := fmt.Sprintf("/message?sessionId=%s", sessionID)
	fmt.Fprintf(os.Stderr, "📡 [SSE] Sending RELATIVE endpoint event: %s\n", endpointURL)
	
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	w.(http.Flusher).Flush()

	fmt.Fprintf(os.Stderr, "👂 [SSE] Entering event loop for session %s\n", sessionID)
	for msg := range messageChan {
		fmt.Fprintf(os.Stderr, "📤 [SSE] Writing message event to wire for %s\n", sessionID)
		_, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ [SSE] Write error for session %s: %v\n", sessionID, err)
			return
		}
		w.(http.Flusher).Flush()
	}
}

// Handler per l'endpoint dei messaggi (/message)
func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		sessionID = "default"
	}

	if r.Method != http.MethodPost {
		fmt.Fprintf(os.Stderr, "🚫 [MSG] Rejected non-POST request from %s (Method: %s)\n", sessionID, r.Method)
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	fmt.Fprintf(os.Stderr, "📥 [MSG] Incoming request from %s (Size: %d bytes)\n", sessionID, r.ContentLength)

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "❌ [MSG] JSON Decode ERROR from %s: %v\n", sessionID, err)
		http.Error(w, "Invalid JSON-RPC", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(os.Stderr, "📥 [MSG] Request validated: sessionId=%s, method=%s, id=%v\n", sessionID, req.Method, req.ID)

	// Mettiamo la richiesta in coda
	select {
	case s.jobChan <- Job{SessionID: sessionID, Request: req}:
		fmt.Fprintf(os.Stderr, "📥 [MSG] Job ENQUEUED successfully for session %s\n", sessionID)
		w.WriteHeader(http.StatusAccepted)
	default:
		fmt.Fprintf(os.Stderr, "🚨 [MSG] Job Queue FULL! Dropping request from session %s\n", sessionID)
		http.Error(w, "Server too busy", http.StatusServiceUnavailable)
	}
}

func (s *Server) ServeStdio() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)

	// Logging di debug su file fisso per non sporcare stdout
	f, _ := os.OpenFile("/tmp/mcp-stdio.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	fmt.Fprintf(f, "\n🚀 [STDIO] Server starting at %v\n", time.Now())

	for {
		var req JSONRPCRequest
		if err := decoder.Decode(&req); err != nil {
			if err.Error() == "EOF" {
				fmt.Fprintf(f, "🔌 [STDIO] EOF received\n")
				return
			}
			fmt.Fprintf(f, "❌ [STDIO] Decode error: %v\n", err)
			continue
		}

		fmt.Fprintf(f, "📥 [STDIO] Received method: %s (ID: %v)\n", req.Method, req.ID)
		
		// Gestione Notifiche (niente ID -> niente risposta)
		if req.ID == nil {
			s.processRequest(req) // Esegue ma ignoriamo il ritorno
			continue
		}

		resp := s.processRequest(req)
		fmt.Fprintf(f, "📤 [STDIO] Sending response for ID: %v\n", req.ID)
		encoder.Encode(resp)
	}
}

func (s *Server) processRequest(req JSONRPCRequest) JSONRPCResponse {
	switch req.Method {
	case "initialize":
		return JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]interface{}{
					"tools":     map[string]interface{}{},
					"resources": map[string]interface{}{},
				},
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
						"id": map[string]string{"type": "string", "description": "The unique UUID of the memory to delete"},
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
		limit := 5
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}
		daysBack := 0
		if d, ok := params.Arguments["days_back"].(float64); ok {
			daysBack = int(d)
		}

		memories, err := s.controller.SearchMemories(query, limit, daysBack, "", "")
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"code": -32000, "message": err.Error()}}
		}
		
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("\n--- MEMORY [%s] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}

	case "list_memories":
		limit := 10
		if l, ok := params.Arguments["limit"].(float64); ok {
			limit = int(l)
		}
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
