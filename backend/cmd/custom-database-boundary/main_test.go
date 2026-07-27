package main

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/custom/databaseboundary"
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

func TestEnforceFinalBoundaryAllowsExactStructuralException(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/ent/schema/channel_monitor.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "structural",
	}
	baselineCommit := "baseline-commit"
	allowed := map[string]exception{
		item.Path: exactException(item, baselineCommit),
	}

	require.NoError(t, enforceFinalBoundary([]change{item}, allowed, baselineCommit))
}

func TestEnforceFinalBoundaryRejectsUnapprovedStructuralChange(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/ent/schema/channel_monitor.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "structural",
	}

	err := enforceFinalBoundary([]change{item}, map[string]exception{}, "baseline-commit")
	require.EqualError(t, err, "unapproved database structure change: "+item.Path)
}

func TestEnforceFinalBoundaryRejectsStructuralFingerprintMismatch(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/ent/schema/channel_monitor.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "structural",
	}
	baselineCommit := "baseline-commit"
	entry := exactException(item, baselineCommit)
	entry.TargetBlob = "other-target"

	err := enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: entry},
		baselineCommit,
	)
	require.EqualError(t, err, "database exception fingerprint mismatch for "+item.Path)
}

func TestEnforceFinalBoundaryStillRejectsWritableSQLException(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "semantic",
		TargetSemantics: databaseboundary.Semantics{
			Statements: []string{"UPDATE users SET active = FALSE"},
		},
	}
	baselineCommit := "baseline-commit"

	err := enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: exactException(item, baselineCommit)},
		baselineCommit,
	)
	require.EqualError(t, err, "final database exception is not read-only in "+item.Path)
}

func exactException(item change, baselineCommit string) exception {
	return exception{
		Path:           item.Path,
		BaseCommit:     baselineCommit,
		BaseBlob:       item.BaseBlob,
		TargetBlob:     item.TargetBlob,
		SemanticDigest: item.TargetSemantics.Digest(),
		ChangeDigest:   item.digest(),
	}
}
