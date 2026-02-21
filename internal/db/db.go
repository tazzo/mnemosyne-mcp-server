package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/pgvector/pgvector-go"
)

type Memory struct {
	ID        int64
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

func (db *DB) Search(vector []float32, limit int, start, end *time.Time) ([]Memory, error) {
	query := `
		SELECT timestamp, content 
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
		if err := rows.Scan(&m.Timestamp, &m.Content); err != nil {
			return nil, err
		}
		memories = append(memories, m)
	}
	return memories, nil
}
