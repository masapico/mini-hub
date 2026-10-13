package sqlitestore

import (
	"context"
	"database/sql"
	"errors"

	"github.com/masapico/minihub/internal/domain"
)

func (s *Store) GetUser(ctx context.Context, id string) (*domain.User, error) {
	return get[domain.User](ctx, s, "users", id)
}
func (s *Store) ListUsers(ctx context.Context) ([]domain.User, error) {
	return list[domain.User](ctx, s, "users")
}
func (s *Store) SaveUser(ctx context.Context, v *domain.User) error {
	if v == nil {
		return errors.New("User is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	return s.save(ctx, "users", v.ID, v)
}
func (s *Store) GetGroup(ctx context.Context, id string) (*domain.Group, error) {
	return get[domain.Group](ctx, s, "groups", id)
}
func (s *Store) ListGroups(ctx context.Context) ([]domain.Group, error) {
	return list[domain.Group](ctx, s, "groups")
}
func (s *Store) SaveGroup(ctx context.Context, v *domain.Group) error {
	if v == nil {
		return errors.New("Group is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	return s.save(ctx, "groups", v.ID, v)
}
func (s *Store) DeleteGroup(ctx context.Context, id string) error { return s.remove(ctx, "groups", id) }
func (s *Store) GetChannel(ctx context.Context, id string) (*domain.Channel, error) {
	return get[domain.Channel](ctx, s, "channels", id)
}
func (s *Store) ListChannels(ctx context.Context) ([]domain.Channel, error) {
	return list[domain.Channel](ctx, s, "channels")
}
func (s *Store) SaveChannel(ctx context.Context, v *domain.Channel) error {
	if v == nil {
		return errors.New("Channel is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	return s.save(ctx, "channels", v.ID, v)
}
func (s *Store) GetSession(ctx context.Context, id string) (*domain.Session, error) {
	return get[domain.Session](ctx, s, "sessions", id)
}
func (s *Store) SaveSession(ctx context.Context, v *domain.Session) error {
	if v == nil {
		return errors.New("Session is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	return s.save(ctx, "sessions", v.TokenHash, v)
}
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.remove(ctx, "sessions", id)
}
func (s *Store) GetSchedule(ctx context.Context, id string) (*domain.Schedule, error) {
	return get[domain.Schedule](ctx, s, "schedules", id)
}
func (s *Store) ListSchedules(ctx context.Context) ([]domain.Schedule, error) {
	return list[domain.Schedule](ctx, s, "schedules")
}
func (s *Store) SaveSchedule(ctx context.Context, v *domain.Schedule) error {
	if v == nil {
		return errors.New("Schedule is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	if err := checkIDs(v.ChannelID); err != nil {
		return err
	}
	return s.save(ctx, "schedules", v.ID, v)
}
func (s *Store) GetAIAccount(ctx context.Context, id string) (*domain.AIAccount, error) {
	return get[domain.AIAccount](ctx, s, "ai_accounts", id)
}
func (s *Store) ListAIAccounts(ctx context.Context) ([]domain.AIAccount, error) {
	return list[domain.AIAccount](ctx, s, "ai_accounts")
}
func (s *Store) SaveAIAccount(ctx context.Context, v *domain.AIAccount) error {
	if v == nil {
		return errors.New("AIAccount is required")
	}
	if v.Version == 0 {
		v.Version = domain.Version
	}
	return s.save(ctx, "ai_accounts", v.ID, v)
}
func (s *Store) DeleteAIAccount(ctx context.Context, id string) error {
	return s.remove(ctx, "ai_accounts", id)
}

func (s *Store) IncrementAIDailyUsage(ctx context.Context, userID, date string) (int, error) {
	if err := checkIDs(userID); err != nil {
		return 0, err
	}
	var count int
	err := s.write.QueryRowContext(ctx, `INSERT INTO ai_daily_usage(user_id, date, count) VALUES(?, ?, 1) ON CONFLICT(user_id, date) DO UPDATE SET count=count+1 RETURNING count`, userID, date).Scan(&count)
	return count, err
}

func (s *Store) GetAIDailyUsage(ctx context.Context, userID, date string) (int, error) {
	if err := checkIDs(userID); err != nil {
		return 0, err
	}
	var count int
	err := s.read.QueryRowContext(ctx, `SELECT count FROM ai_daily_usage WHERE user_id = ? AND date = ?`, userID, date).Scan(&count)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func (s *Store) ListAIDailyUsage(ctx context.Context, date string) (map[string]int, error) {
	rows, err := s.read.QueryContext(ctx, `SELECT user_id, count FROM ai_daily_usage WHERE date = ?`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]int)
	for rows.Next() {
		var uid string
		var count int
		if err := rows.Scan(&uid, &count); err != nil {
			return nil, err
		}
		result[uid] = count
	}
	return result, rows.Err()
}

