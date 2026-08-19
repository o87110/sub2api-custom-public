//go:build unit

package moderation

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestViolationCounterPermanentlyExcludesOutOfScopeAuditRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	counter := NewViolationCounter(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND action <> 'cyber_policy_out_of_scope'")).
		WithArgs(int64(1001), since, false).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	count, err := counter.CountFlaggedByUserSince(context.Background(), 1001, since, false)

	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViolationCounterPreservesCyberPolicyExclusionParameter(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	counter := NewViolationCounter(db)
	since := time.Now().Add(-time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND ($3::bool IS FALSE OR action <> 'cyber_policy')")).
		WithArgs(int64(1001), since, true).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	count, err := counter.CountFlaggedByUserSince(context.Background(), 1001, since, true)

	require.NoError(t, err)
	require.Equal(t, 3, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViolationCounterPropagatesQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	counter := NewViolationCounter(db)
	mock.ExpectQuery("SELECT COUNT").
		WithArgs(int64(1001), sqlmock.AnyArg(), false).
		WillReturnError(errors.New("database unavailable"))

	_, err = counter.CountFlaggedByUserSince(context.Background(), 1001, time.Now(), false)

	require.ErrorContains(t, err, "count custom content moderation violations")
	require.ErrorContains(t, err, "database unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViolationCounterSkipsInvalidUser(t *testing.T) {
	counter := NewViolationCounter(nil)

	count, err := counter.CountFlaggedByUserSince(context.Background(), 0, time.Now(), false)

	require.NoError(t, err)
	require.Zero(t, count)
}

func TestViolationCounterCountsOnlyAPIAuditActions(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	counter := NewViolationCounter(db)
	since := time.Now().Add(-24 * time.Hour)
	mock.ExpectQuery(regexp.QuoteMeta("AND action IN ('allow', 'block')")).
		WithArgs(int64(1001), since).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	count, err := counter.CountAPIAuditFlaggedByUserSince(context.Background(), 1001, since)

	require.NoError(t, err)
	require.Equal(t, 4, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViolationCounterAPIAuditCountPropagatesQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	counter := NewViolationCounter(db)
	mock.ExpectQuery("SELECT COUNT").
		WithArgs(int64(1001), sqlmock.AnyArg()).
		WillReturnError(errors.New("database unavailable"))

	_, err = counter.CountAPIAuditFlaggedByUserSince(context.Background(), 1001, time.Now())

	require.ErrorContains(t, err, "count custom API audit violations")
	require.ErrorContains(t, err, "database unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestViolationCounterAPIAuditCountSkipsInvalidUser(t *testing.T) {
	counter := NewViolationCounter(nil)

	count, err := counter.CountAPIAuditFlaggedByUserSince(context.Background(), 0, time.Now())

	require.NoError(t, err)
	require.Zero(t, count)
}
