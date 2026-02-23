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

const ServerVersion = "1.0.19-enterprise"

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
	fmt.Fprintf(os.Stderr, "🏗️  Initializing Mnemosyne MCP Server [%s]\n", ServerVersion)
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
	fmt.Fprintf(os.Stderr, "🚀 [%s] Worker active and listening for jobs\n", ServerVersion)
	for job := range s.jobChan {
		resp := s.processRequest(job.Request)
		s.mu.RLock()
		clientChan, ok := s.clients[job.SessionID]
		s.mu.RUnlock()

		if ok {
			respJSON, _ := json.Marshal(resp)
			select {
			case clientChan <- string(respJSON):
				fmt.Fprintf(os.Stderr, "✅ [%s] Response dispatched to session %s\n", ServerVersion, job.SessionID)
			case <-time.After(5 * time.Second):
				fmt.Fprintf(os.Stderr, "⏰ [%s] Dispatch timeout for session %s\n", ServerVersion, job.SessionID)
			}
		} else {
			fmt.Fprintf(os.Stderr, "⚠️ [%s] Worker discarded job: session %s not found in client registry\n", ServerVersion, job.SessionID)
		}
	}
}

func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// Forza ID univoco per evitare conflitti di routing SSE
	sessionID := uuid.New().String()

	fmt.Fprintf(os.Stderr, "🔌 [%s] New SSE Connection: %s (Remote: %s)\n", ServerVersion, sessionID, r.RemoteAddr)

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
		fmt.Fprintf(os.Stderr, "🔌 [%s] SSE Connection Closed: %s\n", ServerVersion, sessionID)
		s.mu.Lock()
		delete(s.clients, sessionID)
		s.mu.Unlock()
	}()

	// Invia endpoint assoluto
	host := r.Host
	if host == "" { host = "192.168.1.240:8004" }
	endpointURL := fmt.Sprintf("http://%s/message?sessionId=%s", host, sessionID)
	
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	w.(http.Flusher).Flush()

	// Heartbeat per rompere i buffer dei proxy
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat [%s]\n\n", ServerVersion)
				w.(http.Flusher).Flush()
			case <-stopHeartbeat:
				return
			}
		}
	}()
	defer close(stopHeartbeat)

	fmt.Fprintf(os.Stderr, "👂 [%s] SSE Loop started for %s\n", ServerVersion, sessionID)
	for msg := range messageChan {
		_, err := fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "❌ [%s] SSE Write Error (%s): %v\n", ServerVersion, sessionID, err)
			return
		}
		w.(http.Flusher).Flush()
		fmt.Fprintf(os.Stderr, "📤 [%s] SSE Message written to wire for %s\n", ServerVersion, sessionID)
	}
}

func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "Missing sessionId", http.StatusBadRequest)
		return
	}

	var req JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fmt.Fprintf(os.Stderr, "❌ [%s] JSON Decode Error from %s: %v\n", ServerVersion, sessionID, err)
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(os.Stderr, "📥 [%s] Received %s (ID: %v) from %s\n", ServerVersion, req.Method, req.ID, sessionID)

	select {
	case s.jobChan <- Job{SessionID: sessionID, Request: req}:
		w.WriteHeader(http.StatusAccepted)
	default:
		fmt.Fprintf(os.Stderr, "🚨 [%s] Job queue full, rejecting request from %s\n", ServerVersion, sessionID)
		http.Error(w, "Busy", http.StatusServiceUnavailable)
	}
}

func (s *Server) ServeStdio() {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	fmt.Fprintf(os.Stderr, "🚀 [%s] Stdio Server active\n", ServerVersion)
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
				"serverInfo": map[string]string{"name": "mnemosyne-mcp", "version": ServerVersion},
			},
		}
	case "tools/list":
		tools := []map[string]interface{}{
			{
				"name": "retrieve_memories",
				"description": "Search semantic memory.",
				"inputSchema": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{"query": {"type": "string"}},
					"required": []string{"query"},
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
	case "retrieve_memories":
		query, _ := params.Arguments["query"].(string)
		memories, _ := s.controller.SearchMemories(query, 5, 0, "", "")
		var resultText string
		for _, m := range memories {
			resultText += fmt.Sprintf("\n--- MEMORY [%s] ---\n%s\n", m.ID, m.Content)
		}
		return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": resultText}},
		}}
	}
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"message": "Tool not found"}}
}
