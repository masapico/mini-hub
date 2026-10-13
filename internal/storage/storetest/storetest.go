// Package storetest provides storage implementations for tests.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/masapico/minihub/internal/storage"
	"github.com/masapico/minihub/internal/storage/sqlitestore"
)

func New(t testing.TB, root string) (storage.Storage, error) {
	t.Helper()
	s, err := sqlitestore.Open(filepath.Join(root, "minuhub.db"))
	if err == nil {
		t.Cleanup(func() {
			if err := s.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	return s, err
}
