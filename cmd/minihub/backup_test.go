package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/config"
	"github.com/masapico/minihub/internal/domain"
	"github.com/masapico/minihub/internal/storage/backend"
	"github.com/masapico/minihub/internal/storage/sqlitestore"
)

func TestBackupCLI(t *testing.T) {
	tempDir := t.TempDir()
	dataDir := filepath.Join(tempDir, "data")
	backupOut := filepath.Join(tempDir, "backup", "minuhub_backup.db")

	// 1. Initialize data directory with minihub store
	store, err := backend.Open(dataDir)
	if err != nil {
		t.Fatalf("backend.Open failed: %v", err)
	}
	user := domain.User{
		Version: domain.Version,
		ID:      "u0001",
		Name:    "Alice",
		Role:    domain.RoleAdmin,
		Enabled: true,
	}
	if err := store.SaveUser(context.Background(), &user); err != nil {
		t.Fatalf("SaveUser failed: %v", err)
	}
	defer store.Close()

	// 2. Test missing -out flag fails
	if err := backup([]string{"-data", dataDir}); err == nil {
		t.Fatalf("expected error when -out is omitted, got nil")
	}

	// 3. Test backup with explicit -data and -out while running
	if err := backup([]string{"-data", dataDir, "-out", backupOut}); err != nil {
		t.Fatalf("backup with -data failed: %v", err)
	}

	// Verify backup DB
	verifyStore, err := sqlitestore.OpenExisting(backupOut)
	if err != nil {
		t.Fatalf("OpenExisting on backup failed: %v", err)
	}
	gotUser, err := verifyStore.GetUser(context.Background(), "u0001")
	if err != nil {
		verifyStore.Close()
		t.Fatalf("GetUser failed: %v", err)
	}
	if gotUser.Name != "Alice" {
		verifyStore.Close()
		t.Fatalf("expected user name 'Alice', got %q", gotUser.Name)
	}
	verifyStore.Close()

	// 4. Test backup with -config
	cfgPath := filepath.Join(tempDir, "minihub.json")
	cfgData, _ := json.Marshal(config.Config{
		Version: 1,
		Server: config.Server{
			DataDir: dataDir,
		},
	})
	if err := os.WriteFile(cfgPath, cfgData, 0600); err != nil {
		t.Fatalf("WriteFile cfgPath failed: %v", err)
	}

	backupOut2 := filepath.Join(tempDir, "backup", "minuhub_backup2.db")
	if err := backup([]string{"-config", cfgPath, "-out", backupOut2}); err != nil {
		t.Fatalf("backup with -config failed: %v", err)
	}
	if _, err := os.Stat(backupOut2); err != nil {
		t.Fatalf("Stat backupOut2 failed: %v", err)
	}
}

