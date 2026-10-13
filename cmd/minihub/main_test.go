package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/storage/backend"
)

func TestMigrationCommandConfigAndDataOverride(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config.json")
	if err := os.WriteFile(config, []byte(`{"version":1,"server":{"dataDir":"unused"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "source")
	usersDir := filepath.Join(root, "users")
	if err := os.MkdirAll(usersDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(usersDir, "u.json"), []byte(`{"version":1,"id":"u","name":"u","enabled":true,"role":"user"}`), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-config", config, "-data", root, "-to", "sqlite"}
	if err := migrate(append(args, "-dry-run")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, backend.DBName)); !os.IsNotExist(err) {
		t.Fatal("dry-run published")
	}
	if err := migrate(args); err != nil {
		t.Fatal(err)
	}
	h, err := backend.Open(root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if _, err = h.GetUser(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
}
func TestMigrationCommandRejectsInvalidArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"-to", "file"}, {"-to", "sqlite", "extra"}, {"-to", "sqlite", "-config", filepath.Join(t.TempDir(), "missing")}} {
		if err := migrate(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
