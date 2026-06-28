package storage

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"

	"github.com/weijian/go-llm-gateway/internal/metrics"
)

// SQLiteStore SQLite 持久化存储
type SQLiteStore struct {
	db     *sql.DB
	logger *log.Logger
}

// NewSQLiteStore 创建 SQLite 存储
func NewSQLiteStore(dbPath string, logger *log.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// 启用 WAL 模式提高并发性能
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set wal mode: %w", err)
	}

	// 创建表
	if err := initSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}

	logger.Printf("SQLite store initialized: %s", dbPath)

	return &SQLiteStore{db: db, logger: logger}, nil
}

// initSchema 初始化数据库表结构
func initSchema(db *sql.DB) error {
	schema := `
	CREATE TABLE IF NOT EXISTS request_logs (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp DATETIME NOT NULL,
		method TEXT NOT NULL,
		path TEXT NOT NULL,
		model TEXT DEFAULT '',
		provider TEXT DEFAULT '',
		status_code INTEGER DEFAULT 0,
		latency_ms REAL DEFAULT 0,
		input_tokens INTEGER DEFAULT 0,
		output_tokens INTEGER DEFAULT 0,
		is_stream BOOLEAN DEFAULT 0,
		error TEXT DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON request_logs(timestamp);
	CREATE INDEX IF NOT EXISTS idx_logs_model ON request_logs(model);
	CREATE INDEX IF NOT EXISTS idx_logs_provider ON request_logs(provider);

	CREATE TABLE IF NOT EXISTS daily_stats (
		date TEXT NOT NULL,
		model TEXT NOT NULL,
		provider TEXT DEFAULT '',
		requests INTEGER DEFAULT 0,
		successes INTEGER DEFAULT 0,
		errors INTEGER DEFAULT 0,
		total_tokens INTEGER DEFAULT 0,
		total_latency_ms REAL DEFAULT 0,
		PRIMARY KEY (date, model)
	);
	`
	_, err := db.Exec(schema)
	return err
}

// SaveRecord 保存一条请求记录
func (s *SQLiteStore) SaveRecord(r metrics.RequestRecord) error {
	_, err := s.db.Exec(`
		INSERT INTO request_logs (timestamp, method, path, model, provider, status_code, latency_ms, input_tokens, output_tokens, is_stream, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, r.Timestamp, r.Method, r.Path, r.Model, r.Provider, r.StatusCode, r.Latency, r.InputTokens, r.OutputTokens, r.IsStream, r.Error)

	if err != nil {
		s.logger.Printf("Save record error: %v", err)
		return err
	}

	// 更新每日统计
	date := r.Timestamp.Format("2006-01-02")
	success := 0
	if r.StatusCode >= 200 && r.StatusCode < 400 {
		success = 1
	}
	tokens := r.InputTokens + r.OutputTokens

	_, err = s.db.Exec(`
		INSERT INTO daily_stats (date, model, provider, requests, successes, errors, total_tokens, total_latency_ms)
		VALUES (?, ?, ?, 1, ?, ?, ?, ?)
		ON CONFLICT(date, model) DO UPDATE SET
			requests = requests + 1,
			successes = successes + ?,
			errors = errors + ?,
			total_tokens = total_tokens + ?,
			total_latency_ms = total_latency_ms + ?
	`, date, r.Model, r.Provider, success, 1-success, tokens, r.Latency,
		success, 1-success, tokens, r.Latency)

	return err
}

// GetRecentLogs 获取最近的请求日志
func (s *SQLiteStore) GetRecentLogs(limit int) ([]metrics.RequestRecord, error) {
	rows, err := s.db.Query(`
		SELECT timestamp, method, path, model, provider, status_code, latency_ms, input_tokens, output_tokens, is_stream, error
		FROM request_logs
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var records []metrics.RequestRecord
	for rows.Next() {
		var r metrics.RequestRecord
		var ts string
		if err := rows.Scan(&ts, &r.Method, &r.Path, &r.Model, &r.Provider, &r.StatusCode, &r.Latency, &r.InputTokens, &r.OutputTokens, &r.IsStream, &r.Error); err != nil {
			continue
		}
		r.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		records = append(records, r)
	}
	return records, nil
}

// GetDailyStats 获取指定日期的统计
func (s *SQLiteStore) GetDailyStats(date string) ([]metrics.ModelStats, error) {
	rows, err := s.db.Query(`
		SELECT model, provider, requests, successes, errors, total_tokens,
			CASE WHEN requests > 0 THEN total_latency_ms / requests ELSE 0 END as avg_latency
		FROM daily_stats
		WHERE date = ?
		ORDER BY requests DESC
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []metrics.ModelStats
	for rows.Next() {
		var ms metrics.ModelStats
		if err := rows.Scan(&ms.Model, &ms.Provider, &ms.Requests, &ms.Successes, &ms.Errors, &ms.TotalTokens, &ms.AvgLatency); err != nil {
			continue
		}
		stats = append(stats, ms)
	}
	return stats, nil
}

// GetTotalRequests 获取今日总请求数
func (s *SQLiteStore) GetTotalRequests(date string) (int, error) {
	var total int
	err := s.db.QueryRow(`SELECT COALESCE(SUM(requests), 0) FROM daily_stats WHERE date = ?`, date).Scan(&total)
	return total, err
}

// Cleanup 清理旧数据（保留最近 N 天）
func (s *SQLiteStore) Cleanup(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days).Format("2006-01-02")
	result, err := s.db.Exec(`DELETE FROM request_logs WHERE timestamp < ?`, cutoff)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n > 0 {
		s.logger.Printf("Cleaned up %d old log records", n)
	}

	result, err = s.db.Exec(`DELETE FROM daily_stats WHERE date < ?`, cutoff)
	if err != nil {
		return err
	}
	n, _ = result.RowsAffected()
	if n > 0 {
		s.logger.Printf("Cleaned up %d old daily stats", n)
	}
	return nil
}

// Close 关闭数据库连接
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
