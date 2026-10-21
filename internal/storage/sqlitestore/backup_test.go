package sqlitestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/domain"
)

func TestCreateBackup(t *testing.T) {
	tempDir := t.TempDir()
	srcDB := filepath.Join(tempDir, "minuhub.db")
	backupDB := filepath.Join(tempDir, "backup", "minuhub_backup.db")

	store, err := Open(srcDB)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer store.Close()

	ctx := context.Background()
	user := domain.User{
		Version: domain.Version,
		ID:      "u0001",
		Name:    "Test User",
		Role:    domain.RoleUser,
		Enabled: true,
	}
	if err := store.SaveUser(ctx, &user); err != nil {
		t.Fatalf("SaveUser failed: %v", err)
	}

	// Run backup while store is still open.
	if err := CreateBackup(srcDB, backupDB); err != nil {
		t.Fatalf("CreateBackup failed: %v", err)
	}

	// Verify backup file exists and has content.
	info, err := os.Stat(backupDB)
	if err != nil {
		t.Fatalf("Stat backupDB failed: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("backup file is empty")
	}

	// Open backup with sqlitestore and verify user.
	backupStore, err := OpenExisting(backupDB)
	if err != nil {
		t.Fatalf("OpenExisting on backupDB failed: %v", err)
	}

	loadedUser, err := backupStore.GetUser(ctx, "u0001")
	if err != nil {
		backupStore.Close()
		t.Fatalf("GetUser from backup failed: %v", err)
	}
	if loadedUser.Name != "Test User" {
		backupStore.Close()
		t.Fatalf("expected user name 'Test User', got %q", loadedUser.Name)
	}
	if err := backupStore.Close(); err != nil {
		t.Fatalf("Close backupStore failed: %v", err)
	}

	// Test overwriting existing backup file when it is not open.
	if err := CreateBackup(srcDB, backupDB); err != nil {
		t.Fatalf("CreateBackup overwrite failed: %v", err)
	}
}

