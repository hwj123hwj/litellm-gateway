package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// initAgentSchema creates the agent_settings / assistant_feedback tables.
// 与 metrics/archive/memory 物理隔离：助理人设与反馈是长期配置数据，
// 保留策略与日志类数据无关。
func initAgentSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS agent_settings (
		key        TEXT PRIMARY KEY,
		value      TEXT NOT NULL,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE TABLE IF NOT EXISTS assistant_feedback (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		rating        TEXT NOT NULL CHECK (rating IN ('up', 'down')),
		reply_excerpt TEXT NOT NULL DEFAULT '',
		note          TEXT NOT NULL DEFAULT '',
		created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	);
	CREATE INDEX IF NOT EXISTS idx_assistant_feedback_created ON assistant_feedback (created_at DESC);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return fmt.Errorf("exec agent schema: %w", err)
	}
	return nil
}

// GetSetting returns the stored value for key; sql.ErrNoRows when unset.
func (s *SQLiteStore) GetSetting(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM agent_settings WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetSetting upserts a setting value.
func (s *SQLiteStore) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO agent_settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
	`, key, value, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("set setting %s: %w", key, err)
	}
	return nil
}

// AssistantFeedback is one human rating on an assistant reply.
type AssistantFeedback struct {
	ID           int64     `json:"id"`
	Rating       string    `json:"rating"` // up | down
	ReplyExcerpt string    `json:"reply_excerpt"`
	Note         string    `json:"note"`
	CreatedAt    time.Time `json:"created_at"`
}

// AddAssistantFeedback stores a rating. reply excerpt is truncated to keep
// the table lean — it only needs to remind the user which turn it was.
func (s *SQLiteStore) AddAssistantFeedback(rating, replyExcerpt, note string) error {
	if len(replyExcerpt) > 280 {
		replyExcerpt = replyExcerpt[:280]
	}
	if len(note) > 500 {
		note = note[:500]
	}
	_, err := s.db.Exec(
		`INSERT INTO assistant_feedback (rating, reply_excerpt, note, created_at) VALUES (?, ?, ?, ?)`,
		rating, replyExcerpt, note, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("add assistant feedback: %w", err)
	}
	return nil
}

// ListAssistantFeedback returns the most recent entries, newest first.
func (s *SQLiteStore) ListAssistantFeedback(limit int) ([]AssistantFeedback, error) {
	rows, err := s.db.Query(
		`SELECT id, rating, reply_excerpt, note, created_at FROM assistant_feedback ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list assistant feedback: %w", err)
	}
	defer rows.Close()
	var out []AssistantFeedback
	for rows.Next() {
		var f AssistantFeedback
		if err := rows.Scan(&f.ID, &f.Rating, &f.ReplyExcerpt, &f.Note, &f.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}
