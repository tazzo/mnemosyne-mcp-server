package main

import (
	"os"
	"strings"

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
		if data, err := os.ReadFile("/home/tazpod/secrets/gemini-api-key"); err == nil {
			apiKey = string(data)
		}
	}
	apiKey = strings.Trim(strings.TrimSpace(apiKey), "\"'")

	// 2. Inizializzazione Layer
	database, err := db.New(dbHost, dbPort, dbUser, dbPass, dbName)
	if err != nil {
		os.Exit(1)
	}
	defer database.Close()

	embedClient := embedding.New(apiKey)
	controller := logic.New(database, embedClient)
	mcpServer := mcp.NewServer(controller)

	// 3. Selezione Trasporto (Default: Stdio per CLI nativa)
	transport := getEnv("MCP_TRANSPORT", "stdio")
	port := getEnv("PORT", "8080")

	if transport == "sse" {
		mcpServer.ServeSSE(port)
	} else {
		mcpServer.ServeStdio()
	}
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
