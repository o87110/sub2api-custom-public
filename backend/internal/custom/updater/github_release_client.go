package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var errVersionInfoClientReadOnly = errors.New("version information client does not support release asset downloads")

type githubReleaseClient struct {
	httpClient         *http.Client
	downloadHTTPClient *http.Client
	updateGitHubToken  string
}

type githubReleaseClientError struct {
	err error
}

type githubReleaseClientInitialization interface {
	InitializationError() error
}

// versionInfoClientAdapter lets informational upstream-version checks reuse the
// hardened Custom GitHub client without exposing its download capabilities.
type versionInfoClientAdapter struct {
	client GitHubReleaseClient
}

func newVersionInfoClientAdapter(client GitHubReleaseClient) service.GitHubReleaseClient {
	return &versionInfoClientAdapter{client: client}
}

func (c *versionInfoClientAdapter) FetchLatestRelease(ctx context.Context, repo string) (*service.GitHubRelease, error) {
	release, err := c.client.FetchLatestRelease(ctx, repo)
	if err != nil {
		return nil, err
	}
	return toServiceGitHubRelease(release), nil
}

func (c *versionInfoClientAdapter) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*service.GitHubRelease, error) {
	releases, err := c.client.FetchRecentReleases(ctx, repo, perPage)
	if err != nil {
		return nil, err
	}
	converted := make([]*service.GitHubRelease, 0, len(releases))
	for _, release := range releases {
		converted = append(converted, toServiceGitHubRelease(release))
	}
	return converted, nil
}

func (c *versionInfoClientAdapter) DownloadFile(context.Context, string, string, int64) error {
	return errVersionInfoClientReadOnly
}

func (c *versionInfoClientAdapter) FetchChecksumFile(context.Context, string) ([]byte, error) {
	return nil, errVersionInfoClientReadOnly
}

func toServiceGitHubRelease(release *GitHubRelease) *service.GitHubRelease {
	if release == nil {
		return nil
	}
	assets := make([]service.GitHubAsset, 0, len(release.Assets))
	for _, asset := range release.Assets {
		assets = append(assets, service.GitHubAsset{
			Name:               asset.Name,
			BrowserDownloadURL: asset.BrowserDownloadURL,
			Size:               asset.Size,
		})
	}
	return &service.GitHubRelease{
		TagName:     release.TagName,
		Name:        release.Name,
		Body:        release.Body,
		PublishedAt: release.PublishedAt,
		HTMLURL:     release.HTMLURL,
		Draft:       release.Draft,
		Prerelease:  release.Prerelease,
		Assets:      assets,
	}
}

// NewGitHubReleaseClient 创建 GitHub Release 客户端
// proxyURL 为空时直连 GitHub，支持 http/https/socks5/socks5h 协议
// 代理配置失败时行为由 allowDirectOnProxyError 控制：
//   - false（默认）：返回错误占位客户端，禁止回退到直连
//   - true：回退到直连（仅限管理员显式开启）
func NewGitHubReleaseClient(proxyURL string, allowDirectOnProxyError bool) GitHubReleaseClient {
	return NewGitHubReleaseClientWithAuth(
		proxyURL,
		allowDirectOnProxyError,
		os.Getenv("UPDATE_GITHUB_TOKEN"),
		"",
	)
}

// NewGitHubReleaseClientWithAuth 创建支持可选 GitHub API 鉴权的 Release 客户端。
// 公开仓库无需 Token；配置 tokenFile 时必须成功读取，且优先于 token。
func NewGitHubReleaseClientWithAuth(proxyURL string, allowDirectOnProxyError bool, token, tokenFile string) GitHubReleaseClient {
	resolvedToken := strings.TrimSpace(token)
	if tokenPath := strings.TrimSpace(tokenFile); tokenPath != "" {
		// #nosec G703 -- tokenPath is an operator-controlled local secret path from UPDATE_GITHUB_TOKEN_FILE.
		raw, err := os.ReadFile(tokenPath)
		if err != nil {
			return &githubReleaseClientError{err: fmt.Errorf("read GitHub release token file: %w", err)}
		}
		resolvedToken = strings.TrimSpace(string(raw))
		if resolvedToken == "" {
			return &githubReleaseClientError{err: fmt.Errorf("GitHub release token file is empty")}
		}
	}

	sharedClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  30 * time.Second,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		sharedClient = &http.Client{Timeout: 30 * time.Second}
	}
	downloadClient, err := httpclient.GetClient(httpclient.Options{
		Timeout:  10 * time.Minute,
		ProxyURL: proxyURL,
	})
	if err != nil {
		if strings.TrimSpace(proxyURL) != "" && !allowDirectOnProxyError {
			slog.Warn("proxy download client init failed, all requests will fail", "service", "github_release", "error", err)
			return &githubReleaseClientError{err: fmt.Errorf("proxy client init failed and direct fallback is disabled; set security.proxy_fallback.allow_direct_on_error=true to allow fallback: %w", err)}
		}
		downloadClient = &http.Client{Timeout: 10 * time.Minute}
	}

	apiClient := hardenGitHubRedirects(sharedClient)
	downloadClient = hardenGitHubRedirects(downloadClient)
	return &githubReleaseClient{
		httpClient:         apiClient,
		downloadHTTPClient: downloadClient,
		updateGitHubToken:  resolvedToken,
	}
}

func cloneHTTPClient(client *http.Client) *http.Client {
	cloned := *client
	return &cloned
}

func isGitHubAPIURL(url *url.URL) bool {
	return url != nil && strings.EqualFold(url.Scheme, "https") && url.User == nil &&
		strings.EqualFold(url.Host, "api.github.com")
}

func githubAPICheckRedirect(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if !isGitHubAPIURL(req.URL) {
			req.Header.Del("Authorization")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
}

func hardenGitHubRedirects(client *http.Client) *http.Client {
	if client == nil {
		return nil
	}

	// httpclient.GetClient returns pooled clients. Clone the client before
	// installing updater-specific redirect rules so unrelated HTTP users keep
	// their own redirect behavior while still sharing the transport safely.
	hardened := cloneHTTPClient(client)
	checkAPIAuth := githubAPICheckRedirect(hardened.CheckRedirect)
	hardened.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if !strings.EqualFold(req.URL.Scheme, "https") {
			return fmt.Errorf("refusing non-HTTPS GitHub redirect")
		}

		host := strings.ToLower(req.URL.Hostname())
		switch host {
		case "api.github.com", "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		default:
			return fmt.Errorf("refusing GitHub redirect to untrusted host %q", host)
		}
		return checkAPIAuth(req, via)
	}
	return hardened
}

func newGitHubRequest(ctx context.Context, rawURL, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	req.Header.Set("User-Agent", "Sub2API-Updater")
	return req, nil
}

func (c *githubReleaseClient) newRequest(ctx context.Context, rawURL, accept string) (*http.Request, error) {
	req, err := newGitHubRequest(ctx, rawURL, accept)
	if err != nil {
		return nil, err
	}
	if c != nil && c.updateGitHubToken != "" && isGitHubAPIURL(req.URL) {
		req.Header.Set("Authorization", "Bearer "+c.updateGitHubToken)
	}
	return req, nil
}

func (c *githubReleaseClientError) FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	return c.err
}

func (c *githubReleaseClientError) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	return nil, c.err
}

func (c *githubReleaseClientError) InitializationError() error {
	return c.err
}

func (c *githubReleaseClient) InitializationError() error {
	return nil
}

func (c *githubReleaseClient) FetchLatestRelease(ctx context.Context, repo string) (*GitHubRelease, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	req, err := c.newRequest(ctx, url, "application/vnd.github.v3+json")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func (c *githubReleaseClient) FetchRecentReleases(ctx context.Context, repo string, perPage int) ([]*GitHubRelease, error) {
	if perPage <= 0 {
		perPage = 10
	}
	if perPage > 100 {
		perPage = 100 // GitHub API hard limit
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=%d", repo, perPage)

	req, err := c.newRequest(ctx, url, "application/vnd.github.v3+json")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var releases []*GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}

	return releases, nil
}

func (c *githubReleaseClient) DownloadFile(ctx context.Context, url, dest string, maxSize int64) error {
	req, err := c.newRequest(ctx, url, "application/octet-stream")
	if err != nil {
		return err
	}

	// 使用预配置的下载客户端（已包含代理配置）
	resp, err := c.downloadHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// SECURITY: Check Content-Length if available
	if resp.ContentLength > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxSize)
	}

	out, err := os.Create(dest)
	if err != nil {
		return err
	}

	// SECURITY: Use LimitReader to enforce max download size even if Content-Length is missing/wrong
	limited := io.LimitReader(resp.Body, maxSize+1)
	written, err := io.Copy(out, limited)

	// Close file before attempting to remove (required on Windows)
	closeErr := out.Close()

	if err != nil {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(dest)
		return fmt.Errorf("close downloaded file: %w", closeErr)
	}

	// Check if we hit the limit (downloaded more than maxSize)
	if written > maxSize {
		_ = os.Remove(dest) // Clean up partial file (best-effort)
		return fmt.Errorf("download exceeded maximum size of %d bytes", maxSize)
	}

	return nil
}

func (c *githubReleaseClient) FetchChecksumFile(ctx context.Context, url string) ([]byte, error) {
	req, err := c.newRequest(ctx, url, "application/octet-stream")
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, maxChecksumFileSize+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxChecksumFileSize {
		return nil, fmt.Errorf("checksum file exceeds maximum size of %d bytes", maxChecksumFileSize)
	}
	return data, nil
}
