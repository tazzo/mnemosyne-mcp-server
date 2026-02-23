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
	fmt.Fprintf(os.Stderr, "🚀 Worker active\n")
	for job := range s.jobChan {
		resp := s.processRequest(job.Request)
		s.mu.RLock()
		clientChan, ok := s.clients[job.SessionID]
		s.mu.RUnlock()

		if ok {
			respJSON, _ := json.Marshal(resp)
			select {
			case clientChan <- string(respJSON):
				fmt.Fprintf(os.Stderr, "✅ Sent to %s\n", job.SessionID)
			case <-time.After(5 * time.Second):
				fmt.Fprintf(os.Stderr, "⏰ Drop %s\n", job.SessionID)
			}
		}
	}
}

func (s *Server) HandleSSE(w http.ResponseWriter, r *http.Request) {
	// ID sempre univoco
	sessionID := uuid.New().String()

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
		s.mu.Lock()
		delete(s.clients, sessionID)
		s.mu.Unlock()
	}()

	host := r.Host
	if host == "" { host = "192.168.1.240:8004" }
	endpointURL := fmt.Sprintf("http://%s/message?sessionId=%s", host, sessionID)
	
	// Invia subito l'endpoint
	fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", endpointURL)
	w.(http.Flusher).Flush()

	// Heartbeat goroutine per rompere il buffering
	stopHeartbeat := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				fmt.Fprintf(w, ": heartbeat\n\n")
				w.(http.Flusher).Flush()
			case <-stopHeartbeat:
				return
			}
		}
	}()
	defer close(stopHeartbeat)

	for msg := range messageChan {
		fmt.Fprintf(w, "event: message\ndata: %s\n\n", msg)
		w.(http.Flusher).Flush()
	}
}

func (s *Server) HandleMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("sessionId")
	var req JSONRPCRequest
	json.NewDecoder(r.Body).Decode(&req)
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
				"capabilities": map[string]interface{}{"tools": map[string]interface{}{}},
				"serverInfo": map[string]string{"name": "mnemosyne-mcp", "version": "1.0.0"},
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
	return JSONRPCResponse{JSONRPC: "2.0", ID: req.ID, Error: map[string]interface{}{"message": "Not found"}}
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
