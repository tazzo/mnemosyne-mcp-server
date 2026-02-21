package main

import (
	"fmt"
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
		fmt.Fprintf(os.Stderr, "❌ Failed to connect to DB: %v
", err)
		os.Exit(1)
	}
	defer database.Close()

	embedClient := embedding.New(apiKey)
	controller := logic.New(database, embedClient)
	mcpServer := mcp.NewServer(controller)

	// 3. Graceful Shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 4. Avvio Server (Standard I/O per protocollo MCP)
	fmt.Fprintf(os.Stderr, "🧠 Mnemosyne MCP Server starting...
")
	
	go func() {
		mcpServer.Serve()
	}()

	<-sigChan
	fmt.Fprintf(os.Stderr, "
🔒 Shutting down gracefully...
")
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
