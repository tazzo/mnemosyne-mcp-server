package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
)

type Memory struct {
	ID        string
	Timestamp time.Time
	Content   string
	Embedding []float32
}

type DB struct {
	pool *sql.DB
}

func New(host, port, user, password, dbname string) (*DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		host, port, user, password, dbname)

	pool, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(); err != nil {
		return nil, err
	}

	// T0.1: Ensure idempotency columns exist
	_, err = pool.Exec(`
		ALTER TABLE memories ADD COLUMN IF NOT EXISTS content_hash TEXT UNIQUE;
		ALTER TABLE memories ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP;
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to add idempotency columns to memories table: %w", err)
	}

	// T0.2: Discover Embedding Dimensions
	// Fallback to 3072 if table is empty (e.g. text-embedding-004)
	var dims int
	err = pool.QueryRow("SELECT vector_dims(embedding) FROM memories WHERE embedding IS NOT NULL LIMIT 1").Scan(&dims)
	if err != nil {
		if err == sql.ErrNoRows {
			// Database vuoto o nessuna embedding presente, usa fallback
			dims = 3072
			fmt.Printf("⚠️ No existing embeddings found, defaulting to %d dimensions\n", dims)
		} else {
			return nil, fmt.Errorf("failed to autodiscover embedding dimensions: %w", err)
		}
	} else {
		fmt.Printf("✅ Autodiscovered embedding dimensions: %d\n", dims)
	}

	return &DB{pool: pool}, nil
}

func (db *DB) Close() error {
	return db.pool.Close()
}

func (db *DB) InsertMemory(ts time.Time, content string, vector []float32, hash string) error {
	_, err := db.pool.Exec(`
		INSERT INTO memories (timestamp, content, embedding, content_hash) 
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (content_hash) DO UPDATE SET last_seen_at = CURRENT_TIMESTAMP`,
		ts, content, pgvector.NewVector(vector), hash,
	)
	return err
}

func (db *DB) CheckMemoryExists(hash string) (bool, error) {
	var exists bool
	err := db.pool.QueryRow("SELECT EXISTS(SELECT 1 FROM memories WHERE content_hash = $1)", hash).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func (db *DB) DeleteMemory(id string) error {
	_, err := db.pool.Exec("DELETE FROM memories WHERE id = $1", id)
	return err
}

func (db *DB) GetMemoryByID(id string) (string, error) {
	var content string
	err := db.pool.QueryRow("SELECT content FROM memories WHERE id = $1", id).Scan(&content)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("memory not found")
		}
		return "", err
	}
	return content, nil
}

func (db *DB) List(limit int) ([]Memory, error) {
	rows, err := db.pool.Query("SELECT id, timestamp, content FROM memories ORDER BY timestamp DESC LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.Content); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, nil
}

func (db *DB) Search(vector []float32, limit int, start, end *time.Time) ([]Memory, error) {
	query := `
		SELECT id, timestamp, content 
		FROM memories 
		WHERE 1=1
	`
	args := []interface{}{pgvector.NewVector(vector)}
	argIdx := 2

	if start != nil {
		query += fmt.Sprintf(" AND timestamp >= $%d", argIdx)
		args = append(args, *start)
		argIdx++
	}
	if end != nil {
		query += fmt.Sprintf(" AND timestamp <= $%d", argIdx)
		args = append(args, *end)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY embedding <=> $1 LIMIT %d", limit)

	rows, err := db.pool.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var memories []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.ID, &m.Timestamp, &m.Content); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, nil
}
