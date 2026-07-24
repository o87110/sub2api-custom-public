package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDatabaseWritePatternRejectsWritableCTE(t *testing.T) {
	require.True(t, databaseWritePattern.MatchString(
		"WITH removed AS (DELETE FROM sessions RETURNING id) SELECT id FROM removed",
	))
	require.True(t, databaseWritePattern.MatchString(
		"SELECT maintenance_update() FROM jobs; UPDATE jobs SET active = FALSE",
	))
	require.False(t, databaseWritePattern.MatchString(
		"SELECT id, updated_at FROM users WHERE deleted_at IS NULL",
	))
}
