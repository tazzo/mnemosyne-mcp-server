package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tazzo/mnemosyne-mcp-server/internal/db"
	"github.com/tazzo/mnemosyne-mcp-server/internal/embedding"
)

type Controller struct {
	db        *db.DB
	embed     *embedding.Client
	
	// Cache per deduplicazione (Session-aware)
	cache     map[string]time.Time
	cacheMu   sync.RWMutex

	// Mutex per serializzare le ingestioni (vettorizzazione lenta)
	ingestMu  sync.Mutex
}

func New(database *db.DB, embedClient *embedding.Client) *Controller {
	return &Controller{
		db:    database,
		embed: embedClient,
		cache: make(map[string]time.Time),
	}
}

func (c *Controller) IngestMemory(content string, ts time.Time) error {
	// Serializziamo l'ingestione per evitare di sovraccaricare le API di embedding
	c.ingestMu.Lock()
	defer c.ingestMu.Unlock()

	fmt.Fprintf(os.Stderr, "🧠 [LOGIC] IngestMemory started (Content length: %d)\n", len(content))

	// 1. Prepara il blocco unico di conoscenza
	// Content include già Titolo e Tag dal blueprint V9
	composite := fmt.Sprintf("DATE: %s\n%s", ts.Format(time.RFC3339), content)

	// 2. Deduplicazione tramite Hash
	hash := sha256.Sum256([]byte(composite))
	hashStr := hex.EncodeToString(hash[:])

	c.cacheMu.Lock()
	if lastSeen, exists := c.cache[hashStr]; exists {
		// Se visto negli ultimi 10 minuti, saltiamo (evita doppi salvataggi nella stessa sessione)
		if time.Since(lastSeen) < 10*time.Minute {
			c.cacheMu.Unlock()
			fmt.Fprintf(os.Stderr, "⏩ [LOGIC] Memory already saved recently (hash: %s), skipping.\n", hashStr[:8])
			return nil
		}
	}
	c.cache[hashStr] = time.Now()
	c.cacheMu.Unlock()

	// 3. Vettorizzazione
	fmt.Fprintf(os.Stderr, "📡 [LOGIC] Calling embedding API for hash %s...\n", hashStr[:8])
	embedStart := time.Now()
	vector, err := c.embed.GetEmbedding(composite)
	if err != nil {
		fmt.Fprintf(os.Stderr, "❌ [LOGIC] Embedding FAILED: %v\n", err)
		return fmt.Errorf("failed to get embedding: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✅ [LOGIC] Embedding retrieved in %v\n", time.Since(embedStart))

	// 4. Salvataggio nel DB
	fmt.Fprintf(os.Stderr, "💾 [LOGIC] Saving to database...\n")
	dbStart := time.Now()
	if err := c.db.InsertMemory(ts, composite, vector); err != nil {
		fmt.Fprintf(os.Stderr, "❌ [LOGIC] DB Insert FAILED: %v\n", err)
		return fmt.Errorf("failed to save to database: %w", err)
	}
	fmt.Fprintf(os.Stderr, "✅ [LOGIC] DB Insert SUCCESS in %v\n", time.Since(dbStart))

	return nil
}

func (c *Controller) DeleteMemory(id string) error {
	fmt.Fprintf(os.Stderr, "🗑️ [LOGIC] Deleting memory ID: %s\n", id)
	return c.db.DeleteMemory(id)
}

func (c *Controller) ListMemories(limit int) ([]db.Memory, error) {
	if limit <= 0 {
		limit = 10
	}
	fmt.Fprintf(os.Stderr, "📋 [LOGIC] Listing recent memories (limit: %d)\n", limit)
	return c.db.List(limit)
}

const DefaultBlueprint = `# TAZLAB KNOWLEDGE EXTRACTION PROTOCOL
## ROLE
Act as Senior Platform Architect. Extract technical chronicles in Markdown.
[[[CHRONICLE_START]]]
TS: <timestamp>
TITLE: <context>
TAGS: <tags>
BODY: <details>
[[[CHRONICLE_END]]]`

func (c *Controller) GetBlueprint() (string, error) {
	val, err := c.db.GetConfig("extraction_blueprint")
	if err != nil {
		return DefaultBlueprint, nil
	}
	return val, nil
}

func (c *Controller) UpdateBlueprint(content string) error {
	return c.db.SetConfig("extraction_blueprint", content)
}

func (c *Controller) SearchMemories(query string, limit int, daysBack int, startStr, endStr string) ([]db.Memory, error) {
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
