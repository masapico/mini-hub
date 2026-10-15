package stats

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type ConnectionRecord struct {
	Time             string  `json:"time"`
	OnlineUsers      int     `json:"online_users"`
	TotalConnections int     `json:"total_connections"`
	EnabledUsers     int     `json:"enabled_users"`
	UtilizationRate  float64 `json:"utilization_rate"`
}

type StatsProvider func() (onlineUsers, totalConnections int)
type EnabledUsersProvider func(ctx context.Context) (int, error)

type Recorder struct {
	file         *os.File
	interval     time.Duration
	stats        StatsProvider
	enabledUsers EnabledUsersProvider
	logger       *slog.Logger
	mu           sync.Mutex
	stopCh       chan struct{}
	doneCh       chan struct{}
	started      bool
}

func NewRecorder(dataDir string, interval time.Duration, stats StatsProvider, enabledUsers EnabledUsersProvider, logger *slog.Logger) (*Recorder, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory for connection counts log: %w", err)
	}
	logPath := filepath.Join(dataDir, "connection_counts.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open connection counts log: %w", err)
	}
	return &Recorder{
		file:         f,
		interval:     interval,
		stats:        stats,
		enabledUsers: enabledUsers,
		logger:       logger,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}, nil
}

func (r *Recorder) RecordOnce(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	onlineUsers, totalConns := 0, 0
	if r.stats != nil {
		onlineUsers, totalConns = r.stats()
	}
	enabledCount := 0
	if r.enabledUsers != nil {
		c, err := r.enabledUsers(ctx)
		if err != nil {
			r.logger.Warn("failed to get enabled users count for connection stats", "error", err)
		} else {
			enabledCount = c
		}
	}

	var rate float64
	if enabledCount > 0 {
		rate = math.Round((float64(onlineUsers)/float64(enabledCount))*1000) / 10
	}

	rec := ConnectionRecord{
		Time:             time.Now().Format(time.RFC3339),
		OnlineUsers:      onlineUsers,
		TotalConnections: totalConns,
		EnabledUsers:     enabledCount,
		UtilizationRate:  rate,
	}

	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal connection record: %w", err)
	}
	data = append(data, '\n')
	if _, err := r.file.Write(data); err != nil {
		return fmt.Errorf("write connection record: %w", err)
	}
	return nil
}

func (r *Recorder) Start(ctx context.Context) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.mu.Unlock()

	go func() {
		defer close(r.doneCh)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-r.stopCh:
				return
			case <-ticker.C:
				if err := r.RecordOnce(ctx); err != nil {
					r.logger.Warn("failed to record connection counts", "error", err)
				}
			}
		}
	}()
}

func (r *Recorder) Close() error {
	r.mu.Lock()
	select {
	case <-r.stopCh:
	default:
		close(r.stopCh)
	}
	started := r.started
	r.mu.Unlock()

	if started {
		<-r.doneCh
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.file != nil {
		err := r.file.Close()
		r.file = nil
		return err
	}
	return nil
}

