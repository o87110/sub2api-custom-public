package databaseboundary

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
)

var queryMethods = map[string]int{
	"Exec":            0,
	"ExecContext":     1,
	"NamedExec":       0,
	"NamedQuery":      0,
	"Prepare":         0,
	"PrepareContext":  1,
	"Query":           0,
	"QueryContext":    1,
	"QueryRow":        0,
	"QueryRowContext": 1,
	"Raw":             0,
}

var queryBuilderTerminals = map[string]struct{}{
	"All":     {},
	"Count":   {},
	"Exist":   {},
	"Find":    {},
	"First":   {},
	"FirstX":  {},
	"Get":     {},
	"GetX":    {},
	"Only":    {},
	"OnlyX":   {},
	"Save":    {},
	"SaveX":   {},
	"Scan":    {},
	"Take":    {},
	"Updates": {},
}

var directDatabaseTerminals = map[string]struct{}{
	"Create": {},
	"Delete": {},
	"Update": {},
}

var sqlPrefixes = []string{
	"ALTER ",
	"ANALYZE ",
	"CALL ",
	"COMMENT ",
	"COPY ",
	"CREATE ",
	"DELETE ",
	"DO ",
	"DROP ",
	"GRANT ",
	"INSERT ",
	"MERGE ",
	"PRAGMA ",
	"REVOKE ",
	"SELECT ",
	"TRUNCATE ",
	"UPDATE ",
	"VACUUM ",
	"WITH ",
}

// Semantics contains normalized SQL and unresolved database-call expressions.
type Semantics struct {
	Statements []string
	Dynamic    []string
}

// Empty reports whether the source contains no recognized SQL semantics.
func (s Semantics) Empty() bool {
	return len(s.Statements) == 0 && len(s.Dynamic) == 0
}

// Digest returns a stable SHA-256 over the normalized semantic summary.
func (s Semantics) Digest() string {
	sum := sha256.Sum256([]byte(strings.Join(s.Lines(), "\n")))
	return hex.EncodeToString(sum[:])
}

// Lines returns the stable manifest representation.
func (s Semantics) Lines() []string {
	lines := make([]string, 0, len(s.Statements)+len(s.Dynamic))
	for _, statement := range s.Statements {
		lines = append(lines, "sql\t"+statement)
	}
	for _, dynamic := range s.Dynamic {
		lines = append(lines, "dynamic\t"+dynamic)
	}
	return lines
}

// AnalyzeGo parses Go source without considering comments or ordinary identifiers.
func AnalyzeGo(filename string, source []byte) (Semantics, error) {
	fileset := token.NewFileSet()
	file, err := parser.ParseFile(fileset, filename, source, 0)
	if err != nil {
		return Semantics{}, fmt.Errorf("parse %s: %w", filename, err)
	}

	databaseSource := importsDatabasePackage(file)
	statements := make(map[string]struct{})
	dynamic := make(map[string]struct{})

	ast.Inspect(file, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			selector, ok := value.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			argumentIndex, ok := queryMethods[selector.Sel.Name]
			if !ok {
				if isDatabaseAccessPath(filename) {
					_, builderTerminal := queryBuilderTerminals[selector.Sel.Name]
					_, directTerminal := directDatabaseTerminals[selector.Sel.Name]
					if builderTerminal ||
						(directTerminal && (databaseSource || isDatabaseReceiver(selector.X))) {
						dynamic["Builder:"+formatExpression(fileset, value)] = struct{}{}
					}
				}
				return true
			}
			if argumentIndex >= len(value.Args) {
				return true
			}
			//nolint:staticcheck // ast.Object links are the parser's lightweight local-const resolver; type checking isolated files is not reliable.
			query, resolved := resolveString(value.Args[argumentIndex], map[*ast.Object]bool{})
			if !resolved {
				if databaseSource ||
					isDatabaseAccessPath(filename) ||
					isDatabaseReceiver(selector.X) ||
					strings.HasSuffix(selector.Sel.Name, "Context") {
					dynamic[selector.Sel.Name+":"+formatExpression(fileset, value.Args[argumentIndex])] = struct{}{}
				}
				return true
			}
			if normalized, sql := normalizeSQL(query); sql {
				statements[normalized] = struct{}{}
			}
		}
		return true
	})

	result := Semantics{
		Statements: sortedKeys(statements),
		Dynamic:    sortedKeys(dynamic),
	}
	return result, nil
}

func importsDatabasePackage(file *ast.File) bool {
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			continue
		}
		if path == "database/sql" ||
			strings.HasPrefix(path, "github.com/jackc/pgx") ||
			strings.HasPrefix(path, "github.com/jmoiron/sqlx") ||
			strings.HasPrefix(path, "gorm.io/gorm") {
			return true
		}
	}
	return false
}

func isDatabaseAccessPath(filename string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(filename, "\\", "/"))
	return strings.Contains(normalized, "/repository/") ||
		strings.Contains(normalized, "/database/") ||
		strings.Contains(normalized, "/db/") ||
		strings.HasSuffix(normalized, "_repo.go") ||
		strings.HasSuffix(normalized, "_repository.go")
}

func isDatabaseReceiver(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.Ident:
		switch strings.ToLower(value.Name) {
		case "conn", "db", "executor", "queryer", "sqlconn", "sqltx", "tx":
			return true
		}
	case *ast.SelectorExpr:
		switch strings.ToLower(value.Sel.Name) {
		case "conn", "db", "driver", "executor", "queryer", "tx":
			return true
		}
		return isDatabaseReceiver(value.X)
	case *ast.ParenExpr:
		return isDatabaseReceiver(value.X)
	}
	return false
}

//nolint:staticcheck // See the call site: parser object links keep local const resolution scoped and fail closed.
func resolveString(expression ast.Expr, resolving map[*ast.Object]bool) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind != token.STRING {
			return "", false
		}
		decoded, err := strconv.Unquote(value.Value)
		return decoded, err == nil
	case *ast.BinaryExpr:
		if value.Op != token.ADD {
			return "", false
		}
		left, leftOK := resolveString(value.X, resolving)
		right, rightOK := resolveString(value.Y, resolving)
		return left + right, leftOK && rightOK
	case *ast.ParenExpr:
		return resolveString(value.X, resolving)
	case *ast.Ident:
		object := value.Obj
		if object == nil || object.Kind != ast.Con || resolving[object] {
			return "", false
		}
		spec, ok := object.Decl.(*ast.ValueSpec)
		if !ok {
			return "", false
		}
		index := -1
		for candidateIndex, name := range spec.Names {
			if name.Obj == object {
				index = candidateIndex
				break
			}
		}
		if index < 0 || index >= len(spec.Values) {
			return "", false
		}
		resolving[object] = true
		resolved, resolvedOK := resolveString(spec.Values[index], resolving)
		delete(resolving, object)
		return resolved, resolvedOK
	default:
		return "", false
	}
}

func normalizeSQL(value string) (string, bool) {
	normalized := normalizeWhitespace(value)
	upper := strings.ToUpper(normalized)
	for _, prefix := range sqlPrefixes {
		if upper == strings.TrimSpace(prefix) || strings.HasPrefix(upper, prefix) {
			return normalized, true
		}
	}
	return "", false
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
}

func formatExpression(fileset *token.FileSet, expression ast.Expr) string {
	var output bytes.Buffer
	if err := format.Node(&output, fileset, expression); err != nil {
		return "@unformattable"
	}
	return normalizeWhitespace(output.String())
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	for index := 1; index < len(keys); index++ {
		for cursor := index; cursor > 0 && keys[cursor] < keys[cursor-1]; cursor-- {
			keys[cursor], keys[cursor-1] = keys[cursor-1], keys[cursor]
		}
	}
	return keys
}
