//go:build unit

package updater

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type checksumClientStub struct {
	checksum []byte
	err      error
}

func (s *checksumClientStub) FetchLatestRelease(context.Context, string) (*GitHubRelease, error) {
	panic("unexpected FetchLatestRelease")
}

func (s *checksumClientStub) FetchRecentReleases(context.Context, string, int) ([]*GitHubRelease, error) {
	panic("unexpected FetchRecentReleases")
}

func (s *checksumClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("unexpected DownloadFile")
}

func (s *checksumClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return s.checksum, s.err
}

func TestUpdateServiceClientInitializationFailurePrecedesCache(t *testing.T) {
	cache := &updateServiceCacheStub{data: `{"this":"must not be read"}`}
	client := &updateServiceGitHubClientStub{initErr: fmt.Errorf("token file is unreadable")}
	service := NewUpdateService(cache, client, "0.1.162-custom.8", "release")

	_, err := service.CheckUpdate(context.Background(), false)

	require.ErrorContains(t, err, "initialize private update client")
	require.ErrorContains(t, err, "token file is unreadable")
	require.Empty(t, client.latestRepos)
}

func TestUpdateServiceClientInitializationFailureBlocksRollbackLookup(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		initErr: fmt.Errorf("token file is unreadable"),
		recentReleases: []*GitHubRelease{{
			TagName: "v0.1.161-custom.1",
		}},
	}
	service := NewUpdateService(
		&updateServiceCacheStub{},
		client,
		"0.1.162-custom.8",
		"release",
	)

	_, err := service.ListRollbackVersions(context.Background())

	require.ErrorContains(t, err, "initialize private update client")
	require.Empty(t, client.recentRepos)
}

func TestUpdateServiceRejectsAnyNonPrivateInstallRepository(t *testing.T) {
	for _, repository := range []string{
		"Wei-Shaw/sub2api",
		"someone/other-custom",
		"o87110/sub2api-custom-public-fork",
	} {
		service := NewUpdateServiceForRepository(
			&updateServiceCacheStub{},
			&updateServiceGitHubClientStub{},
			"0.1.162-custom.8",
			"release",
			repository,
		)
		_, err := service.CheckUpdate(context.Background(), false)
		require.ErrorContains(t, err, `must be "o87110/sub2api-custom-public"`, repository)
	}
}

func TestAttachOfficialReleaseRejectsNilAndInvalidMetadata(t *testing.T) {
	tests := []struct {
		name    string
		release *GitHubRelease
	}{
		{name: "nil release"},
		{
			name: "invalid tag",
			release: &GitHubRelease{
				TagName:     "latest",
				PublishedAt: "2026-07-01T00:00:00Z",
				HTMLURL:     "https://github.com/Wei-Shaw/sub2api/releases/tag/latest",
			},
		},
		{
			name: "missing published metadata",
			release: &GitHubRelease{
				TagName: "v0.1.162",
				HTMLURL: "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.162",
			},
		},
		{
			name: "wrong release URL",
			release: &GitHubRelease{
				TagName:     "v0.1.162",
				PublishedAt: "2026-07-01T00:00:00Z",
				HTMLURL:     "https://example.com/v0.1.162",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &updateServiceGitHubClientStub{
				releasesByRepo: map[string]*GitHubRelease{
					customGitHubRepo: {
						TagName: "v0.1.162-custom.8",
						Assets:  validGitHubAssets(),
					},
					githubRepo: tt.release,
				},
			}
			service := NewUpdateService(&updateServiceCacheStub{}, client, "0.1.162-custom.8", "release")

			info, err := service.CheckUpdate(context.Background(), true)

			require.NoError(t, err)
			require.NotNil(t, info)
			require.Empty(t, info.OfficialLatestVersion)
			require.NotEmpty(t, info.OfficialReleaseWarning)
		})
	}
}

func TestVerifyChecksumRejectsDuplicateTargetEntries(t *testing.T) {
	filePath := filepath.Join(t.TempDir(), "archive.tar.gz")
	require.NoError(t, os.WriteFile(filePath, []byte("trusted archive"), 0600))
	sum := sha256.Sum256([]byte("trusted archive"))
	line := fmt.Sprintf("%x  %s\n", sum, filepath.Base(filePath))
	service := NewUpdateService(
		&updateServiceCacheStub{},
		&checksumClientStub{checksum: []byte(line + line)},
		"0.1.162-custom.8",
		"release",
	)

	err := service.verifyChecksum(context.Background(), filePath, "unused")

	require.ErrorContains(t, err, "exactly once")
}

func TestReplaceExecutablePreservesExistingBackupOnFailure(t *testing.T) {
	tempDir := t.TempDir()
	executable := filepath.Join(tempDir, "sub2api")
	backup := executable + ".backup"
	require.NoError(t, os.WriteFile(executable, []byte("current"), 0700))
	require.NoError(t, os.WriteFile(backup, []byte("previous backup"), 0700))

	err := replaceExecutable(executable, filepath.Join(tempDir, "missing-new-binary"))

	require.ErrorContains(t, err, "original executable and backup restored")
	currentBytes, readErr := os.ReadFile(executable)
	require.NoError(t, readErr)
	require.Equal(t, "current", string(currentBytes))
	backupBytes, readErr := os.ReadFile(backup)
	require.NoError(t, readErr)
	require.Equal(t, "previous backup", string(backupBytes))
}

func TestReplaceExecutableRotatesBackupAfterSuccessfulInstall(t *testing.T) {
	tempDir := t.TempDir()
	executable := filepath.Join(tempDir, "sub2api")
	backup := executable + ".backup"
	newBinary := filepath.Join(tempDir, "new-sub2api")
	require.NoError(t, os.WriteFile(executable, []byte("current"), 0700))
	require.NoError(t, os.WriteFile(backup, []byte("previous backup"), 0700))
	require.NoError(t, os.WriteFile(newBinary, []byte("new"), 0700))

	require.NoError(t, replaceExecutable(executable, newBinary))

	currentBytes, err := os.ReadFile(executable)
	require.NoError(t, err)
	require.Equal(t, "new", string(currentBytes))
	backupBytes, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, "current", string(backupBytes))
	previousBackups, err := filepath.Glob(filepath.Join(tempDir, ".sub2api-backup-previous-*"))
	require.NoError(t, err)
	require.Empty(t, previousBackups)
}

type tarEntry struct {
	name     string
	size     int64
	data     string
	typeflag byte
}

func writeTarGz(t *testing.T, entries []tarEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "payload.tar.gz")
	file, err := os.Create(path)
	require.NoError(t, err)
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		require.NoError(t, tarWriter.WriteHeader(&tar.Header{
			Name:     entry.name,
			Mode:     0600,
			Size:     entry.size,
			Typeflag: typeflag,
		}))
		if entry.data != "" {
			_, err = io.CopyN(tarWriter, strings.NewReader(entry.data), entry.size)
			require.NoError(t, err)
		} else if entry.size > 0 {
			_, err = io.CopyN(tarWriter, zeroReader{}, entry.size)
			require.NoError(t, err)
		}
	}
	require.NoError(t, tarWriter.Close())
	require.NoError(t, gzipWriter.Close())
	require.NoError(t, file.Close())
	return path
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestExtractBinaryRejectsSkippedExpansionLimitAndDuplicateBinary(t *testing.T) {
	service := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.8",
		"release",
	)

	t.Run("skipped expansion limit", func(t *testing.T) {
		archive := writeTarGz(t, []tarEntry{{name: "unused", size: maxSkippedBytes + 1}})
		err := service.extractBinary(context.Background(), archive, filepath.Join(t.TempDir(), "sub2api"))
		require.ErrorContains(t, err, "skipped archive entries exceed")
	})

	t.Run("non-regular skipped expansion limit", func(t *testing.T) {
		archive := writeTarGz(t, []tarEntry{{
			name:     "device-payload",
			size:     maxSkippedBytes + 1,
			typeflag: 'Z',
		}})
		err := service.extractBinary(context.Background(), archive, filepath.Join(t.TempDir(), "sub2api"))
		require.ErrorContains(t, err, "skipped archive entries exceed")
	})

	t.Run("duplicate binary", func(t *testing.T) {
		archive := writeTarGz(t, []tarEntry{
			{name: "first/sub2api", size: 1, data: "a"},
			{name: "second/sub2api", size: 1, data: "b"},
		})
		err := service.extractBinary(context.Background(), archive, filepath.Join(t.TempDir(), "sub2api"))
		require.ErrorContains(t, err, "multiple sub2api binaries")
	})

	t.Run("entry count limit", func(t *testing.T) {
		entries := make([]tarEntry, maxArchiveEntries+1)
		for i := range entries {
			entries[i] = tarEntry{name: fmt.Sprintf("empty-%d", i)}
		}
		archive := writeTarGz(t, entries)
		err := service.extractBinary(context.Background(), archive, filepath.Join(t.TempDir(), "sub2api"))
		require.ErrorContains(t, err, "archive contains more than")
	})
}

func TestExtractBinaryObservesCanceledContext(t *testing.T) {
	service := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.8",
		"release",
	)
	archive := writeTarGz(t, []tarEntry{{name: "sub2api", size: 1, data: "a"}})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := service.extractBinary(ctx, archive, filepath.Join(t.TempDir(), "sub2api"))

	require.ErrorIs(t, err, context.Canceled)
}

func TestCustomUpdateCacheUsesRepositoryAndSchemaNamespace(t *testing.T) {
	cache := newUpdateCache(nil).(*updateCache)
	require.Equal(t, "update:custom:o87110:sub2api-custom:v2", cache.key)
}

func TestCachedReleaseWithoutAssetIDIsRejected(t *testing.T) {
	cache := &updateServiceCacheStub{data: fmt.Sprintf(
		`{"repository":%q,"latest":"0.1.162-custom.8","release_info":{"assets":[{"name":"checksums.txt","download_url":"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/102","size":128}]},"official_release_warning":"unavailable","timestamp":%d}`,
		customGitHubRepo,
		time.Now().Unix(),
	)}
	service := NewUpdateService(cache, &updateServiceGitHubClientStub{}, "0.1.162-custom.7", "release")

	_, err := service.getFromCache(context.Background())

	require.ErrorContains(t, err, "asset ID must be a positive integer")
}

func TestBinaryVersionOutputRequiresExactCustomVersion(t *testing.T) {
	version, err := binaryVersionFromOutput("Sub2API v0.1.162-custom.8 (commit: test, built: test)\n")
	require.NoError(t, err)
	require.Equal(t, "0.1.162-custom.8", version)

	for _, output := range []string{
		"Sub2API 0.1.162",
		"Sub2API development",
		"v0.1.162-custom.8",
	} {
		_, err := binaryVersionFromOutput(output)
		require.Error(t, err, output)
	}
}
