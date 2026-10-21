package sqlitestore

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// CreateBackup creates a consistent snapshot of the source SQLite database
// at destDBPath using SQLite's VACUUM INTO. The source database can be
// actively written to by minihub in WAL mode while the backup runs.
func CreateBackup(srcDBPath, destDBPath string) error {
	srcDBPath, err := filepath.Abs(srcDBPath)
	if err != nil {
		return fmt.Errorf("resolve source database path: %w", err)
	}
	destDBPath, err = filepath.Abs(destDBPath)
	if err != nil {
		return fmt.Errorf("resolve destination database path: %w", err)
	}

	info, err := os.Stat(srcDBPath)
	if err != nil {
		return fmt.Errorf("inspect source database: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("source database path is a directory: %s", srcDBPath)
	}

	// Ensure destination directory exists.
	destDir := filepath.Dir(destDBPath)
	if err := os.MkdirAll(destDir, 0700); err != nil {
		return fmt.Errorf("create destination directory: %w", err)
	}

	// VACUUM INTO fails if the target file already exists.
	if _, err := os.Stat(destDBPath); err == nil {
		if err := os.Remove(destDBPath); err != nil {
			return fmt.Errorf("remove existing destination file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect destination path: %w", err)
	}

	// Open connection to source database with busy timeout.
	srcSlash := filepath.ToSlash(srcDBPath)
	if !strings.HasPrefix(srcSlash, "/") {
		srcSlash = "/" + srcSlash
	}
	u := url.URL{Scheme: "file", Path: srcSlash}
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(10000)")
	u.RawQuery = q.Encode()

	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return fmt.Errorf("open source database for backup: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Execute VACUUM INTO with normalized, escaped destination path.
	destSlash := filepath.ToSlash(destDBPath)
	escapedDest := strings.ReplaceAll(destSlash, "'", "''")
	vacuumSQL := fmt.Sprintf("VACUUM INTO '%s';", escapedDest)
	if _, err := db.Exec(vacuumSQL); err != nil {
		return fmt.Errorf("execute VACUUM INTO: %w", err)
	}

	// Verify destination database integrity and schema.
	verifyDB, err := inspect(destDBPath)
	if err != nil {
		return fmt.Errorf("verify created backup: %w", err)
	}
	if err := verifyDB.Close(); err != nil {
		return fmt.Errorf("close verification connection: %w", err)
	}

	return nil
}

