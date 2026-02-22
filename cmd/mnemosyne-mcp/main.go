package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"tazlab/mnemosyne-mcp-server/internal/db"
	"tazlab/mnemosyne-mcp-server/internal/embedding"
	"tazlab/mnemosyne-mcp-server/internal/logic"
	"tazlab/mnemosyne-mcp-server/internal/mcp"
)

func main() {
	// 1. Caricamento Configurazione
	dbHost := getEnv("DB_HOST", "192.168.1.241")
	dbPort := getEnv("DB_PORT", "5432")
	dbUser := getEnv("DB_USER", "mnemosyne")
	dbPass := getEnv("DB_PASS", "dyUuu54TOA8zGMkc)4JFNLYF")
	dbName := getEnv("DB_NAME", "mnemosyne")
	
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		// Fallback per test locale se non in container
		if data, err := os.ReadFile("/home/tazpod/secrets/gemini-api-key"); err == nil {
			apiKey = string(data)
		}
	}
	// Pulizia aggressiva della chiave (rimuove apici e spazi)
	apiKey = strings.Trim(strings.TrimSpace(apiKey), "\"'")

	// 2. Inizializzazione Layer
	database, err := db.New(dbHost, dbPort, dbUser, dbPass, dbName)
	if err != nil {
		// In modalit\u00e0 Stdio non possiamo loggare su stderr durante il bootstrap
		os.Exit(1)
	}
	defer database.Close()

	embedClient := embedding.New(apiKey)
	controller := logic.New(database, embedClient)
	mcpServer := mcp.NewServer(controller)

	// 3. Selezione Trasporto (SSE o Stdio)
	transport := getEnv("MCP_TRANSPORT", "stdio")

	if transport == "stdio" {
		mcpServer.ServeStdio() 
		return
	}

	// 4. Avvio del background worker (solo per SSE asincrono)
	mcpServer.StartWorker()

	// 5. Handlers HTTP per trasporto SSE (Remote MCP)
	http.HandleFunc("/sse", mcpServer.HandleSSE)
	http.HandleFunc("/message", mcpServer.HandleMessage)

	// 5. Graceful Shutdown setup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 6. Avvio Server HTTP
	port := getEnv("PORT", "8080")
	srv := &http.Server{Addr: ":" + port}

	fmt.Fprintf(os.Stderr, "🧠 Mnemosyne MCP Server (SSE) starting on port %s...\n", port)
	
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "❌ HTTP server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-sigChan
	fmt.Fprintf(os.Stderr, "\n🔒 Shutting down gracefully...\n")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
