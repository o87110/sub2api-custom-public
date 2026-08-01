package main

import (
	"os"
	"path/filepath"
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

func TestEnforceFinalBoundaryAllowsExactReviewedWriteException(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "go-sql",
		TargetSemantics: databaseboundary.Semantics{
			Statements: []string{"UPDATE users SET active = FALSE"},
		},
	}
	baselineCommit := "baseline-commit"
	entry := exactException(item, baselineCommit)
	entry.ReviewedWrite = true

	require.NoError(t, enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: entry},
		baselineCommit,
	))
}

func TestEnforceFinalBoundaryAllowsUnchangedBaselineWriteSQL(t *testing.T) {
	const existingWrite = "UPDATE users SET active = FALSE"
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "go-sql",
		BaseSemantics: databaseboundary.Semantics{
			Statements: []string{existingWrite},
		},
		TargetSemantics: databaseboundary.Semantics{
			Statements: []string{
				existingWrite,
				"SELECT id FROM users WHERE active = TRUE",
			},
		},
	}
	baselineCommit := "baseline-commit"

	require.NoError(t, enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: exactException(item, baselineCommit)},
		baselineCommit,
	))
}

func TestEnforceFinalBoundaryRejectsTargetDynamicSQL(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "go-sql",
		TargetSemantics: databaseboundary.Semantics{
			Dynamic: []string{`Query:"SELECT * FROM " + table`},
		},
	}
	baselineCommit := "baseline-commit"

	err := enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: exactException(item, baselineCommit)},
		baselineCommit,
	)
	require.EqualError(t, err, "dynamic or unresolved SQL changed in "+item.Path)
}

func TestEnforceFinalBoundaryAllowsUnchangedBaselineDynamicSQL(t *testing.T) {
	const inheritedDynamic = `Query:query`
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "go-sql",
		BaseSemantics: databaseboundary.Semantics{
			Dynamic: []string{inheritedDynamic},
		},
		TargetSemantics: databaseboundary.Semantics{
			Statements: []string{"SELECT group_id FROM api_key_groups WHERE api_key_id = $1"},
			Dynamic:    []string{inheritedDynamic},
		},
	}
	baselineCommit := "baseline-commit"

	require.NoError(t, enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: exactException(item, baselineCommit)},
		baselineCommit,
	))
}

func TestEnforceFinalBoundaryAllowsBaselineDynamicSQLCleanedInTarget(t *testing.T) {
	item := change{
		Status:     "M",
		Path:       "backend/internal/repository/example.go",
		BaseBlob:   "base-blob",
		TargetBlob: "target-blob",
		Kind:       "go-sql",
		BaseSemantics: databaseboundary.Semantics{
			Dynamic: []string{`Query:"SELECT * FROM " + table`},
		},
		TargetSemantics: databaseboundary.Semantics{
			Statements: []string{"SELECT id FROM users WHERE active = TRUE"},
		},
	}
	baselineCommit := "baseline-commit"

	require.NoError(t, enforceFinalBoundary(
		[]change{item},
		map[string]exception{item.Path: exactException(item, baselineCommit)},
		baselineCommit,
	))
}

func TestReadExceptionsParsesReviewedWriteMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exceptions.tsv")
	line := "backend/internal/repository/example.go\tbaseline\tbase\ttarget\tsemantic\tchange\treviewed-write\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))

	entries, err := readExceptions(path)
	require.NoError(t, err)
	require.True(t, entries["backend/internal/repository/example.go"].ReviewedWrite)
}

func TestReadExceptionsRejectsUnknownMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exceptions.tsv")
	line := "backend/internal/repository/example.go\tbaseline\tbase\ttarget\tsemantic\tchange\tallow-all\n"
	require.NoError(t, os.WriteFile(path, []byte(line), 0o600))

	_, err := readExceptions(path)
	require.EqualError(t, err, "invalid database exception mode at line 1")
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
