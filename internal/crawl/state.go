package crawl

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registers "sqlite"
)

const (
	stateFileName  = ".openaleph_crawl_state.db"
	failedFileName = ".openaleph-failed.txt"
)

// StatePath returns where the crawl state of root is kept. Like the Python
// client it prefers the crawled directory and falls back to the temp
// directory when that is not writable. The names are shared with the Python
// client, so a crawl can be resumed by either tool.
func StatePath(root string) (path string, fallback bool) {
	// A uniquely named probe file, so no pre-existing file is touched.
	if f, err := os.CreateTemp(root, ".openaleph_write_test_*"); err == nil {
		name := f.Name()
		f.Close()
		_ = os.Remove(name)
		return filepath.Join(root, stateFileName), false
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("openaleph_crawl_state_%s.db", pathHash(root))), true
}

func pathHash(root string) string {
	sum := sha256.Sum256([]byte(root))
	return hex.EncodeToString(sum[:])[:16]
}

// State records which files were uploaded or failed. Its SQLite schema is
// the one used by the Python client.
type State struct {
	Path string
	db   *sql.DB
}

// StateExistsError is returned when a new crawl would clobber an existing
// state file. The Python client silently deletes it; oa never deletes or
// overwrites state on its own.
type StateExistsError struct{ Path string }

func (e *StateExistsError) Error() string {
	return fmt.Sprintf("state file %s already exists: pass --resume to continue that crawl, or remove the file to start over", e.Path)
}

// OpenState opens the state database. With fresh set it refuses to reuse an
// existing database and returns *StateExistsError instead.
func OpenState(path string, fresh bool) (*State, error) {
	if _, err := os.Stat(path); err == nil && fresh {
		return nil, &StateExistsError{Path: path}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection serializes writers from all upload workers.
	db.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"CREATE TABLE IF NOT EXISTS processed (path TEXT PRIMARY KEY)",
		"CREATE TABLE IF NOT EXISTS failed (path TEXT PRIMARY KEY)",
	} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, err
		}
	}
	return &State{Path: path, db: db}, nil
}

// Close closes the database.
func (s *State) Close() error { return s.db.Close() }

// Processed reports whether rel was uploaded in this or an earlier session.
func (s *State) Processed(rel string) (bool, error) {
	var one int
	err := s.db.QueryRow("SELECT 1 FROM processed WHERE path = ?", rel).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// MarkProcessed records a successful upload and clears an earlier failure.
func (s *State) MarkProcessed(rel string) error {
	if _, err := s.db.Exec("INSERT OR IGNORE INTO processed(path) VALUES(?)", rel); err != nil {
		return err
	}
	_, err := s.db.Exec("DELETE FROM failed WHERE path = ?", rel)
	return err
}

// MarkFailed records a failed upload.
func (s *State) MarkFailed(rel string) error {
	_, err := s.db.Exec("INSERT OR IGNORE INTO failed(path) VALUES(?)", rel)
	return err
}

// Counts returns the number of uploaded and failed files.
func (s *State) Counts() (processed, failed int, err error) {
	if err = s.db.QueryRow("SELECT COUNT(*) FROM processed").Scan(&processed); err != nil {
		return
	}
	err = s.db.QueryRow("SELECT COUNT(*) FROM failed").Scan(&failed)
	return
}

// FailedPaths lists failed files in sorted order.
func (s *State) FailedPaths() ([]string, error) {
	rows, err := s.db.Query("SELECT path FROM failed ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
