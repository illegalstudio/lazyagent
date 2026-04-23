package search

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type index struct {
	db *sql.DB
}

func cacheDir() string {
	if d, err := os.UserCacheDir(); err == nil && d != "" {
		return filepath.Join(d, "lazyagent")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "lazyagent")
	}
	return ""
}

func indexPath() string {
	if d := cacheDir(); d != "" {
		return filepath.Join(d, "search.sqlite")
	}
	return ""
}

func openIndex(path string) (*index, error) {
	if path == "" {
		path = indexPath()
	}
	if path == "" {
		return nil, fmt.Errorf("could not resolve cache directory")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	idx := &index{db: db}
	if err := idx.init(); err != nil {
		db.Close()
		return nil, err
	}
	return idx, nil
}

func (i *index) close() error {
	return i.db.Close()
}

func (i *index) init() error {
	stmts := []string{
		`PRAGMA journal_mode=WAL`,
		`CREATE TABLE IF NOT EXISTS sources (
			agent TEXT NOT NULL,
			source_id TEXT NOT NULL,
			path TEXT NOT NULL,
			mtime_ns INTEGER NOT NULL,
			size INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			PRIMARY KEY(agent, source_id)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks USING fts5(
			agent UNINDEXED,
			source_id UNINDEXED,
			session_id UNINDEXED,
			source_path UNINDEXED,
			cwd UNINDEXED,
			name UNINDEXED,
			role UNINDEXED,
			ts UNINDEXED,
			text,
			tokenize = 'unicode61'
		)`,
	}
	for _, stmt := range stmts {
		if _, err := i.db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (i *index) reset() error {
	_, err := i.db.Exec(`DELETE FROM chunks; DELETE FROM sources`)
	return err
}

func (i *index) sourceCurrent(src sourceState) (bool, error) {
	var mtimeNS, size int64
	err := i.db.QueryRow(
		`SELECT mtime_ns, size FROM sources WHERE agent = ? AND source_id = ?`,
		src.Agent, src.ID,
	).Scan(&mtimeNS, &size)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return mtimeNS == src.MTimeNS && size == src.Size, nil
}

func (i *index) upsertSource(tx *sql.Tx, src sourceState) error {
	_, err := tx.Exec(
		`INSERT INTO sources(agent, source_id, path, mtime_ns, size, last_seen)
		 VALUES(?, ?, ?, ?, ?, ?)
		 ON CONFLICT(agent, source_id) DO UPDATE SET
		   path = excluded.path,
		   mtime_ns = excluded.mtime_ns,
		   size = excluded.size,
		   last_seen = excluded.last_seen`,
		src.Agent, src.ID, src.Path, src.MTimeNS, src.Size, time.Now().Unix(),
	)
	return err
}

func (i *index) replaceSource(src sourceState, chunks []chunk) error {
	tx, err := i.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM chunks WHERE agent = ? AND source_id = ?`, src.Agent, src.ID); err != nil {
		return err
	}
	for _, c := range chunks {
		if strings.TrimSpace(c.Text) == "" {
			continue
		}
		_, err := tx.Exec(
			`INSERT INTO chunks(agent, source_id, session_id, source_path, cwd, name, role, ts, text)
			 VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.Source.Agent, c.Source.ID, c.SessionID, c.Source.Path, c.CWD, c.Name, c.Role,
			c.Timestamp.Format(time.RFC3339Nano), normalizeText(c.Text),
		)
		if err != nil {
			return err
		}
	}
	if err := i.upsertSource(tx, src); err != nil {
		return err
	}
	return tx.Commit()
}

func (i *index) touchSource(src sourceState) error {
	_, err := i.db.Exec(
		`UPDATE sources SET last_seen = ? WHERE agent = ? AND source_id = ?`,
		time.Now().Unix(), src.Agent, src.ID,
	)
	return err
}

func (i *index) pruneMissing(agent string, seen map[string]struct{}) error {
	rows, err := i.db.Query(`SELECT source_id FROM sources WHERE agent = ?`, agent)
	if err != nil {
		return err
	}
	defer rows.Close()

	var stale []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		if _, ok := seen[id]; !ok {
			stale = append(stale, id)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range stale {
		if _, err := i.db.Exec(`DELETE FROM chunks WHERE agent = ? AND source_id = ?`, agent, id); err != nil {
			return err
		}
		if _, err := i.db.Exec(`DELETE FROM sources WHERE agent = ? AND source_id = ?`, agent, id); err != nil {
			return err
		}
	}
	return nil
}

func (i *index) search(query string, agents []string, limit int) ([]hit, error) {
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	placeholders := make([]string, len(agents))
	args := make([]any, 0, len(agents)+2)
	args = append(args, match)
	for idx, agent := range agents {
		placeholders[idx] = "?"
		args = append(args, agent)
	}
	args = append(args, limit)
	rows, err := i.db.Query(
		`SELECT agent, session_id, cwd, name, role, ts, text, bm25(chunks) AS rank
		 FROM chunks
		 WHERE chunks MATCH ? AND agent IN (`+strings.Join(placeholders, ",")+`)
		 ORDER BY rank
		 LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []hit
	for rows.Next() {
		var h hit
		var ts string
		if err := rows.Scan(&h.Agent, &h.SessionID, &h.CWD, &h.Name, &h.Role, &ts, &h.Text, &h.Rank); err != nil {
			return nil, err
		}
		h.Timestamp, _ = time.Parse(time.RFC3339Nano, ts)
		hits = append(hits, h)
	}
	return hits, rows.Err()
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
