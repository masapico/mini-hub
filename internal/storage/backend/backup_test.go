package backend

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/domain"
)

func TestBackupWhileRunning(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	backupDB := filepath.Join(t.TempDir(), "backup", "minuhub_backup.db")

	// 1. Open the store as minihub would, holding the .minihub.lock
	handle, err := Open(dataDir)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer handle.Close()

	ctx := context.Background()
	user := domain.User{
		Version: domain.Version,
		ID:      "u0001",
		Name:    "Alice",
		Role:    domain.RoleAdmin,
		Enabled: true,
	}
	if err := handle.SaveUser(ctx, &user); err != nil {
		t.Fatalf("SaveUser failed: %v", err)
	}

	// 2. Perform backup while minihub is actively running (lock is held)
	if err := Backup(dataDir, backupDB); err != nil {
		t.Fatalf("Backup while running failed: %v", err)
	}

	// 3. Verify backup file exists and has content
	info, err := os.Stat(backupDB)
	if err != nil {
		t.Fatalf("Stat backupDB failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("backup file is empty")
	}

	// 4. Test missing marker returns error
	invalidDir := t.TempDir()
	if err := Backup(invalidDir, filepath.Join(t.TempDir(), "out.db")); err == nil {
		t.Fatalf("expected error for directory missing marker, got nil")
	}
}

