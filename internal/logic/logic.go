package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tazzo/mnemosyne-mcp-server/internal/db"
	"github.com/tazzo/mnemosyne-mcp-server/internal/embedding"
)

type IngestRequest struct {
	TraceID   string
	Content   string
	Timestamp time.Time
}

type Controller struct {
	db    *db.DB
	embed *embedding.Client

	// Cache per deduplicazione veloce (Session-aware) - legacy, maintained for quick checks
	cache   map[string]time.Time
	cacheMu sync.RWMutex

	// Worker Pool per ingestione asincrona
	ingestQueue chan IngestRequest
}

func New(database *db.DB, embedClient *embedding.Client) *Controller {
	c := &Controller{
		db:          database,
		embed:       embedClient,
		cache:       make(map[string]time.Time),
		ingestQueue: make(chan IngestRequest, 100),
	}

	// T1.2: Avvia il worker in background
	go c.startWorkerPool()

	return c
}

func (c *Controller) startWorkerPool() {
	for req := range c.ingestQueue {
		// Elabora la richiesta in modo seriale
		c.processIngestTask(req)
	}
}

// IngestMemory acts as the Producer (T1.3)
func (c *Controller) IngestMemory(content string, ts time.Time, traceID string) error {
	req := IngestRequest{
		TraceID:   traceID,
		Content:   content,
		Timestamp: ts,
	}

	select {
	case c.ingestQueue <- req:
		slog.Info("Memory queued for ingestion", "trace_id", traceID, "queue_len", len(c.ingestQueue))
		return nil
	default:
		slog.Error("Ingestion queue is full", "trace_id", traceID)
		return fmt.Errorf("ingestion queue is full (Service Unavailable)")
	}
}

func (c *Controller) processIngestTask(req IngestRequest) {
	log := slog.With("trace_id", req.TraceID, "component", "logic")
	log.Info("Worker processing memory", "content_len", len(req.Content))

	// 1. Prepara il blocco unico di conoscenza
	composite := fmt.Sprintf("DATE: %s\n%s", req.Timestamp.Format(time.RFC3339), req.Content)

	// 2. Deduplicazione tramite Hash
	hash := sha256.Sum256([]byte(composite))
	hashStr := hex.EncodeToString(hash[:])
	log = log.With("hash", hashStr[:8])

	// Fast in-memory check
	c.cacheMu.Lock()
	if lastSeen, exists := c.cache[hashStr]; exists {
		if time.Since(lastSeen) < 10*time.Minute {
			c.cacheMu.Unlock()
			log.Info("Memory already saved recently (in-memory), skipping")
			return
		}
	}
	c.cache[hashStr] = time.Now()
	c.cacheMu.Unlock()

	// T1.4: DB-First Idempotency Check
	dbExistsStart := time.Now()
	exists, err := c.db.CheckMemoryExists(hashStr)
	if err != nil {
		log.Error("Failed to check DB for idempotency", "error", err)
		// Continue anyway to try inserting
	} else if exists {
		log.Info("Memory already exists in DB, skipping embedding", "latency_ms", time.Since(dbExistsStart).Milliseconds())
		// Aggiorniamo comunque il last_seen_at
		err = c.db.InsertMemory(req.Timestamp, composite, []float32{0}, hashStr) // Il vector viene ignorato o triggera ON CONFLICT update
		if err != nil {
			log.Warn("Failed to update last_seen_at for existing memory", "error", err)
		}
		return
	}

	// 3. Vettorizzazione
	log.Info("Calling embedding API...")
	embedStart := time.Now()
	vector, err := c.embed.GetEmbedding(composite)
	if err != nil {
		log.Error("Embedding FAILED", "error", err)
		// Retry logic could go here
		return
	}
	log.Info("Embedding retrieved", "latency_ms", time.Since(embedStart).Milliseconds())

	// 4. Salvataggio nel DB
	dbStart := time.Now()
	if err := c.db.InsertMemory(req.Timestamp, composite, vector, hashStr); err != nil {
		log.Error("DB Insert FAILED", "error", err)
		return
	}
	log.Info("DB Insert SUCCESS", "latency_ms", time.Since(dbStart).Milliseconds())
}

func (c *Controller) DeleteMemory(id string) error {
	slog.Info("Deleting memory", "id", id, "component", "logic")
	return c.db.DeleteMemory(id)
}

func (c *Controller) ListMemories(limit int) ([]db.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	slog.Info("Listing recent memories", "limit", limit, "component", "logic")
	return c.db.List(limit)
}

func (c *Controller) SearchMemories(query string, limit int, daysBack int, startStr, endStr string) ([]db.Memory, error) {
	slog.Info("Searching memories", "query", query, "limit", limit, "component", "logic")
	// 1. Vettorizzazione query
	vector, err := c.embed.GetEmbedding(query)
	if err != nil {
		return nil, err
	}

	// 2. Gestione Filtri Temporali
	var start, end *time.Time

	if daysBack > 0 {
		t := time.Now().AddDate(0, 0, -daysBack)
		start = &t
	}

	if startStr != "" {
		t, err := time.Parse("2006-01-02", startStr)
		if err == nil {
			start = &t
		}
	}

	if endStr != "" {
		t, err := time.Parse("2006-01-02", endStr)
		if err == nil {
			end = &t
		}
	}

	// 3. Ricerca Semantica
	return c.db.Search(vector, limit, start, end)
}

// GetMemory T2.2: Retrieve a single memory by ID
func (c *Controller) GetMemory(id string) (string, error) {
	slog.Info("Getting single memory", "id", id, "component", "logic")
	return c.db.GetMemoryByID(id)
}
