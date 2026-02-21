package main

import (
	"fmt"
	"net/http"
	"os"
	"os/signal"
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

	// 2. Inizializzazione Layer
	database, err := db.New(dbHost, dbPort, dbUser, dbPass, dbName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to DB: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()

	embedClient := embedding.New(apiKey)
	controller := logic.New(database, embedClient)
	mcpServer := mcp.NewServer(controller)

	// 3. Handlers HTTP per trasporto SSE (Remote MCP)
	http.HandleFunc("/sse", mcpServer.HandleSSE)
	http.HandleFunc("/message", mcpServer.HandleMessage)

	// 4. Graceful Shutdown setup
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 5. Avvio Server HTTP
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
