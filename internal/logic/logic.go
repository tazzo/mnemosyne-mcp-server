package logic

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"tazlab/mnemosyne-mcp-server/internal/db"
	"tazlab/mnemosyne-mcp-server/internal/embedding"
)

type Controller struct {
	db        *db.DB
	embed     *embedding.Client
	
	// Cache per deduplicazione (Session-aware)
	cache     map[string]time.Time
	cacheMu   sync.RWMutex
}

func New(database *db.DB, embedClient *embedding.Client) *Controller {
	return &Controller{
		db:    database,
		embed: embedClient,
		cache: make(map[string]time.Time),
	}
}

func (c *Controller) IngestMemory(content string, ts time.Time) error {
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
			fmt.Printf("⏩ Memory already saved recently (hash: %s), skipping.\n", hashStr[:8])
			return nil
		}
	}
	c.cache[hashStr] = time.Now()
	c.cacheMu.Unlock()

	// 3. Vettorizzazione
	vector, err := c.embed.GetEmbedding(composite)
	if err != nil {
		return fmt.Errorf("failed to get embedding: %w", err)
	}

	// 4. Salvataggio nel DB
	if err := c.db.InsertMemory(ts, composite, vector); err != nil {
		return fmt.Errorf("failed to save to database: %w", err)
	}

	return nil
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
