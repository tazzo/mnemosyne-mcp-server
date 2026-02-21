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

	// Inizializzazione Tabelle di Configurazione
	_, err = pool.Exec(`
		CREATE TABLE IF NOT EXISTS mnemosyne_config (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create config table: %w", err)
	}

	return &DB{pool: pool}, nil
}

func (db *DB) Close() error {
	return db.pool.Close()
}

func (db *DB) InsertMemory(ts time.Time, content string, vector []float32) error {
	_, err := db.pool.Exec(
		"INSERT INTO memories (timestamp, content, embedding) VALUES ($1, $2, $3)",
		ts, content, pgvector.NewVector(vector),
	)
	return err
}

func (db *DB) DeleteMemory(id string) error {
	_, err := db.pool.Exec("DELETE FROM memories WHERE id = $1", id)
	return err
}

func (db *DB) SetConfig(key, value string) error {
	_, err := db.pool.Exec(`
		INSERT INTO mnemosyne_config (key, value) 
		VALUES ($1, $2) 
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		key, value,
	)
	return err
}

func (db *DB) GetConfig(key string) (string, error) {
	var value string
	err := db.pool.QueryRow("SELECT value FROM mnemosyne_config WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
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
