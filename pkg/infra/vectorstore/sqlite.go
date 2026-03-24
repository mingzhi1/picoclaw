package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/sipeed/picoclaw/pkg/infra/logger"

	_ "modernc.org/sqlite"
)

// SQLiteStore is a brute-force vector store backed by SQLite.
// Suitable for up to ~100k chunks. Beyond that, use a dedicated vector DB.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLiteStore opens (or creates) a vector store at dbPath.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	os.MkdirAll(filepath.Dir(dbPath), 0o755)

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open vector db: %w", err)
	}

	ddl := `
CREATE TABLE IF NOT EXISTS chunks (
	id        TEXT PRIMARY KEY,
	content   TEXT NOT NULL,
	source    TEXT NOT NULL,
	embedding TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chunks_source ON chunks(source);

CREATE TABLE IF NOT EXISTS rag_sources (
	source_path  TEXT PRIMARY KEY,
	content_hash TEXT NOT NULL,
	last_indexed TEXT NOT NULL DEFAULT (datetime('now'))
);
`
	if _, err := db.Exec(ddl); err != nil {
		db.Close()
		return nil, fmt.Errorf("init vector db tables: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// Upsert inserts or replaces chunks with their embeddings.
func (s *SQLiteStore) Upsert(_ context.Context, chunks []Chunk, embeddings [][]float32) error {
	if len(chunks) != len(embeddings) {
		return fmt.Errorf("chunks and embeddings length mismatch: %d vs %d", len(chunks), len(embeddings))
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	stmt, err := tx.Prepare(
		"INSERT OR REPLACE INTO chunks (id, content, source, embedding) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for i, c := range chunks {
		embJSON, err := json.Marshal(embeddings[i])
		if err != nil {
			return fmt.Errorf("marshal embedding %d: %w", i, err)
		}
		if _, err := stmt.Exec(c.ID, c.Content, c.Source, string(embJSON)); err != nil {
			return fmt.Errorf("insert chunk %s: %w", c.ID, err)
		}
	}
	return tx.Commit()
}

// Search finds the topK most similar chunks using brute-force cosine similarity.
func (s *SQLiteStore) Search(_ context.Context, query []float32, topK int) ([]Chunk, error) {
	if topK <= 0 {
		topK = 5
	}

	rows, err := s.db.Query("SELECT id, content, source, embedding FROM chunks")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type scored struct {
		chunk Chunk
		score float64
	}
	var results []scored

	for rows.Next() {
		var id, content, source, embJSON string
		if err := rows.Scan(&id, &content, &source, &embJSON); err != nil {
			continue
		}
		var emb []float32
		if err := json.Unmarshal([]byte(embJSON), &emb); err != nil {
			continue
		}
		score := cosineSimilarity(query, emb)
		results = append(results, scored{
			chunk: Chunk{ID: id, Content: content, Source: source},
			score: score,
		})
	}

	// Sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if len(results) > topK {
		results = results[:topK]
	}

	out := make([]Chunk, len(results))
	for i, r := range results {
		r.chunk.Score = r.score
		out[i] = r.chunk
	}

	logger.DebugCF("vectorstore", "Search completed",
		map[string]any{"total_chunks": len(results), "top_k": topK})

	return out, nil
}

// DeleteBySource removes all chunks associated with a source.
func (s *SQLiteStore) DeleteBySource(_ context.Context, source string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM chunks WHERE source = ?", source)
	return err
}

// --- Source tracking (rag_sources table) ---

// GetSourceHash returns the stored content hash for a source, or "" if not indexed.
func (s *SQLiteStore) GetSourceHash(source string) string {
	var hash string
	err := s.db.QueryRow("SELECT content_hash FROM rag_sources WHERE source_path = ?", source).Scan(&hash)
	if err != nil {
		return ""
	}
	return hash
}

// SetSourceHash records the content hash for a source.
func (s *SQLiteStore) SetSourceHash(source, hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO rag_sources (source_path, content_hash, last_indexed) 
		 VALUES (?, ?, datetime('now'))`, source, hash)
	return err
}

// RemoveSource deletes source tracking and all its chunks.
func (s *SQLiteStore) RemoveSource(ctx context.Context, source string) error {
	if err := s.DeleteBySource(ctx, source); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.Exec("DELETE FROM rag_sources WHERE source_path = ?", source)
	return err
}

// ListSources returns all indexed source paths.
func (s *SQLiteStore) ListSources() ([]string, error) {
	rows, err := s.db.Query("SELECT source_path FROM rag_sources ORDER BY last_indexed DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sources []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			continue
		}
		sources = append(sources, src)
	}
	return sources, rows.Err()
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
