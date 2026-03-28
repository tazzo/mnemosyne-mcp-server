package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/tazzo/mnemosyne-mcp-server/internal/db"
	"github.com/tazzo/mnemosyne-mcp-server/internal/embedding"
	"github.com/tazzo/mnemosyne-mcp-server/internal/logic"
	"github.com/tazzo/mnemosyne-mcp-server/internal/mcp"
)

func main() {
	// Configure slog based on LOG_FORMAT environment variable
	logFormat := getEnv("LOG_FORMAT", "text")
	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		handler = slog.NewTextHandler(os.Stderr, nil)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// 1. Caricamento Configurazione
	slog.Info("Starting mnemosyne-mcp-server")
	dbHost := getEnv("DB_HOST", "tazlab-db-primary.tazlab-db.svc")
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
		slog.Error("Failed to initialize DB", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	embedClient := embedding.New(apiKey)
	controller := logic.New(database, embedClient)
	mcpServer := mcp.NewServer(controller)

	// 3. Selezione Trasporto (Default: Stdio per CLI nativa)
	transport := getEnv("MCP_TRANSPORT", "stdio")
	port := getEnv("PORT", "8080")

	if transport == "http" {
		mcpServer.ServeHTTP(port)
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
