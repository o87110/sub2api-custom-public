package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/custom/databaseboundary"
)

const absentBlob = "@absent"

var databaseWritePattern = regexp.MustCompile(
	`(?i)(^|[^[:alnum:]_])(ALTER|CALL|COMMENT|COPY|CREATE|DELETE|DO|DROP|GRANT|INSERT|MERGE|REVOKE|TRUNCATE|UPDATE)([^[:alnum:]_]|$)`,
)

type change struct {
	Status          string
	Path            string
	BaseBlob        string
	TargetBlob      string
	Kind            string
	BaseSemantics   databaseboundary.Semantics
	TargetSemantics databaseboundary.Semantics
}

func (c change) line() string {
	return strings.Join([]string{
		c.Status,
		c.Path,
		c.BaseBlob,
		c.TargetBlob,
		c.Kind,
		c.BaseSemantics.Digest(),
		c.TargetSemantics.Digest(),
		c.digest(),
	}, "\t")
}

func (c change) digest() string {
	body := strings.Join([]string{
		c.Status,
		c.Path,
		c.BaseBlob,
		c.TargetBlob,
		c.Kind,
		strings.Join(c.BaseSemantics.Lines(), "\n"),
		strings.Join(c.TargetSemantics.Lines(), "\n"),
	}, "\x00")
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

type exception struct {
	Path           string
	BaseCommit     string
	BaseBlob       string
	TargetBlob     string
	SemanticDigest string
	ChangeDigest   string
}

func main() {
	var (
		repository     = flag.String("repo", "", "repository root")
		baseObject     = flag.String("base", "", "base commit or tree")
		targetObject   = flag.String("target", "", "target tree")
		mode           = flag.String("mode", "final", "final or report")
		baselineCommit = flag.String("baseline-commit", "", "explicit baseline commit for final mode")
		exceptionsPath = flag.String("exceptions", "", "exact final-mode exceptions TSV")
		manifestPath   = flag.String("manifest", "", "write normalized manifest to this path")
	)
	flag.Parse()

	if *repository == "" || *baseObject == "" || *targetObject == "" {
		fatal(errors.New("--repo, --base, and --target are required"))
	}
	if *mode != "final" && *mode != "report" {
		fatal(fmt.Errorf("unsupported mode %q", *mode))
	}

	root, err := filepath.Abs(*repository)
	if err != nil {
		fatal(err)
	}
	changes, err := inspectChanges(root, *baseObject, *targetObject)
	if err != nil {
		fatal(err)
	}

	lines := make([]string, 0, len(changes))
	for _, item := range changes {
		lines = append(lines, item.line())
	}
	sort.Strings(lines)
	manifest := strings.Join(lines, "\n")
	if manifest != "" {
		manifest += "\n"
	}
	if *manifestPath != "" {
		if err := os.WriteFile(*manifestPath, []byte(manifest), 0o600); err != nil {
			fatal(fmt.Errorf("write manifest: %w", err))
		}
	}

	fingerprint := sha256.Sum256([]byte(manifest))
	fmt.Printf("database_fingerprint=%s\n", hex.EncodeToString(fingerprint[:]))
	fmt.Printf("database_changed=%t\n", len(changes) > 0)

	if *mode == "report" {
		return
	}
	if *baselineCommit == "" || *exceptionsPath == "" {
		fatal(errors.New("final mode requires --baseline-commit and --exceptions"))
	}
	allowed, err := readExceptions(*exceptionsPath)
	if err != nil {
		fatal(err)
	}
	if err := enforceFinalBoundary(changes, allowed, *baselineCommit); err != nil {
		fatal(err)
	}
}

func inspectChanges(root, baseObject, targetObject string) ([]change, error) {
	output, err := git(root, "diff", "--name-status", "-z", "--no-renames", baseObject, targetObject)
	if err != nil {
		return nil, err
	}
	fields := bytes.Split(output, []byte{0})
	var changes []change
	for index := 0; index+1 < len(fields); index += 2 {
		status := string(fields[index])
		path := string(fields[index+1])
		if status == "" && path == "" {
			continue
		}
		if status != "A" && status != "M" && status != "D" {
			return nil, fmt.Errorf("unsupported Git status %q for %s", status, path)
		}
		if !isStructuralDatabasePath(path) && !strings.HasSuffix(path, ".go") {
			continue
		}

		item := change{
			Status:     status,
			Path:       path,
			BaseBlob:   absentBlob,
			TargetBlob: absentBlob,
		}
		if status != "A" {
			item.BaseBlob, err = blobOID(root, baseObject, path)
			if err != nil {
				return nil, err
			}
		}
		if status != "D" {
			item.TargetBlob, err = blobOID(root, targetObject, path)
			if err != nil {
				return nil, err
			}
		}

		if isStructuralDatabasePath(path) {
			item.Kind = "structural"
			changes = append(changes, item)
			continue
		}

		if status != "A" {
			source, readErr := git(root, "cat-file", "blob", baseObject+":"+path)
			if readErr != nil {
				return nil, readErr
			}
			item.BaseSemantics, err = databaseboundary.AnalyzeGo(path, source)
			if err != nil {
				return nil, err
			}
		}
		if status != "D" {
			source, readErr := git(root, "cat-file", "blob", targetObject+":"+path)
			if readErr != nil {
				return nil, readErr
			}
			item.TargetSemantics, err = databaseboundary.AnalyzeGo(path, source)
			if err != nil {
				return nil, err
			}
		}
		if equalSemantics(item.BaseSemantics, item.TargetSemantics) {
			continue
		}
		item.Kind = "go-sql"
		changes = append(changes, item)
	}
	sort.Slice(changes, func(left, right int) bool {
		return changes[left].Path < changes[right].Path
	})
	return changes, nil
}

func enforceFinalBoundary(changes []change, allowed map[string]exception, baselineCommit string) error {
	seen := make(map[string]bool)
	for _, item := range changes {
		if len(item.TargetSemantics.Dynamic) > 0 || len(item.BaseSemantics.Dynamic) > 0 {
			return fmt.Errorf("dynamic or unresolved SQL changed in %s", item.Path)
		}
		entry, ok := allowed[item.Path]
		if !ok {
			if item.Kind == "structural" {
				return fmt.Errorf("unapproved database structure change: %s", item.Path)
			}
			return fmt.Errorf("unapproved embedded SQL change: %s", item.Path)
		}
		if entry.BaseCommit != baselineCommit ||
			entry.BaseBlob != item.BaseBlob ||
			entry.TargetBlob != item.TargetBlob ||
			entry.SemanticDigest != item.TargetSemantics.Digest() ||
			entry.ChangeDigest != item.digest() {
			return fmt.Errorf("database exception fingerprint mismatch for %s", item.Path)
		}
		if item.Kind != "structural" {
			for _, statement := range item.TargetSemantics.Statements {
				upper := strings.ToUpper(strings.TrimSpace(statement))
				if !strings.HasPrefix(upper, "SELECT ") && !strings.HasPrefix(upper, "WITH ") {
					return fmt.Errorf("final database exception is not read-only in %s", item.Path)
				}
				if databaseWritePattern.MatchString(statement) {
					return fmt.Errorf("final database exception contains a write or DDL token in %s", item.Path)
				}
			}
		}
		seen[item.Path] = true
	}
	for path := range allowed {
		if !seen[path] {
			return fmt.Errorf("stale database exception has no matching semantic change: %s", path)
		}
	}
	return nil
}

func readExceptions(path string) (result map[string]exception, err error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open database exceptions: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			result = nil
			err = fmt.Errorf("close database exceptions: %w", closeErr)
		}
	}()

	result = make(map[string]exception)
	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			return nil, fmt.Errorf("invalid database exception line %d", lineNumber)
		}
		entry := exception{
			Path:           fields[0],
			BaseCommit:     fields[1],
			BaseBlob:       fields[2],
			TargetBlob:     fields[3],
			SemanticDigest: fields[4],
			ChangeDigest:   fields[5],
		}
		if entry.Path == "" || result[entry.Path].Path != "" {
			return nil, fmt.Errorf("empty or duplicate database exception at line %d", lineNumber)
		}
		result[entry.Path] = entry
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func isStructuralDatabasePath(path string) bool {
	normalized := strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(normalized, "backend/migrations/") ||
		strings.HasPrefix(normalized, "backend/ent/") ||
		strings.HasSuffix(strings.ToLower(normalized), ".sql") {
		return true
	}
	for _, segment := range strings.Split(strings.ToLower(normalized), "/") {
		if segment == "schema" || segment == "schemas" || segment == "database" {
			return true
		}
	}
	return false
}

func blobOID(root, object, path string) (string, error) {
	output, err := git(root, "rev-parse", object+":"+path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}

func git(root string, arguments ...string) ([]byte, error) {
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) {
		return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), strings.TrimSpace(string(exitError.Stderr)))
	}
	return nil, fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
}

func equalSemantics(left, right databaseboundary.Semantics) bool {
	return strings.Join(left.Lines(), "\n") == strings.Join(right.Lines(), "\n")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "ERROR:", err)
	os.Exit(1)
}
