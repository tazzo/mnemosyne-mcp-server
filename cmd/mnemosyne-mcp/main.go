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
	logFormat := os.Getenv("LOG_FORMAT")
	var handler slog.Handler
	if logFormat == "json" {
		handler = slog.NewJSONHandler(os.Stderr, nil)
	} else {
		handler = slog.NewTextHandler(os.Stderr, nil)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	slog.Info("Starting mnemosyne-mcp-server")

	// 1. Mandatory Configuration (Fail Fast)
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPass := os.Getenv("DB_PASS")
	dbName := os.Getenv("DB_NAME")
	apiKey := os.Getenv("GEMINI_API_KEY")

	// Verify all mandatory variables
	missing := []string{}
	if dbHost == "" {
		missing = append(missing, "DB_HOST")
	}
	if dbPort == "" {
		missing = append(missing, "DB_PORT")
	}
	if dbUser == "" {
		missing = append(missing, "DB_USER")
	}
	if dbPass == "" {
		missing = append(missing, "DB_PASS")
	}
	if dbName == "" {
		missing = append(missing, "DB_NAME")
	}
	if apiKey == "" {
		missing = append(missing, "GEMINI_API_KEY")
	}

	if len(missing) > 0 {
		slog.Error("Missing mandatory environment variables", "variables", strings.Join(missing, ", "))
		os.Exit(1)
	}

	// Clean API key
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

	// 3. Optional Configuration
	transport := os.Getenv("MCP_TRANSPORT")
	if transport == "" {
		transport = "stdio"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	if transport == "http" {
		mcpServer.ServeHTTP(port)
	} else {
		mcpServer.ServeStdio()
	}
}
