package backend

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/masapico/minihub/internal/storage/sqlitestore"
)

// Backup creates an online, crash-consistent backup snapshot of the SQLite database
// in dataDir to destDBPath without requiring minihub to stop.
func Backup(dataDir, destDBPath string) error {
	info, err := os.Stat(dataDir)
	if err != nil {
		return fmt.Errorf("inspect data directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("data path is not a directory: %s", dataDir)
	}

	mark, err := readMarker(dataDir)
	if err != nil {
		return fmt.Errorf("verify storage marker: %w", err)
	}
	if mark == "" {
		return errors.New("storage marker storage.json is missing in data directory")
	}
	if mark != "sqlite" {
		return fmt.Errorf("unsupported storage marker type %q", mark)
	}

	srcDB := filepath.Join(dataDir, DBName)
	if _, err := os.Stat(srcDB); err != nil {
		return fmt.Errorf("SQLite database %s is missing: %w", srcDB, err)
	}

	return sqlitestore.CreateBackup(srcDB, destDBPath)
}

