package affiliatereversal

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type recordingAuthCacheInvalidator struct {
	userIDs        []int64
	contexts       []context.Context
	contextHealthy []bool
}

func (r *recordingAuthCacheInvalidator) InvalidateAuthCacheByKey(context.Context, string) {}

func (r *recordingAuthCacheInvalidator) InvalidateAuthCacheByUserID(ctx context.Context, userID int64) {
	r.userIDs = append(r.userIDs, userID)
	r.contexts = append(r.contexts, ctx)
	_, hasDeadline := ctx.Deadline()
	r.contextHealthy = append(r.contextHealthy, ctx.Err() == nil && hasDeadline)
}

func (r *recordingAuthCacheInvalidator) InvalidateAuthCacheByGroupID(context.Context, int64) {}

type recordingBalanceCacheInvalidator struct {
	userIDs []int64
	errors  []error
}

func (r *recordingBalanceCacheInvalidator) InvalidateUserBalance(_ context.Context, userID int64) error {
	r.userIDs = append(r.userIDs, userID)
	if len(r.errors) == 0 {
		return nil
	}
	err := r.errors[0]
	r.errors = r.errors[1:]
	return err
}

func TestInvalidateCachesForOrdersRetriesAfterPriorFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	rows := sqlmock.NewRows([]string{"inviter_user_id"}).AddRow(int64(7)).AddRow(int64(12))
	mock.ExpectQuery("SELECT DISTINCT inviter_user_id").
		WithArgs("{101,102}").
		WillReturnRows(rows)
	mock.ExpectQuery("SELECT DISTINCT inviter_user_id").
		WithArgs("{101,102}").
		WillReturnRows(sqlmock.NewRows([]string{"inviter_user_id"}).AddRow(int64(7)).AddRow(int64(12)))

	auth := &recordingAuthCacheInvalidator{}
	balance := &recordingBalanceCacheInvalidator{errors: []error{errors.New("redis unavailable")}}
	svc := &Service{
		client:               client,
		authCacheInvalidator: auth,
		billingCacheService:  balance,
	}

	svc.InvalidateCachesForOrders([]int64{102, 101})
	svc.InvalidateCachesForOrders([]int64{101, 102})

	require.Equal(t, []int64{7, 12, 7, 12}, auth.userIDs)
	require.Equal(t, []int64{7, 12, 7, 12}, balance.userIDs)
	require.Len(t, auth.contexts, 4)
	require.Equal(t, []bool{true, true, true, true}, auth.contextHealthy)
	require.NoError(t, mock.ExpectationsWereMet())
}
