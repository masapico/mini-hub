package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/masapico/minihub/internal/domain"
)

func (s *Store) GetWebhook(ctx context.Context, id string) (*domain.Webhook, error) {
	return get[domain.Webhook](ctx, s, "webhooks", id)
}

func (s *Store) GetWebhookByTokenHash(ctx context.Context, tokenHash string) (*domain.Webhook, error) {
	var data []byte
	if err := s.read.QueryRowContext(ctx, "SELECT data FROM webhooks WHERE token_hash=?", tokenHash).Scan(&data); err != nil {
		return nil, missing(err)
	}
	var v domain.Webhook
	err := json.Unmarshal(data, &v)
	return &v, err
}

func (s *Store) ListWebhooks(ctx context.Context) ([]domain.Webhook, error) {
	return list[domain.Webhook](ctx, s, "webhooks")
}

func (s *Store) ListChannelWebhooks(ctx context.Context, channelID string) ([]domain.Webhook, error) {
	if err := checkIDs(channelID); err != nil {
		return nil, err
	}
	rows, err := s.read.QueryContext(ctx, "SELECT data FROM webhooks WHERE channel_id=? ORDER BY id", channelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Webhook
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var v domain.Webhook
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) SaveWebhook(ctx context.Context, v *domain.Webhook) error {
	if v == nil {
		return errors.New("Webhook is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	if err := checkIDs(v.ID, v.ChannelID); err != nil {
		return err
	}
	if v.TokenHash == "" {
		return errors.New("TokenHash is required")
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.write.ExecContext(ctx, `INSERT INTO webhooks(id, channel_id, token_hash, data) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET channel_id=excluded.channel_id, token_hash=excluded.token_hash, data=excluded.data`, v.ID, v.ChannelID, v.TokenHash, string(b))
	return err
}

func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	return s.remove(ctx, "webhooks", id)
}

func (s *Store) TouchWebhookUsed(ctx context.Context, id string, at time.Time) error {
	if err := checkIDs(id); err != nil {
		return err
	}
	ts := at.Format(time.RFC3339Nano)
	_, err := s.write.ExecContext(ctx, `UPDATE webhooks SET data = json_set(data, '$.lastUsedAt', ?) WHERE id = ?`, ts, id)
	return err
}

