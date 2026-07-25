package updater

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrNoUpdateAvailable         = infraerrors.Conflict("ALREADY_UP_TO_DATE", "no update available; current version is latest")
	ErrRollbackVersionNotAllowed = infraerrors.BadRequest("ROLLBACK_VERSION_NOT_ALLOWED", "version is not in the allowed rollback list")
)

const (
	updateCacheTTL   = 1200 // 20 minutes
	githubRepo       = "Wei-Shaw/sub2api"
	customGitHubRepo = "o87110/sub2api-custom-public"

	// Security: max download size (500MB)
	maxDownloadSize      = 500 * 1024 * 1024
	maxChecksumFileSize  = 1024 * 1024
	maxBinarySize        = 500 * 1024 * 1024
	maxSkippedBytes      = 64 * 1024 * 1024
	maxArchiveEntries    = 4096
	maxVersionOutput     = 8 * 1024
	binaryVersionTimeout = 5 * time.Second

	// Rollback: expose at most the 3 most recent versions older than current
	maxRollbackVersions = 3
	// Fetch a few extra releases so filtering (current/newer/prerelease) still leaves enough candidates
	rollbackFetchPageSize = 15
)

var (
	customReleaseTagPattern   = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+$`)
	officialReleaseTagPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	binaryVersionPattern      = regexp.MustCompile(`(?m)^.*Sub2API[[:space:]]+(v?[0-9]+\.[0-9]+\.[0-9]+-custom\.[0-9]+)(?:[[:space:]].*)?$`)
)

// UpdateCache defines cache operations for update service
type UpdateCache interface {
	GetUpdateInfo(ctx context.Context) (string, error)
	SetUpdateInfo(ctx context.Context, data string, ttl time.Duration) error
}

// GitHubReleaseClient 获取 GitHub release 信息的接口
type GitHubReleaseClient interface {
	FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error)
	FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error)
	DownloadFile(ctx context.Context, url, dest string, maxSize int64) error
	FetchChecksumFile(ctx context.Context, url string) ([]byte, error)
}

// UpdateService handles software updates
type UpdateService struct {
	cache          UpdateCache
	githubClient   GitHubReleaseClient
	clientInitErr  error
	currentVersion string
	buildType      string // "source" for manual builds, "release" for CI builds
	repository     string
}

// NewUpdateService creates a service using the trusted public custom Release repository.
func NewUpdateService(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType string) *UpdateService {
	return NewUpdateServiceForRepository(cache, githubClient, version, buildType, customGitHubRepo)
}

// NewUpdateServiceForRepository creates a service using a configurable Release source.
func NewUpdateServiceForRepository(cache UpdateCache, githubClient GitHubReleaseClient, version, buildType, repository string) *UpdateService {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	if repository == "" {
		repository = customGitHubRepo
	}
	var clientInitErr error
	if initialized, ok := githubClient.(githubReleaseClientInitialization); ok {
		clientInitErr = initialized.InitializationError()
	}
	return &UpdateService{
		cache:          cache,
		githubClient:   githubClient,
		clientInitErr:  clientInitErr,
		currentVersion: strings.TrimPrefix(strings.TrimSpace(version), "v"),
		buildType:      buildType,
		repository:     repository,
	}
}

// UpdateInfo contains update information
type UpdateInfo struct {
	CurrentVersion         string       `json:"current_version"`
	CurrentBuildVersion    string       `json:"current_build_version"`
	LatestVersion          string       `json:"latest_version"`
	HasUpdate              bool         `json:"has_update"`
	ReleaseInfo            *ReleaseInfo `json:"release_info,omitempty"`
	Cached                 bool         `json:"cached"`
	Warning                string       `json:"warning,omitempty"`
	BuildType              string       `json:"build_type"` // "source" or "release"
	UpdateRepository       string       `json:"update_repository"`
	OfficialRepository     string       `json:"official_repository"`
	OfficialLatestVersion  string       `json:"official_latest_version,omitempty"`
	HasOfficialUpdate      bool         `json:"has_official_update"`
	OfficialReleaseInfo    *ReleaseInfo `json:"official_release_info,omitempty"`
	OfficialReleaseWarning string       `json:"official_release_warning,omitempty"`
}

// ReleaseInfo contains GitHub release details
type ReleaseInfo struct {
	Name        string  `json:"name"`
	Body        string  `json:"body"`
	PublishedAt string  `json:"published_at"`
	HTMLURL     string  `json:"html_url"`
	Assets      []Asset `json:"assets,omitempty"`
}

// Asset represents a release asset
type Asset struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	DownloadURL string `json:"download_url"`
	Size        int64  `json:"size"`
}

// GitHubRelease represents GitHub API response
type GitHubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Body        string        `json:"body"`
	PublishedAt string        `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Assets      []GitHubAsset `json:"assets"`
}

// RollbackVersion describes a release version the system can roll back to
type RollbackVersion struct {
	Version     string `json:"version"` // without "v" prefix, e.g. "0.1.160-custom.1"
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
}

type GitHubAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	APIURL             string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// CheckUpdate checks for available updates
func (s *UpdateService) CheckUpdate(ctx context.Context, force bool) (*UpdateInfo, error) {
	if err := validateCustomUpdateRepository(s.repository); err != nil {
		return &UpdateInfo{
			CurrentVersion:      baseUpdateVersion(s.currentVersion),
			CurrentBuildVersion: s.currentVersion,
			LatestVersion:       s.currentVersion,
			HasUpdate:           false,
			BuildType:           s.buildType,
			UpdateRepository:    s.repository,
			OfficialRepository:  githubRepo,
		}, err
	}
	if s.clientInitErr != nil {
		return &UpdateInfo{
			CurrentVersion:      baseUpdateVersion(s.currentVersion),
			CurrentBuildVersion: s.currentVersion,
			LatestVersion:       s.currentVersion,
			HasUpdate:           false,
			BuildType:           s.buildType,
			UpdateRepository:    s.repository,
			OfficialRepository:  githubRepo,
		}, fmt.Errorf("initialize fixed public Release source client (optional GitHub Token): %w", s.clientInitErr)
	}

	// Try cache first.
	if !force {
		if cached, err := s.getFromCache(ctx); err == nil && cached != nil {
			return cached, nil
		}
	}

	info, err := s.fetchLatestRelease(ctx)
	if err != nil {
		current := &UpdateInfo{
			CurrentVersion:      baseUpdateVersion(s.currentVersion),
			CurrentBuildVersion: s.currentVersion,
			LatestVersion:       s.currentVersion,
			HasUpdate:           false,
			BuildType:           s.buildType,
			UpdateRepository:    s.repository,
			OfficialRepository:  githubRepo,
		}

		// The fixed public Release source is authoritative for installation,
		// update, and rollback. Never hide its source or optional GitHub Token
		// failure behind the official upstream or stale cached data.
		return current, fmt.Errorf("fixed public Release source %q is unavailable: %w", s.repository, err)
	}

	s.attachOfficialRelease(ctx, info)
	s.saveToCache(ctx, info)
	return info, nil
}

// PerformUpdate downloads and applies the update
// Uses atomic file replacement pattern for safe in-place updates
func (s *UpdateService) PerformUpdate(ctx context.Context) error {
	info, err := s.CheckUpdate(ctx, true)
	if err != nil {
		return err
	}

	if !info.HasUpdate {
		return ErrNoUpdateAvailable
	}

	return s.applyReleaseAssets(ctx, "v"+info.LatestVersion, info.ReleaseInfo.Assets)
}

// applyReleaseAssets downloads the platform archive from the given release assets,
// verifies its checksum, and atomically swaps the running binary.
// Shared by PerformUpdate (latest) and RollbackToVersion (specific older version).
func (s *UpdateService) applyReleaseAssets(ctx context.Context, targetTag string, releaseAssets []Asset) error {
	targetVersion, ok := customVersionFromReleaseTag(targetTag)
	if !ok {
		return fmt.Errorf("invalid target custom tag %q", targetTag)
	}
	archiveName := fmt.Sprintf("sub2api_%s_%s.tar.gz", targetVersion, s.getArchiveName())
	var archiveAsset *Asset
	var checksumAsset *Asset

	for i := range releaseAssets {
		asset := &releaseAssets[i]
		if asset.Name == archiveName {
			if archiveAsset != nil {
				return fmt.Errorf("multiple compatible release assets found for %s/%s", runtime.GOOS, runtime.GOARCH)
			}
			archiveAsset = asset
		}
		if asset.Name == "checksums.txt" {
			if checksumAsset != nil {
				return fmt.Errorf("multiple checksum assets found")
			}
			checksumAsset = asset
		}
	}

	if archiveAsset == nil || strings.TrimSpace(archiveAsset.DownloadURL) == "" {
		return fmt.Errorf("no compatible release found for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if filepath.Base(archiveAsset.Name) != archiveAsset.Name {
		return fmt.Errorf("invalid release asset name %q", archiveAsset.Name)
	}
	if checksumAsset == nil {
		return fmt.Errorf("required checksum asset checksums.txt is missing")
	}

	if err := validateDownloadURL(archiveAsset.DownloadURL, s.repository, archiveAsset.ID); err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if strings.TrimSpace(checksumAsset.DownloadURL) == "" {
		return fmt.Errorf("checksum asset is missing its API download URL")
	}
	if err := validateDownloadURL(checksumAsset.DownloadURL, s.repository, checksumAsset.ID); err != nil {
		return fmt.Errorf("invalid checksum URL: %w", err)
	}

	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return fmt.Errorf("failed to resolve symlinks: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	tempDir, err := os.MkdirTemp(exeDir, ".sub2api-update-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Asset API URLs end with an opaque numeric ID, so preserve the release
	// asset name for archive detection and checksum lookup.
	archivePath := filepath.Join(tempDir, archiveAsset.Name)
	if err := s.downloadFile(ctx, archiveAsset.DownloadURL, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if err := s.verifyChecksum(ctx, archivePath, checksumAsset.DownloadURL); err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}

	newBinaryPath := filepath.Join(tempDir, "sub2api")
	if err := s.extractBinary(ctx, archivePath, newBinaryPath); err != nil {
		return fmt.Errorf("extraction failed: %w", err)
	}
	if err := validateExtractedBinary(newBinaryPath); err != nil {
		return fmt.Errorf("invalid extracted binary: %w", err)
	}
	if err := os.Chmod(newBinaryPath, 0755); err != nil {
		return fmt.Errorf("chmod failed: %w", err)
	}
	if err := validateBinaryVersion(ctx, newBinaryPath, targetTag); err != nil {
		return fmt.Errorf("binary version verification failed: %w", err)
	}
	if err := replaceExecutable(exePath, newBinaryPath); err != nil {
		return err
	}

	return nil
}

// ListRollbackVersions returns up to maxRollbackVersions release versions that are
// strictly older than the current version (the current version itself is excluded),
// newest first. Draft and prerelease entries are skipped.
func (s *UpdateService) ListRollbackVersions(ctx context.Context) ([]RollbackVersion, error) {
	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return nil, err
	}

	versions := make([]RollbackVersion, 0, len(releases))
	for _, r := range releases {
		versions = append(versions, RollbackVersion{
			Version:     strings.TrimPrefix(r.TagName, "v"),
			Name:        r.Name,
			Body:        r.Body,
			PublishedAt: r.PublishedAt,
			HTMLURL:     r.HTMLURL,
		})
	}
	return versions, nil
}

// RollbackToVersion downloads and installs a specific older version.
// The target must be one of the versions returned by ListRollbackVersions;
// anything else (including the current version) is rejected.
func (s *UpdateService) RollbackToVersion(ctx context.Context, version string) error {
	target := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if target == "" {
		return ErrRollbackVersionNotAllowed
	}

	releases, err := s.fetchRollbackCandidates(ctx)
	if err != nil {
		return err
	}

	var match *GitHubRelease
	for _, r := range releases {
		if strings.TrimPrefix(r.TagName, "v") == target {
			match = r
			break
		}
	}
	if match == nil {
		return ErrRollbackVersionNotAllowed
	}

	assets := make([]Asset, len(match.Assets))
	for i, a := range match.Assets {
		assets[i] = Asset{
			ID:          a.ID,
			Name:        a.Name,
			DownloadURL: githubAssetDownloadURL(a),
			Size:        a.Size,
		}
	}

	return s.applyReleaseAssets(ctx, match.TagName, assets)
}

// fetchRollbackCandidates fetches recent releases and keeps the newest
// maxRollbackVersions entries strictly older than the current version.
func (s *UpdateService) fetchRollbackCandidates(ctx context.Context) ([]*GitHubRelease, error) {
	if err := validateCustomUpdateRepository(s.repository); err != nil {
		return nil, err
	}
	if s.clientInitErr != nil {
		return nil, fmt.Errorf("initialize fixed public Release source client (optional GitHub Token): %w", s.clientInitErr)
	}
	releases, err := s.githubClient.FetchRecentReleases(ctx, s.repository, rollbackFetchPageSize)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool, len(releases))
	candidates := make([]*GitHubRelease, 0, maxRollbackVersions)
	for _, r := range releases {
		if r == nil || r.Draft || r.Prerelease {
			continue
		}
		v, ok := customVersionFromReleaseTag(r.TagName)
		if !ok || seen[v] {
			continue
		}
		// Only versions strictly older than current (also excludes current itself)
		if compareVersions(v, s.currentVersion) >= 0 {
			continue
		}
		seen[v] = true
		candidates = append(candidates, r)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return compareVersions(
			strings.TrimPrefix(candidates[i].TagName, "v"),
			strings.TrimPrefix(candidates[j].TagName, "v"),
		) > 0
	})

	if len(candidates) > maxRollbackVersions {
		candidates = candidates[:maxRollbackVersions]
	}
	return candidates, nil
}

func (s *UpdateService) fetchLatestRelease(ctx context.Context) (*UpdateInfo, error) {
	if err := validateCustomUpdateRepository(s.repository); err != nil {
		return nil, err
	}
	release, err := s.githubClient.FetchLatestRelease(ctx, s.repository)
	if err != nil {
		return nil, err
	}

	if release == nil {
		return nil, fmt.Errorf("custom update source returned an empty release")
	}
	latestVersion, ok := customVersionFromReleaseTag(release.TagName)
	if !ok {
		return nil, fmt.Errorf("custom update source returned invalid tag %q", release.TagName)
	}
	releaseInfo := releaseInfoFromGitHubRelease(release, true)
	if err := validateReleaseAssetMetadata(release.TagName, releaseInfo.Assets, s.repository); err != nil {
		return nil, fmt.Errorf("custom update source returned invalid assets: %w", err)
	}
	info := &UpdateInfo{
		CurrentVersion:      baseUpdateVersion(s.currentVersion),
		CurrentBuildVersion: s.currentVersion,
		LatestVersion:       latestVersion,
		HasUpdate:           compareVersions(s.currentVersion, latestVersion) < 0,
		ReleaseInfo:         releaseInfo,
		Cached:              false,
		BuildType:           s.buildType,
		UpdateRepository:    s.repository,
		OfficialRepository:  githubRepo,
	}
	return info, nil
}

func (s *UpdateService) attachOfficialRelease(ctx context.Context, info *UpdateInfo) {
	if info == nil {
		return
	}
	info.UpdateRepository = s.repository
	info.OfficialRepository = githubRepo
	release, err := s.githubClient.FetchLatestRelease(ctx, githubRepo)
	if err != nil {
		info.OfficialReleaseWarning = err.Error()
		return
	}

	if err := validateOfficialRelease(release); err != nil {
		info.OfficialReleaseWarning = err.Error()
		return
	}
	latestVersion := strings.TrimPrefix(release.TagName, "v")
	info.OfficialLatestVersion = latestVersion
	info.HasOfficialUpdate = compareVersions(baseUpdateVersion(s.currentVersion), latestVersion) < 0
	info.OfficialReleaseInfo = releaseInfoFromGitHubRelease(release, false)
}

func releaseInfoFromGitHubRelease(release *GitHubRelease, includeAssets bool) *ReleaseInfo {
	if release == nil {
		return nil
	}
	info := &ReleaseInfo{
		Name:        release.Name,
		Body:        release.Body,
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
	}
	if includeAssets {
		assets := make([]Asset, len(release.Assets))
		for i, a := range release.Assets {
			assets[i] = Asset{
				ID:          a.ID,
				Name:        a.Name,
				DownloadURL: githubAssetDownloadURL(a),
				Size:        a.Size,
			}
		}
		info.Assets = assets
	}
	return info
}

func validateCustomUpdateRepository(repository string) error {
	repository = strings.Trim(strings.TrimSpace(repository), "/")
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("invalid custom update repository %q", repository)
	}
	if !strings.EqualFold(repository, customGitHubRepo) {
		return fmt.Errorf("custom update repository must be %q, got %q", customGitHubRepo, repository)
	}
	return nil
}

func validateOfficialRelease(release *GitHubRelease) error {
	if release == nil {
		return fmt.Errorf("official update source returned an empty release")
	}
	if !officialReleaseTagPattern.MatchString(strings.TrimSpace(release.TagName)) ||
		!semver.IsValid(strings.TrimSpace(release.TagName)) {
		return fmt.Errorf("official update source returned invalid tag %q", release.TagName)
	}
	if release.Draft || release.Prerelease {
		return fmt.Errorf("official update source returned a draft or prerelease")
	}
	if strings.TrimSpace(release.PublishedAt) == "" {
		return fmt.Errorf("official update source returned incomplete published metadata")
	}
	releaseURL, err := url.Parse(strings.TrimSpace(release.HTMLURL))
	if err != nil ||
		!strings.EqualFold(releaseURL.Scheme, "https") ||
		!strings.EqualFold(releaseURL.Host, "github.com") ||
		releaseURL.RawQuery != "" ||
		releaseURL.Fragment != "" ||
		!strings.EqualFold(strings.Trim(releaseURL.Path, "/"), "Wei-Shaw/sub2api/releases/tag/"+release.TagName) {
		return fmt.Errorf("official update source returned invalid release URL")
	}
	return nil
}

func validateReleaseAssetMetadata(targetTag string, assets []Asset, repository string) error {
	if _, ok := customVersionFromReleaseTag(targetTag); !ok {
		return fmt.Errorf("invalid target custom tag %q", targetTag)
	}
	if len(assets) == 0 {
		return fmt.Errorf("release asset metadata is empty")
	}
	for _, asset := range assets {
		if filepath.Base(asset.Name) != asset.Name || strings.TrimSpace(asset.Name) == "" {
			return fmt.Errorf("invalid release asset name %q", asset.Name)
		}
		if asset.Size <= 0 {
			return fmt.Errorf("release asset %q has invalid size", asset.Name)
		}
		if err := validateDownloadURL(asset.DownloadURL, repository, asset.ID); err != nil {
			return fmt.Errorf("release asset %q: %w", asset.Name, err)
		}
	}
	return nil
}

func customVersionFromReleaseTag(tag string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if !customReleaseTagPattern.MatchString(tag) || !semver.IsValid(tag) {
		return "", false
	}
	return strings.TrimPrefix(tag, "v"), true
}

func isCustomBuildVersion(version string) bool {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	_, ok := customVersionFromReleaseTag("v" + version)
	return ok
}

func (s *UpdateService) downloadFile(ctx context.Context, downloadURL, dest string) error {
	return s.githubClient.DownloadFile(ctx, downloadURL, dest, maxDownloadSize)
}

func (s *UpdateService) getArchiveName() string {
	osName := runtime.GOOS
	arch := runtime.GOARCH
	return fmt.Sprintf("%s_%s", osName, arch)
}

// validateDownloadURL accepts only GitHub Asset API URLs for the configured repository.
// SECURITY: This prevents SSRF and ensures downloads only come from trusted GitHub domains
func validateDownloadURL(rawURL, repository string, expectedAssetID int64) error {
	parsedURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if expectedAssetID <= 0 {
		return fmt.Errorf("asset ID must be a positive integer")
	}
	if parsedURL.Scheme != "https" {
		return fmt.Errorf("only HTTPS URLs are allowed")
	}
	if parsedURL.User != nil {
		return fmt.Errorf("URL credentials are not allowed")
	}
	if parsedURL.RawQuery != "" || parsedURL.Fragment != "" || parsedURL.ForceQuery {
		return fmt.Errorf("asset URL query and fragment are not allowed")
	}
	if parsedURL.RawPath != "" || parsedURL.EscapedPath() != parsedURL.Path {
		return fmt.Errorf("escaped asset URL paths are not allowed")
	}
	if !strings.EqualFold(parsedURL.Hostname(), "api.github.com") || parsedURL.Port() != "" {
		return fmt.Errorf("release assets must use the GitHub Asset API")
	}

	repoParts := strings.Split(strings.Trim(strings.TrimSpace(repository), "/"), "/")
	if len(repoParts) != 2 || repoParts[0] == "" || repoParts[1] == "" {
		return fmt.Errorf("invalid update repository %q", repository)
	}
	pathParts := strings.Split(strings.Trim(parsedURL.Path, "/"), "/")
	if len(pathParts) != 6 ||
		pathParts[0] != "repos" ||
		!strings.EqualFold(pathParts[1], repoParts[0]) ||
		!strings.EqualFold(pathParts[2], repoParts[1]) ||
		pathParts[3] != "releases" ||
		pathParts[4] != "assets" {
		return fmt.Errorf("asset URL does not match configured repository %q", repository)
	}
	urlAssetID, err := strconv.ParseInt(pathParts[5], 10, 64)
	if err != nil || urlAssetID <= 0 {
		return fmt.Errorf("asset URL must end in a positive numeric ID")
	}
	if urlAssetID != expectedAssetID {
		return fmt.Errorf("asset URL ID %d does not match metadata ID %d", urlAssetID, expectedAssetID)
	}

	return nil
}

func (s *UpdateService) verifyChecksum(ctx context.Context, filePath, checksumURL string) error {
	// Download checksums file
	checksumData, err := s.githubClient.FetchChecksumFile(ctx, checksumURL)
	if err != nil {
		return fmt.Errorf("failed to download checksums: %w", err)
	}

	// Calculate file hash
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Find one and only one valid expected hash in the checksums file.
	fileName := filepath.Base(filePath)
	scanner := bufio.NewScanner(strings.NewReader(string(checksumData)))
	var expectedHash string
	matchCount := 0
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == fileName {
			matchCount++
			decoded, decodeErr := hex.DecodeString(parts[0])
			if decodeErr != nil || len(decoded) != sha256.Size || len(parts[0]) != sha256.Size*2 {
				return fmt.Errorf("invalid checksum entry for %s", fileName)
			}
			expectedHash = strings.ToLower(parts[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read checksums: %w", err)
	}
	if matchCount == 0 {
		return fmt.Errorf("checksum not found for %s", fileName)
	}
	if matchCount != 1 {
		return fmt.Errorf("checksum for %s must appear exactly once", fileName)
	}
	if expectedHash != actualHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

func (s *UpdateService) extractBinary(ctx context.Context, archivePath, destPath string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	var reader io.Reader = &contextReader{ctx: ctx, reader: f}

	// Handle gzip compression
	if strings.HasSuffix(archivePath, ".gz") || strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".tgz") {
		gzr, err := gzip.NewReader(reader)
		if err != nil {
			return err
		}
		defer func() { _ = gzr.Close() }()
		reader = gzr
	}

	// Handle tar archive
	if strings.Contains(archivePath, ".tar") {
		tr := tar.NewReader(reader)
		var totalExpanded int64
		var skippedExpanded int64
		entryCount := 0
		foundBinary := false
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}

			entryCount++
			if entryCount > maxArchiveEntries {
				return fmt.Errorf("archive contains more than %d entries", maxArchiveEntries)
			}
			if hdr.Size < 0 {
				return fmt.Errorf("archive entry %q has a negative size", hdr.Name)
			}
			if hdr.Size > maxBinarySize+maxSkippedBytes-totalExpanded {
				return fmt.Errorf("archive expanded size exceeds %d bytes", maxBinarySize+maxSkippedBytes)
			}
			totalExpanded += hdr.Size

			// SECURITY: Prevent Zip Slip / Path Traversal attack
			// Only allow files with safe base names, no directory traversal
			baseName := filepath.Base(hdr.Name)

			// Check for path traversal attempts
			if strings.Contains(hdr.Name, "..") {
				return fmt.Errorf("path traversal attempt detected: %s", hdr.Name)
			}

			// Validate the entry is a regular file
			if hdr.Typeflag != tar.TypeReg {
				if hdr.Size > maxSkippedBytes-skippedExpanded {
					return fmt.Errorf("skipped archive entries exceed %d bytes", maxSkippedBytes)
				}
				skippedExpanded += hdr.Size
				if _, err := io.CopyN(io.Discard, tr, hdr.Size); err != nil {
					return fmt.Errorf("discard archive entry %q: %w", hdr.Name, err)
				}
				continue
			}

			// Only extract the specific binary we need
			if baseName == "sub2api" || baseName == "sub2api.exe" {
				if foundBinary {
					return fmt.Errorf("archive contains multiple sub2api binaries")
				}
				foundBinary = true
				if hdr.Size <= 0 {
					return fmt.Errorf("binary is empty")
				}
				if hdr.Size > maxBinarySize {
					return fmt.Errorf("binary too large: %d bytes (max %d)", hdr.Size, maxBinarySize)
				}

				out, err := os.Create(destPath)
				if err != nil {
					return err
				}

				written, err := io.CopyN(out, tr, hdr.Size)
				if err != nil {
					_ = out.Close()
					_ = os.Remove(destPath)
					return err
				}
				if written != hdr.Size {
					_ = out.Close()
					_ = os.Remove(destPath)
					return fmt.Errorf("binary size mismatch: expected %d bytes, extracted %d", hdr.Size, written)
				}
				if err := out.Close(); err != nil {
					_ = os.Remove(destPath)
					return err
				}
				continue
			}

			if hdr.Size > maxSkippedBytes-skippedExpanded {
				return fmt.Errorf("skipped archive entries exceed %d bytes", maxSkippedBytes)
			}
			skippedExpanded += hdr.Size
			if _, err := io.CopyN(io.Discard, tr, hdr.Size); err != nil {
				return fmt.Errorf("discard archive entry %q: %w", hdr.Name, err)
			}
		}
		if !foundBinary {
			return fmt.Errorf("binary not found in archive")
		}
		return nil
	}

	// Direct copy for non-tar files (with size limit)
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}

	limited := io.LimitReader(reader, maxBinarySize+1)
	written, err := io.Copy(out, limited)
	if err != nil {
		_ = out.Close()
		return err
	}
	if written == 0 {
		_ = out.Close()
		return fmt.Errorf("binary is empty")
	}
	if written > maxBinarySize {
		_ = out.Close()
		return fmt.Errorf("binary too large: %d bytes (max %d)", written, maxBinarySize)
	}
	return out.Close()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if err == nil {
		if contextErr := r.ctx.Err(); contextErr != nil {
			return n, contextErr
		}
	}
	return n, err
}

type cappedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	remaining := b.limit - b.buffer.Len()
	if remaining <= 0 {
		b.exceeded = true
		return 0, fmt.Errorf("version output exceeds %d bytes", b.limit)
	}
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining])
		b.exceeded = true
		return remaining, fmt.Errorf("version output exceeds %d bytes", b.limit)
	}
	return b.buffer.Write(p)
}

func (b *cappedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

func (b *cappedBuffer) Exceeded() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exceeded
}

func validateBinaryVersion(ctx context.Context, path, targetTag string) error {
	return validateBinaryVersionWithTimeout(ctx, path, targetTag, binaryVersionTimeout)
}

func validateBinaryVersionWithTimeout(ctx context.Context, path, targetTag string, timeout time.Duration) error {
	expectedVersion, ok := customVersionFromReleaseTag(targetTag)
	if !ok {
		return fmt.Errorf("invalid target custom tag %q", targetTag)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	versionCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var output cappedBuffer
	output.limit = maxVersionOutput
	command := exec.CommandContext(versionCtx, path, "--version")
	configureVersionCommand(command)
	command.Stdout = &output
	command.Stderr = &output
	runErr := command.Run()
	if output.Exceeded() {
		_ = terminateVersionCommand(command)
		return fmt.Errorf("binary version output exceeds %d bytes", maxVersionOutput)
	}
	if errors.Is(versionCtx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("binary version probe timed out after %s", timeout)
	}
	if runErr != nil {
		return fmt.Errorf("run binary version probe: %w", runErr)
	}

	actualVersion, err := binaryVersionFromOutput(output.String())
	if err != nil {
		return err
	}
	if actualVersion != expectedVersion {
		return fmt.Errorf("binary version %q does not match target %q", actualVersion, expectedVersion)
	}
	return nil
}

func binaryVersionFromOutput(output string) (string, error) {
	matches := binaryVersionPattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return "", fmt.Errorf("binary returned an unsupported version")
	}
	return strings.TrimPrefix(matches[1], "v"), nil
}

func moveExistingBackupAside(backupPath string) (string, error) {
	info, err := os.Lstat(backupPath)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect existing backup: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("existing backup is not a regular file")
	}

	temp, err := os.CreateTemp(filepath.Dir(backupPath), ".sub2api-backup-previous-*")
	if err != nil {
		return "", fmt.Errorf("reserve previous backup path: %w", err)
	}
	tempPath := temp.Name()
	if closeErr := temp.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return "", fmt.Errorf("close previous backup placeholder: %w", closeErr)
	}
	if err := os.Remove(tempPath); err != nil {
		return "", fmt.Errorf("remove previous backup placeholder: %w", err)
	}
	if err := os.Rename(backupPath, tempPath); err != nil {
		return "", fmt.Errorf("preserve existing backup: %w", err)
	}
	return tempPath, nil
}

func restorePreviousBackup(previousPath, backupPath string) error {
	if previousPath == "" {
		return nil
	}
	if _, err := os.Lstat(backupPath); err == nil {
		return fmt.Errorf("cannot restore previous backup because %q still exists; preserved at %q", backupPath, previousPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup restore target: %w", err)
	}
	if err := os.Rename(previousPath, backupPath); err != nil {
		return fmt.Errorf("restore previous backup from %q: %w", previousPath, err)
	}
	return nil
}

func replaceExecutable(exePath, newBinaryPath string) error {
	backupPath := exePath + ".backup"
	previousBackup, err := moveExistingBackupAside(backupPath)
	if err != nil {
		return err
	}

	if err := os.Rename(exePath, backupPath); err != nil {
		restoreErr := restorePreviousBackup(previousBackup, backupPath)
		if restoreErr != nil {
			return errors.Join(fmt.Errorf("backup current executable: %w", err), restoreErr)
		}
		return fmt.Errorf("backup current executable: %w", err)
	}

	if err := os.Rename(newBinaryPath, exePath); err != nil {
		restoreCurrentErr := os.Rename(backupPath, exePath)
		restoreBackupErr := restorePreviousBackup(previousBackup, backupPath)
		if restoreCurrentErr != nil || restoreBackupErr != nil {
			joined := []error{fmt.Errorf("replace executable: %w", err)}
			if restoreCurrentErr != nil {
				joined = append(joined, fmt.Errorf("restore current executable: %w", restoreCurrentErr))
			}
			if restoreBackupErr != nil {
				joined = append(joined, restoreBackupErr)
			}
			return errors.Join(joined...)
		}
		return fmt.Errorf("replace executable failed; original executable and backup restored: %w", err)
	}

	if previousBackup != "" {
		if err := os.Remove(previousBackup); err != nil {
			return fmt.Errorf("executable installed but previous backup cleanup failed at %q: %w", previousBackup, err)
		}
	}
	return nil
}

func validateExtractedBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("extracted binary is not a regular file")
	}
	if info.Size() <= 0 {
		return fmt.Errorf("extracted binary is empty")
	}
	return nil
}

func (s *UpdateService) getFromCache(ctx context.Context) (*UpdateInfo, error) {
	if err := validateCustomUpdateRepository(s.repository); err != nil {
		return nil, err
	}
	data, err := s.cache.GetUpdateInfo(ctx)
	if err != nil {
		return nil, err
	}

	var cached struct {
		Repository             string       `json:"repository"`
		Latest                 string       `json:"latest"`
		ReleaseInfo            *ReleaseInfo `json:"release_info"`
		OfficialLatest         string       `json:"official_latest"`
		OfficialReleaseInfo    *ReleaseInfo `json:"official_release_info"`
		OfficialReleaseWarning string       `json:"official_release_warning"`
		Timestamp              int64        `json:"timestamp"`
	}
	if err := json.Unmarshal([]byte(data), &cached); err != nil {
		return nil, err
	}
	if cached.Repository != s.repository {
		return nil, fmt.Errorf("cache repository mismatch")
	}
	if !isCustomBuildVersion(cached.Latest) {
		return nil, fmt.Errorf("cache contains invalid custom version %q", cached.Latest)
	}
	if cached.ReleaseInfo == nil {
		return nil, fmt.Errorf("cache missing custom release info")
	}
	if err := validateReleaseAssetMetadata("v"+cached.Latest, cached.ReleaseInfo.Assets, s.repository); err != nil {
		return nil, fmt.Errorf("cache contains invalid release assets: %w", err)
	}
	if cached.OfficialLatest == "" &&
		cached.OfficialReleaseWarning == "" {
		return nil, fmt.Errorf("cache missing official release info")
	}
	if time.Now().Unix()-cached.Timestamp > updateCacheTTL {
		return nil, fmt.Errorf("cache expired")
	}

	return &UpdateInfo{
		CurrentVersion:         baseUpdateVersion(s.currentVersion),
		CurrentBuildVersion:    s.currentVersion,
		LatestVersion:          cached.Latest,
		HasUpdate:              compareVersions(s.currentVersion, cached.Latest) < 0,
		ReleaseInfo:            cached.ReleaseInfo,
		Cached:                 true,
		BuildType:              s.buildType,
		UpdateRepository:       s.repository,
		OfficialRepository:     githubRepo,
		OfficialLatestVersion:  cached.OfficialLatest,
		HasOfficialUpdate:      cached.OfficialLatest != "" && compareVersions(baseUpdateVersion(s.currentVersion), cached.OfficialLatest) < 0,
		OfficialReleaseInfo:    cached.OfficialReleaseInfo,
		OfficialReleaseWarning: cached.OfficialReleaseWarning,
	}, nil
}

func (s *UpdateService) saveToCache(ctx context.Context, info *UpdateInfo) {
	cacheData := struct {
		Repository             string       `json:"repository"`
		Latest                 string       `json:"latest"`
		ReleaseInfo            *ReleaseInfo `json:"release_info"`
		OfficialLatest         string       `json:"official_latest"`
		OfficialReleaseInfo    *ReleaseInfo `json:"official_release_info"`
		OfficialReleaseWarning string       `json:"official_release_warning"`
		Timestamp              int64        `json:"timestamp"`
	}{
		Repository:             s.repository,
		Latest:                 info.LatestVersion,
		ReleaseInfo:            info.ReleaseInfo,
		OfficialLatest:         info.OfficialLatestVersion,
		OfficialReleaseInfo:    info.OfficialReleaseInfo,
		OfficialReleaseWarning: info.OfficialReleaseWarning,
		Timestamp:              time.Now().Unix(),
	}

	data, _ := json.Marshal(cacheData)
	_ = s.cache.SetUpdateInfo(ctx, string(data), time.Duration(updateCacheTTL)*time.Second)
}

// compareVersions compares two semantic versions
func compareVersions(current, latest string) int {
	currentVersion := normalizeUpdateVersion(current)
	latestVersion := normalizeUpdateVersion(latest)
	if !semver.IsValid(currentVersion) || !semver.IsValid(latestVersion) {
		return strings.Compare(currentVersion, latestVersion)
	}
	return semver.Compare(currentVersion, latestVersion)
}

func normalizeUpdateVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return "v0.0.0"
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return v
}

func baseUpdateVersion(v string) string {
	normalized := strings.TrimPrefix(normalizeUpdateVersion(v), "v")
	if separator := strings.IndexAny(normalized, "-+"); separator >= 0 {
		normalized = normalized[:separator]
	}
	return normalized
}

func githubAssetDownloadURL(asset GitHubAsset) string {
	return strings.TrimSpace(asset.APIURL)
}
