//go:build unit

package updater

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type updateServiceCacheStub struct {
	data string
}

func (s *updateServiceCacheStub) GetUpdateInfo(context.Context) (string, error) {
	if s.data == "" {
		return "", errors.New("cache miss")
	}
	return s.data, nil
}

func (s *updateServiceCacheStub) SetUpdateInfo(_ context.Context, data string, _ time.Duration) error {
	s.data = data
	return nil
}

type updateServiceGitHubClientStub struct {
	release         *GitHubRelease
	latestErr       error
	initErr         error
	releasesByRepo  map[string]*GitHubRelease
	latestErrByRepo map[string]error
	latestRepos     []string
	recentRepos     []string
	recentReleases  []*GitHubRelease
	recentErr       error
}

func (s *updateServiceGitHubClientStub) InitializationError() error {
	return s.initErr
}

func (s *updateServiceGitHubClientStub) FetchLatestRelease(_ context.Context, repo string) (*GitHubRelease, error) {
	s.latestRepos = append(s.latestRepos, repo)
	if s.latestErrByRepo != nil {
		if err, ok := s.latestErrByRepo[repo]; ok {
			return nil, err
		}
	}
	if s.releasesByRepo != nil {
		if release, ok := s.releasesByRepo[repo]; ok {
			return release, nil
		}
	}
	return s.release, s.latestErr
}

func (s *updateServiceGitHubClientStub) FetchRecentReleases(_ context.Context, repo string, _ int) ([]*GitHubRelease, error) {
	s.recentRepos = append(s.recentRepos, repo)
	return s.recentReleases, s.recentErr
}

func (s *updateServiceGitHubClientStub) DownloadFile(context.Context, string, string, int64) error {
	panic("DownloadFile should not be called when no update is available")
}

func (s *updateServiceGitHubClientStub) FetchChecksumFile(context.Context, string) ([]byte, error) {
	panic("FetchChecksumFile should not be called when no update is available")
}

func validGitHubAssets() []GitHubAsset {
	return []GitHubAsset{
		{
			ID:     101,
			Name:   "sub2api_0.1.162-custom.1_linux_amd64.tar.gz",
			APIURL: "https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/101",
			Size:   1024,
		},
		{
			ID:     102,
			Name:   "checksums.txt",
			APIURL: "https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/102",
			Size:   128,
		},
	}
}

func TestUpdateServicePerformUpdateNoUpdateReturnsSentinel(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{
			release: &GitHubRelease{
				TagName: "v0.1.132-custom.1",
				Name:    "v0.1.132-custom.1",
				Assets:  validGitHubAssets(),
			},
		},
		"0.1.132-custom.1",
		"release",
	)

	err := svc.PerformUpdate(context.Background())

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrNoUpdateAvailable))
	require.ErrorIs(t, err, ErrNoUpdateAvailable)
}

func newRollbackTestService(current string, releases []*GitHubRelease) *UpdateService {
	return NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentReleases: releases},
		current,
		"release",
	)
}

func TestUpdateServiceListRollbackVersionsFiltersAndCaps(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148-custom.1", PublishedAt: "2026-07-09T00:00:00Z"},                   // newer than current: excluded
		{TagName: "v0.1.147-custom.3", PublishedAt: "2026-07-08T00:00:00Z"},                   // current: excluded
		{TagName: "v0.1.146-custom.3", PublishedAt: "2026-07-07T12:00:00Z", Prerelease: true}, // GitHub prerelease: excluded
		{TagName: "v0.1.146-custom.2", PublishedAt: "2026-07-07T00:00:00Z"},
		{TagName: "v0.1.145-custom.1", PublishedAt: "2026-07-06T00:00:00Z", Draft: true}, // draft: excluded
		{TagName: "v0.1.144-custom.4", PublishedAt: "2026-07-05T00:00:00Z"},
		{TagName: "v0.1.144-custom.4", PublishedAt: "2026-07-05T00:00:00Z"}, // duplicate: excluded
		{TagName: "v0.1.143-custom.2", PublishedAt: "2026-07-04T00:00:00Z"},
		{TagName: "v0.1.142-custom.1", PublishedAt: "2026-07-03T00:00:00Z"}, // beyond cap of 3: excluded
		{TagName: "v0.1.141", PublishedAt: "2026-07-02T00:00:00Z"},          // official tag: excluded
	}
	svc := newRollbackTestService("0.1.147-custom.3", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146-custom.2", versions[0].Version)
	require.Equal(t, "0.1.144-custom.4", versions[1].Version)
	require.Equal(t, "0.1.143-custom.2", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsSortsUnorderedInput(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.144-custom.1"},
		{TagName: "v0.1.146-custom.1"},
		{TagName: "v0.1.145-custom.1"},
	}
	svc := newRollbackTestService("0.1.147-custom.1", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Len(t, versions, 3)
	require.Equal(t, "0.1.146-custom.1", versions[0].Version)
	require.Equal(t, "0.1.145-custom.1", versions[1].Version)
	require.Equal(t, "0.1.144-custom.1", versions[2].Version)
}

func TestUpdateServiceListRollbackVersionsEmptyWhenNoneOlder(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.147-custom.1"},
		{TagName: "v0.1.148-custom.1"},
	}
	svc := newRollbackTestService("0.1.147-custom.1", releases)

	versions, err := svc.ListRollbackVersions(context.Background())

	require.NoError(t, err)
	require.Empty(t, versions)
}

func TestUpdateServiceListRollbackVersionsPropagatesFetchError(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{recentErr: errors.New("github unavailable")},
		"0.1.147-custom.1",
		"release",
	)

	_, err := svc.ListRollbackVersions(context.Background())

	require.Error(t, err)
	require.Contains(t, err.Error(), "github unavailable")
}

func TestUpdateServiceRollbackToVersionRejectsDisallowedTargets(t *testing.T) {
	releases := []*GitHubRelease{
		{TagName: "v0.1.148-custom.1"},
		{TagName: "v0.1.147-custom.1"},
		{TagName: "v0.1.146-custom.1"},
		{TagName: "v0.1.145-custom.1"},
		{TagName: "v0.1.144-custom.1"},
		{TagName: "v0.1.143-custom.1"},
		{TagName: "v0.1.142-custom.1"},
	}
	svc := newRollbackTestService("0.1.147-custom.1", releases)

	for _, target := range []string{
		"",                  // empty
		"0.1.147-custom.1",  // current version
		"v0.1.147-custom.1", // current version with prefix
		"0.1.148-custom.1",  // newer than current
		"0.1.142-custom.1",  // older than the 3 most recent
		"9.9.9",             // nonexistent
	} {
		err := svc.RollbackToVersion(context.Background(), target)
		require.ErrorIs(t, err, ErrRollbackVersionNotAllowed, "target %q should be rejected", target)
	}
}

func TestUpdateServiceRollbackToVersionAcceptsVPrefix(t *testing.T) {
	// No platform asset in the release: the target passes the allowlist check
	// and fails later at asset lookup, proving the version itself was accepted.
	releases := []*GitHubRelease{
		{TagName: "v0.1.147-custom.1"},
		{TagName: "v0.1.146-custom.1"},
	}
	svc := newRollbackTestService("0.1.147-custom.1", releases)

	err := svc.RollbackToVersion(context.Background(), "v0.1.146-custom.1")

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrRollbackVersionNotAllowed)
	require.Contains(t, err.Error(), "no compatible release found")
}

func TestCompareVersionsSupportsCustomBuilds(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    int
	}{
		{name: "custom maintenance upgrade", current: "0.1.160-custom.1", latest: "0.1.160-custom.2", want: -1},
		{name: "custom maintenance downgrade", current: "0.1.160-custom.2", latest: "0.1.160-custom.1", want: 1},
		{name: "next official base", current: "0.1.160-custom.9", latest: "0.1.161-custom.1", want: -1},
		{name: "standard semver release beats prerelease", current: "0.1.160-custom.1", latest: "0.1.160", want: -1},
		{name: "optional v prefix", current: "v0.1.160-custom.2", latest: "0.1.160-custom.2", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compareVersions(tt.current, tt.latest))
		})
	}
}

func TestBaseUpdateVersion(t *testing.T) {
	require.Equal(t, "0.1.160", baseUpdateVersion("v0.1.160-custom.2"))
	require.Equal(t, "0.1.161", baseUpdateVersion("0.1.161"))
}

func TestUpdateServiceDefaultsToFixedPublicReleaseRepository(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.1",
		"release",
	)
	require.Equal(t, customGitHubRepo, svc.repository)

	blank := NewUpdateServiceForRepository(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.1",
		"release",
		" ",
	)
	require.Equal(t, customGitHubRepo, blank.repository)

	normalized := NewUpdateServiceForRepository(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.1",
		"release",
		"/o87110/sub2api-custom-public/",
	)
	require.Equal(t, customGitHubRepo, normalized.repository)
}

func TestUpdateServiceRejectsOfficialRepositoryAsInstallSource(t *testing.T) {
	client := &updateServiceGitHubClientStub{}
	svc := NewUpdateServiceForRepository(
		&updateServiceCacheStub{},
		client,
		"0.1.162-custom.1",
		"release",
		githubRepo,
	)

	info, err := svc.CheckUpdate(context.Background(), false)

	require.ErrorContains(t, err, "must be")
	require.NotNil(t, info)
	require.False(t, info.HasUpdate)
	require.Empty(t, client.latestRepos, "official repository must not be queried as an install source")
}

func TestUpdateServiceRejectsNonCustomLatestTag(t *testing.T) {
	client := &updateServiceGitHubClientStub{release: &GitHubRelease{TagName: "v0.1.163"}}
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		client,
		"0.1.162-custom.7",
		"release",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.ErrorContains(t, err, `invalid tag "v0.1.163"`)
	require.NotNil(t, info)
	require.False(t, info.HasUpdate)
	require.Equal(t, []string{customGitHubRepo}, client.latestRepos)
}

func TestUpdateServiceRejectsPollutedCustomCache(t *testing.T) {
	cache := &updateServiceCacheStub{data: fmt.Sprintf(
		`{"repository":%q,"latest":"0.1.163","release_info":{},"official_release_warning":"unavailable","timestamp":%d}`,
		customGitHubRepo,
		time.Now().Unix(),
	)}
	svc := NewUpdateService(
		cache,
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.7",
		"release",
	)

	_, err := svc.getFromCache(context.Background())

	require.ErrorContains(t, err, "invalid custom version")
}

func TestApplyReleaseAssetsRequiresChecksum(t *testing.T) {
	svc := NewUpdateService(
		&updateServiceCacheStub{},
		&updateServiceGitHubClientStub{},
		"0.1.162-custom.7",
		"release",
	)

	err := svc.applyReleaseAssets(context.Background(), "v0.1.163-custom.1", []Asset{{
		ID:          12345,
		Name:        "sub2api_0.1.163-custom.1_" + svc.getArchiveName() + ".tar.gz",
		DownloadURL: "https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		Size:        1024,
	}})

	require.ErrorContains(t, err, "required checksum asset")
}

func TestValidateDownloadURLRequiresConfiguredRepositoryAssetAPI(t *testing.T) {
	require.NoError(t, validateDownloadURL(
		"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		"o87110/sub2api-custom-public",
		12345,
	))

	for _, rawURL := range []string{
		"http://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		"https://api.github.com/repos/Wei-Shaw/sub2api/releases/assets/12345",
		"https://github.com/o87110/sub2api-custom-public/releases/download/v0.1.160/custom.tar.gz",
		"https://example.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/not-a-number",
		"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345?download=1",
		"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345#fragment",
	} {
		require.Error(t, validateDownloadURL(rawURL, "o87110/sub2api-custom-public", 12345), rawURL)
	}
	require.Error(t, validateDownloadURL(
		"https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		"o87110/sub2api-custom-public",
		54321,
	))
}

func TestUpdateServiceFixedPublicReleaseSourceFailureIsExplicitAndCacheIsRepositoryScoped(t *testing.T) {
	cache := &updateServiceCacheStub{
		data: fmt.Sprintf(
			`{"repository":"Wei-Shaw/sub2api","latest":"9.9.9","timestamp":%d}`,
			time.Now().Unix(),
		),
	}
	svc := NewUpdateServiceForRepository(
		cache,
		&updateServiceGitHubClientStub{latestErr: errors.New("GitHub API returned 401")},
		"0.1.160-custom.1",
		"release",
		"o87110/sub2api-custom-public",
	)

	_, cacheErr := svc.getFromCache(context.Background())
	require.ErrorContains(t, cacheErr, "repository mismatch")

	info, err := svc.CheckUpdate(context.Background(), false)
	require.ErrorContains(t, err, `fixed public Release source "o87110/sub2api-custom-public" is unavailable`)
	require.NotNil(t, info)
	require.Equal(t, "0.1.160", info.CurrentVersion)
	require.Equal(t, "0.1.160-custom.1", info.CurrentBuildVersion)
	require.False(t, info.HasUpdate)
}

func TestUpdateServiceCheckUpdateIncludesOfficialReleaseForCustomSource(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		releasesByRepo: map[string]*GitHubRelease{
			"o87110/sub2api-custom-public": {
				TagName: "v0.1.161-custom.6",
				Name:    "v0.1.161-custom.6",
				HTMLURL: "https://github.com/o87110/sub2api-custom-public/releases/tag/v0.1.161-custom.6",
				Assets:  validGitHubAssets(),
			},
			githubRepo: {
				TagName:     "v0.1.162",
				Name:        "v0.1.162",
				PublishedAt: "2026-07-01T00:00:00Z",
				HTMLURL:     "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.162",
			},
		},
	}
	svc := NewUpdateServiceForRepository(
		&updateServiceCacheStub{},
		client,
		"0.1.161-custom.6",
		"release",
		"o87110/sub2api-custom-public",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, []string{"o87110/sub2api-custom-public", githubRepo}, client.latestRepos)
	require.Equal(t, "0.1.161-custom.6", info.LatestVersion)
	require.False(t, info.HasUpdate)
	require.Equal(t, "o87110/sub2api-custom-public", info.UpdateRepository)
	require.Equal(t, githubRepo, info.OfficialRepository)
	require.Equal(t, "0.1.162", info.OfficialLatestVersion)
	require.True(t, info.HasOfficialUpdate)
	require.Equal(t, "https://github.com/Wei-Shaw/sub2api/releases/tag/v0.1.162", info.OfficialReleaseInfo.HTMLURL)
	require.Empty(t, info.OfficialReleaseInfo.Assets)
}

func TestUpdateServiceCheckUpdateIgnoresOfficialReleaseFailure(t *testing.T) {
	client := &updateServiceGitHubClientStub{
		releasesByRepo: map[string]*GitHubRelease{
			"o87110/sub2api-custom-public": {
				TagName: "v0.1.161-custom.6",
				Name:    "v0.1.161-custom.6",
				Assets:  validGitHubAssets(),
			},
		},
		latestErrByRepo: map[string]error{
			githubRepo: errors.New("official rate limited"),
		},
	}
	svc := NewUpdateServiceForRepository(
		&updateServiceCacheStub{},
		client,
		"0.1.161-custom.5",
		"release",
		"o87110/sub2api-custom-public",
	)

	info, err := svc.CheckUpdate(context.Background(), true)

	require.NoError(t, err)
	require.Equal(t, "0.1.161-custom.6", info.LatestVersion)
	require.True(t, info.HasUpdate)
	require.Empty(t, info.OfficialLatestVersion)
	require.False(t, info.HasOfficialUpdate)
	require.Contains(t, info.OfficialReleaseWarning, "official rate limited")
}

func TestGitHubAssetDownloadURLUsesAssetAPI(t *testing.T) {
	asset := GitHubAsset{
		ID:                 12345,
		APIURL:             "https://api.github.com/repos/o87110/sub2api-custom-public/releases/assets/12345",
		BrowserDownloadURL: "https://github.com/o87110/sub2api-custom-public/releases/download/tag/archive.tar.gz",
	}
	require.Equal(t, asset.APIURL, githubAssetDownloadURL(asset))
}
