package config

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestCustomDistributionYAMLParses(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	files := []string{
		".github/workflows/backend-ci.yml",
		".github/workflows/upstream-sync.yml",
		".github/workflows/upstream-upgrade-gate.yml",
		".github/workflows/publish-custom.yml",
		".github/workflows/release.yml",
		".github/workflows/security-scan.yml",
		".goreleaser.yaml",
		".goreleaser.simple.yaml",
		"deploy/docker-compose.yml",
		"deploy/docker-compose.custom.yml",
		"deploy/rehearsal/docker-compose.yml",
	}

	for _, relativePath := range files {
		t.Run(relativePath, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
			require.NoError(t, err)

			var document yaml.Node
			require.NoError(t, yaml.Unmarshal(raw, &document))
			require.NotEmpty(t, document.Content)
		})
	}
}

func TestCustomWorkflowActionsAreImmutable(t *testing.T) {
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	workflows := []string{
		".github/workflows/backend-ci.yml",
		".github/workflows/publish-custom.yml",
		".github/workflows/release.yml",
		".github/workflows/security-scan.yml",
		".github/workflows/upstream-sync.yml",
		".github/workflows/upstream-upgrade-gate.yml",
	}
	remoteAction := regexp.MustCompile(`^[^/\s]+/[^/\s]+@[0-9a-f]{40}$`)
	dockerAction := regexp.MustCompile(`^docker://[^\s]+@sha256:[0-9a-f]{64}$`)

	for _, relativePath := range workflows {
		t.Run(relativePath, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(repositoryRoot, relativePath))
			require.NoError(t, err)

			var document yaml.Node
			require.NoError(t, yaml.Unmarshal(raw, &document))
			require.NotEmpty(t, document.Content)

			var usesNodes []*yaml.Node
			collectUsesNodes(&document, &usesNodes)
			for _, node := range usesNodes {
				value := node.Value
				switch {
				case strings.HasPrefix(value, "./"):
					continue
				case strings.HasPrefix(value, "docker://"):
					require.Regexp(t, dockerAction, value)
				default:
					require.Regexp(t, remoteAction, value)
					require.Regexp(t, `^# v[0-9]`, strings.TrimSpace(node.LineComment))
				}
			}
		})
	}
}

func collectUsesNodes(node *yaml.Node, result *[]*yaml.Node) {
	if node.Kind == yaml.MappingNode {
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			if key.Value == "uses" && value.Kind == yaml.ScalarNode {
				*result = append(*result, value)
			}
			collectUsesNodes(value, result)
		}
		return
	}
	for _, child := range node.Content {
		collectUsesNodes(child, result)
	}
}
