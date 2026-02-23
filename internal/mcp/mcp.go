package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"

	"github.com/google/uuid"
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
		fmt.Fprintf(os.Stderr, "👷 Worker: Processing session %s (Method: %s)\n", job.SessionID, job.Request.Method)
		
		resp := s.processRequest(job.Request)
		
		s.mu.RLock()
		clientChan, ok := s.clients[job.SessionID]
		s.mu.RUnlock()

		if ok {
			respJSON, _ := json.Marshal(resp)
			select {
			case clientChan <- string(respJSON):
				fmt.Fprintf(os.Stderr, "✅ Worker: Response delivered to %s\n", job.SessionID)
			case <-time.After(10 * time.Second):
				fmt.Fprintf(os.Stderr, "⚠️ Worker: Timeout for %s\n", job.SessionID)
			}
		}
	}
}

func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" || sessionID == "default" {
		sessionID = uuid.New().String()
	}

	fmt.Fprintf(os.Stderr, "🔌 [SSE] New Session: %s\n", sessionID)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	messageChan := make(chan string, 100)
	s.mu.Lock()
	s.clients[sessionID] = messageChan
	s.mu.Unlock()

	defer func() {
		fmt.Fprintf(os.Stderr, "🔌 [SSE] Closed: %s\n", sessionID)
		s.mu.Lock()
		delete(s.clients, sessionID)
		s.mu.Unlock()
	}()

	host := r.Host
	if host == "" { host = "192.168.1.240:8004" }
	endpointURL := fmt.Sprintf("http://%s/message?sessionId=%s", host, sessionID)
	
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	w.(http.Flusher).Flush()

	fmt.Fprintf(os.Stderr, "👂 [SSE] Loop started: %s\n", sessionID)
	for msg := range messageChan {
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		w.(http.Flusher).Flush()
	}
}

func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" { sessionID = "default" }

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	select {
	case s.jobChan <- Job{SessionID: sessionID, Request: req}:
		w.WriteHeader(http.StatusAccepted)
	default:
		http.Error(w, "Busy", http.StatusServiceUnavailable)
	}
}

func (s *Server) ServeStdio() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var req JSONRPCRequest
		if err := decoder.Decode(&req); err != nil { return }
		if req.ID == nil {
			s.processRequest(req)
			continue
		}
		resp := s.processRequest(req)
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
				"serverInfo": map[string]string{"name": "mnemosyne-mcp", "version": "1.0.0"},
			},
		}
	case "tools/list":
		tools := []map[string]interface{}{
			{
				"name": "retrieve_memories",
				"description": "Search semantic memory for past solutions.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"query": {"type": "string", "description": "Search query"},
					},
					"required": []string{"query"},
				},
			},
			{
				"name": "ingest_memory",
				"description": "Saves a detailed technical chronicle.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"content": {"type": "string"},
						"timestamp": {"type": "string"},
					},
					"required": []string{"content", "timestamp"},
				},
			},
			{
				"name": "list_memories",
				"description": "List recent memory IDs.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"limit": {"type": "integer"},
					},
				},
			},
			{
				"name": "delete_memory",
				"description": "Delete a specific memory.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"id": {"type": "string"},
					},
					"required": []string{"id"},
				},
			},
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": tools}}
	case "resources/list":
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"resources": []interface{}{}}}
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
	case "retrieve_memories":
		query, _ := params.Arguments["query"].(string)
		memories, err := s.controller.SearchMemories(query, 5, 0, "", "")
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"message": err.Error()}}
		}
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("\n--- MEMORY [%s] [%s] ---\n%s\n", m.ID, m.Timestamp.Format("2006-01-02"), m.Content)
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}
	case "ingest_memory":
		content, _ := params.Arguments["content"].(string)
		tsStr, _ := params.Arguments["timestamp"].(string)
		ts, _ := time.Parse(time.RFC3339, tsStr)
		if ts.IsZero() { ts = time.Now() }
		err := s.controller.IngestMemory(content, ts)
		if err != nil {
			return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"message": err.Error()}}
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "✅ OK"}}}}
	case "list_memories":
		memories, _ := s.controller.ListMemories(10)
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("ID: %s | Date: %s\n", m.ID, m.Timestamp.Format("2006-01-02"))
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": []map[string]string{{"type": "text", "text": resultText}}}}
	case "delete_memory":
		id, _ := params.Arguments["id"].(string)
		s.controller.DeleteMemory(id)
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"content": []map[string]string{{"type": "text", "text": "✅ Deleted"}}}}
	}
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"message": "Tool not found"}}
}
