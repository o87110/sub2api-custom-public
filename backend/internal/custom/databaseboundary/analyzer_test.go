package databaseboundary

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAnalyzeGoRecognizesSQLStatements(t *testing.T) {
	source := []byte(`package fixture
import "context"
func query(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) any
	Exec(string, ...any) any
}) {
	const selectSQL = ` + "`SELECT id FROM users WHERE id = $1`" + `
	db.QueryRowContext(ctx, selectSQL, 1)
	db.Exec("INSERT INTO audit_logs(id) VALUES ($1)", 1)
	db.Exec("UPDATE users SET active = TRUE WHERE id = $1", 1)
	db.Exec("DELETE FROM sessions WHERE user_id = $1", 1)
	db.Exec("ALTER TABLE users ADD COLUMN note TEXT")
}`)

	semantics, err := AnalyzeGo("fixture.go", source)
	require.NoError(t, err)
	require.Equal(t, []string{
		"ALTER TABLE users ADD COLUMN note TEXT",
		"DELETE FROM sessions WHERE user_id = $1",
		"INSERT INTO audit_logs(id) VALUES ($1)",
		"SELECT id FROM users WHERE id = $1",
		"UPDATE users SET active = TRUE WHERE id = $1",
	}, semantics.Statements)
	require.Empty(t, semantics.Dynamic)
}

func TestAnalyzeGoRecognizesAdditionalDatabaseStatements(t *testing.T) {
	source := []byte(`package fixture
import "database/sql"
func query(db *sql.DB) {
	db.Exec("GRANT SELECT ON users TO reporter")
	db.Exec("VACUUM users")
	db.Query("PRAGMA table_info(users)")
	db.Exec("DO $$ BEGIN RAISE NOTICE 'test'; END $$")
}`)

	semantics, err := AnalyzeGo("repository/fixture.go", source)
	require.NoError(t, err)
	require.Equal(t, []string{
		"DO $$ BEGIN RAISE NOTICE 'test'; END $$",
		"GRANT SELECT ON users TO reporter",
		"PRAGMA table_info(users)",
		"VACUUM users",
	}, semantics.Statements)
	require.Empty(t, semantics.Dynamic)
}

func TestAnalyzeGoIgnoresCommentsAndIdentifiers(t *testing.T) {
	source := []byte(`package fixture
// SELECT and DELETE are documentation words, not executable SQL.
func updateSchemaComment() string { return "ordinary text" }
`)

	semantics, err := AnalyzeGo("fixture.go", source)
	require.NoError(t, err)
	require.True(t, semantics.Empty())
}

func TestAnalyzeGoFailsClosedOnDynamicQuery(t *testing.T) {
	source := []byte(`package fixture
import "database/sql"
func query(db *sql.DB, table string) {
	db.Query("SELECT * FROM " + table)
}`)

	semantics, err := AnalyzeGo("fixture.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{`Query:"SELECT * FROM " + table`}, semantics.Dynamic)
}

func TestAnalyzeGoFailsClosedOnDynamicDBReceiverWithoutSQLImport(t *testing.T) {
	source := []byte(`package fixture
import "context"
func query(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) any
}, table string) {
	db.QueryRowContext(ctx, "SELECT * FROM " + table)
}`)

	semantics, err := AnalyzeGo("fixture.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{`QueryRowContext:"SELECT * FROM " + table`}, semantics.Dynamic)
}

func TestAnalyzeGoDoesNotResolveMutableQueryFromAnotherBinding(t *testing.T) {
	source := []byte(`package fixture
import "database/sql"
func unsafeQuery(db *sql.DB, input string) {
	query := input
	db.Query(query)
}
func unrelated() string {
	query := "SELECT id FROM users"
	return query
}`)

	semantics, err := AnalyzeGo("repository/fixture.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{"Query:query"}, semantics.Dynamic)
}

func TestAnalyzeGoFailsClosedAfterQueryReassignment(t *testing.T) {
	source := []byte(`package fixture
import "database/sql"
func query(db *sql.DB, input string) {
	statement := "SELECT id FROM users"
	statement = input
	db.Query(statement)
}`)

	semantics, err := AnalyzeGo("repository/fixture.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{"Query:statement"}, semantics.Dynamic)
}

func TestAnalyzeGoTracksDatabaseQueryBuilderTerminals(t *testing.T) {
	source := []byte(`package fixture
import "context"
func query(ctx context.Context, client interface{}) {
	client.User.Query().Where(active()).All(ctx)
	client.User.Update().SetName("new").Save(ctx)
}`)

	semantics, err := AnalyzeGo("backend/internal/repository/user_repo.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{
		`Builder:client.User.Query().Where(active()).All(ctx)`,
		`Builder:client.User.Update().SetName("new").Save(ctx)`,
	}, semantics.Dynamic)
}

func TestAnalyzeGoTreatsSQLResultScanAsDecoding(t *testing.T) {
	source := []byte(`package fixture
import (
	"context"
	"database/sql"
)
func query(ctx context.Context, db *sql.DB, row *sql.Row, rows *sql.Rows) {
	var id int64
	db.QueryRowContext(ctx, "SELECT id FROM users WHERE id = $1", 1).Scan(&id)
	row.Scan(&id)
	rows.Scan(&id)
}`)

	semantics, err := AnalyzeGo("backend/internal/repository/user_repo.go", source)
	require.NoError(t, err)
	require.Equal(t, []string{"SELECT id FROM users WHERE id = $1"}, semantics.Statements)
	require.Empty(t, semantics.Dynamic)
}

func TestAnalyzeGoStillTracksEntBuilderScan(t *testing.T) {
	source := []byte(`package fixture
import "context"
func query(ctx context.Context, client interface{}, target any) {
	client.User.Query().Where(active()).Scan(ctx, target)
}`)

	semantics, err := AnalyzeGo("backend/internal/repository/user_repo.go", source)
	require.NoError(t, err)
	require.Empty(t, semantics.Statements)
	require.Equal(t, []string{
		`Builder:client.User.Query().Where(active()).Scan(ctx, target)`,
	}, semantics.Dynamic)
}

func TestAnalyzeGoBatchImageRepositoryKeepsKnownVendorDynamicSQLVisible(t *testing.T) {
	path := filepath.Join("..", "..", "repository", "batch_image_repo.go")
	source, err := os.ReadFile(path)
	require.NoError(t, err)

	semantics, err := AnalyzeGo(path, source)
	require.NoError(t, err)
	require.NotEmpty(t, semantics.Statements)
	require.ElementsMatch(t, []string{
		"ExecContext:` UPDATE batch_image_jobs SET status = ` + statusSQL + `, last_error_code = $2, last_error_message = $3, finished_at = CASE WHEN ` + statusSQL + ` = 'failed' AND finished_at IS NULL THEN $4 ELSE finished_at END, updated_at = $4, version = version + 1 WHERE batch_id = $1`",
		"QueryContext:query",
	}, semantics.Dynamic)
}

func TestAnalyzeGoIgnoresQueryBuilderNamesOutsideDatabasePaths(t *testing.T) {
	source := []byte(`package fixture
func update(store interface{}) {
	store.Update()
	store.Save()
}`)

	semantics, err := AnalyzeGo("backend/internal/service/cache.go", source)
	require.NoError(t, err)
	require.True(t, semantics.Empty())
}

func TestAnalyzeGoDoesNotTreatDynamicHTTPQueryAsDatabaseSQL(t *testing.T) {
	source := []byte(`package fixture
type requestContext interface { Query(string) string }
func read(c requestContext, key string) string {
	return c.Query(key)
}`)

	semantics, err := AnalyzeGo("handler.go", source)
	require.NoError(t, err)
	require.True(t, semantics.Empty())
}
