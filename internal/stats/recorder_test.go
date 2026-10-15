package stats

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecorderRecordOnce(t *testing.T) {
	dir := t.TempDir()
	stats := func() (int, int) {
		return 5, 8
	}
	enabledUsers := func(ctx context.Context) (int, error) {
		return 20, nil
	}

	rec, err := NewRecorder(dir, 10*time.Minute, stats, enabledUsers, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()

	if err := rec.RecordOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(dir, "connection_counts.log"))
	if err != nil {
		t.Fatal(err)
	}

	var parsed ConnectionRecord
	if err := json.Unmarshal(content, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v, content=%s", err, string(content))
	}

	if parsed.OnlineUsers != 5 || parsed.TotalConnections != 8 || parsed.EnabledUsers != 20 || parsed.UtilizationRate != 25.0 {
		t.Fatalf("unexpected parsed record: %+v", parsed)
	}
	if parsed.Time == "" {
		t.Fatal("expected non-empty time")
	}
}

func TestRecorderPeriodicExecution(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stats := func() (int, int) {
		calls++
		return calls, calls
	}
	enabledUsers := func(ctx context.Context) (int, error) {
		return 10, nil
	}

	rec, err := NewRecorder(dir, 20*time.Millisecond, stats, enabledUsers, nil)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rec.Start(ctx)

	// Wait for at least 2 ticks
	time.Sleep(70 * time.Millisecond)

	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(dir, "connection_counts.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
		var parsed ConnectionRecord
		if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
			t.Fatalf("invalid line %d: %v", lineCount, err)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if lineCount < 2 {
		t.Fatalf("expected at least 2 lines, got %d", lineCount)
	}
}

