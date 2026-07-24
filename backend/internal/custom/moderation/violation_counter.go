package moderation

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ViolationCounter is the narrow custom read port used by the official moderation service.
type ViolationCounter interface {
	CountFlaggedByUserSince(ctx context.Context, userID int64, since time.Time, excludeCyberPolicy bool) (int, error)
}

type sqlViolationCounter struct {
	db *sql.DB
}

// NewViolationCounter creates the custom read-only violation counter.
func NewViolationCounter(db *sql.DB) ViolationCounter {
	return &sqlViolationCounter{db: db}
}

func (c *sqlViolationCounter) CountFlaggedByUserSince(
	ctx context.Context,
	userID int64,
	since time.Time,
	excludeCyberPolicy bool,
) (int, error) {
	if userID <= 0 {
		return 0, nil
	}
	if c == nil || c.db == nil {
		return 0, fmt.Errorf("custom violation counter database is unavailable")
	}

	var count int
	err := c.db.QueryRowContext(ctx, `
WITH last_auto_ban AS (
    SELECT MAX(created_at) AS at
    FROM content_moderation_logs
    WHERE user_id = $1 AND auto_banned = TRUE
)
SELECT COUNT(*)
FROM content_moderation_logs
WHERE user_id = $1
  AND flagged = TRUE
  AND action <> 'hash_block'
  AND action <> 'cyber_policy_out_of_scope'
  AND ($3::bool IS FALSE OR action <> 'cyber_policy')
  AND created_at >= $2
  AND created_at > COALESCE((SELECT at FROM last_auto_ban), '-infinity'::timestamptz)
`, userID, since, excludeCyberPolicy).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count custom content moderation violations: %w", err)
	}
	return count, nil
}
