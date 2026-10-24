package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dsh-mygo/internal/version"
)

const catalogAssetName = "runtime-catalog.json"
const maxCatalogResponseBytes = 4 << 20
const catalogCacheTTL = 15 * time.Minute

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var githubHosts = map[string]bool{
	"api.github.com":                       true,
	"github.com":                           true,
	"objects.githubusercontent.com":        true,
	"release-assets.githubusercontent.com": true,
}

// ReleaseAsset is a downloadable GitHub release asset.
type ReleaseAsset struct {
	Name   string
	URL    string
	Size   int64
	Digest string
}

// CatalogRelease is one release that carries a compatible runtime catalog.
type CatalogRelease struct {
	Tag     string
	Catalog Catalog
	Assets  map[string]ReleaseAsset
}

type releaseResponse struct {
	TagName string `json:"tag_name"`
	Draft   bool   `json:"draft"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Size               int64  `json:"size"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

// FetchCatalog finds the newest non-draft release with runtime-catalog.json.
func (m *Manager) FetchCatalog(ctx context.Context, force bool) (CatalogRelease, error) {
	m.mu.Lock()
	if !force && m.catalog.release.Tag != "" && time.Since(m.catalog.at) < catalogCacheTTL {
		cached := m.catalog.release
		m.mu.Unlock()
		return cached, nil
	}
	m.mu.Unlock()
	if !repositoryPattern.MatchString(m.Repository) {
		return CatalogRelease{}, fmt.Errorf("runtime repository is not configured")
	}
	endpoint := strings.TrimRight(m.APIBase, "/") + "/repos/" + m.Repository + "/releases?per_page=20"
	var releases []releaseResponse
	if err := m.getJSON(ctx, endpoint, &releases); err != nil {
		return CatalogRelease{}, err
	}
	for _, release := range releases {
		if release.Draft {
			continue
		}
		assets := make(map[string]ReleaseAsset, len(release.Assets))
		var catalogAsset *ReleaseAsset
		for _, asset := range release.Assets {
			item := ReleaseAsset{Name: asset.Name, URL: asset.BrowserDownloadURL, Size: asset.Size, Digest: asset.Digest}
			assets[asset.Name] = item
			if asset.Name == catalogAssetName {
				copy := item
				catalogAsset = &copy
			}
		}
		if catalogAsset == nil {
			continue
		}
		data, err := m.getBytes(ctx, catalogAsset.URL, maxCatalogResponseBytes)
		if err != nil {
			return CatalogRelease{}, fmt.Errorf("could not read the runtime catalog")
		}
		catalog, err := ParseCatalog(data, CurrentExpected())
		if err != nil {
			return CatalogRelease{}, err
		}
		result := CatalogRelease{Tag: release.TagName, Catalog: catalog, Assets: assets}
		m.mu.Lock()
		m.catalog = catalogCache{release: result, at: time.Now()}
		m.mu.Unlock()
		return result, nil
	}
	return CatalogRelease{}, fmt.Errorf("no runtime release with %s was found", catalogAssetName)
}

// DownloadRuntime downloads one catalog entry into the runtime store.
func (m *Manager) DownloadRuntime(ctx context.Context, release CatalogRelease, entry CatalogEntry, progress func(downloaded, total int64)) (string, error) {
	asset, ok := release.Assets[entry.Asset]
	if !ok {
		return "", fmt.Errorf("runtime asset is missing from the release")
	}
	if asset.Size > 0 && asset.Size != entry.Size {
		return "", fmt.Errorf("runtime asset size does not match the catalog")
	}
	if asset.Digest != "" && strings.TrimPrefix(asset.Digest, "sha256:") != entry.SHA256 {
		return "", fmt.Errorf("runtime asset digest does not match the catalog")
	}
	directory := filepath.Join(m.Root, "downloads")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("create runtime download directory: %w", err)
	}
	temp, err := os.CreateTemp(directory, ".download-*.part")
	if err != nil {
		return "", fmt.Errorf("create runtime download: %w", err)
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		temp.Close()
		return "", fmt.Errorf("create runtime download request: %w", err)
	}
	request.Header.Set("User-Agent", "dsh-mygo/"+versionLabel())
	response, err := m.httpClient().Do(request)
	if err != nil {
		temp.Close()
		return "", fmt.Errorf("download runtime: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		temp.Close()
		return "", fmt.Errorf("download runtime: HTTP %d", response.StatusCode)
	}
	if err := m.validateURL(response.Request.URL.String()); err != nil {
		temp.Close()
		return "", err
	}
	hash := sha256.New()
	var downloaded int64
	lastProgress := time.Time{}
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			temp.Close()
			return "", err
		}
		count, readErr := response.Body.Read(buffer)
		if count > 0 {
			downloaded += int64(count)
			if downloaded > entry.Size || downloaded > maxArchiveBytes {
				temp.Close()
				return "", fmt.Errorf("runtime download is larger than declared")
			}
			if _, err := temp.Write(buffer[:count]); err != nil {
				temp.Close()
				return "", fmt.Errorf("write runtime download: %w", err)
			}
			if _, err := hash.Write(buffer[:count]); err != nil {
				temp.Close()
				return "", err
			}
			if progress != nil && time.Since(lastProgress) >= 100*time.Millisecond {
				lastProgress = time.Now()
				progress(downloaded, entry.Size)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			temp.Close()
			return "", fmt.Errorf("read runtime download: %w", readErr)
		}
	}
	if err := temp.Close(); err != nil {
		return "", fmt.Errorf("close runtime download: %w", err)
	}
	if downloaded != entry.Size {
		return "", fmt.Errorf("runtime download size did not match the catalog")
	}
	if actual := hex.EncodeToString(hash.Sum(nil)); actual != entry.SHA256 {
		return "", fmt.Errorf("runtime download checksum did not match the catalog")
	}
	final := filepath.Join(directory, entry.Asset)
	_ = os.Remove(final)
	if err := os.Rename(tempName, final); err != nil {
		return "", fmt.Errorf("finish runtime download: %w", err)
	}
	if progress != nil {
		progress(downloaded, entry.Size)
	}
	return final, nil
}

// InstallDownload downloads, verifies and installs one catalog runtime.
func (m *Manager) InstallDownload(ctx context.Context, release CatalogRelease, entry CatalogEntry, progress func(string, float32)) (Runtime, error) {
	report := func(stage string, ratio float32) {
		if progress != nil {
			progress(stage, ratio)
		}
	}
	archive, err := m.DownloadRuntime(ctx, release, entry, func(downloaded, total int64) {
		ratio := float32(0)
		if total > 0 {
			ratio = float32(downloaded) / float32(total)
		}
		report("Downloading runtime", ratio)
	})
	if err != nil {
		return Runtime{}, err
	}
	defer os.Remove(archive)
	report("Installing runtime", 0)
	return m.InstallArchive(ctx, archive, func(ratio float32) {
		report("Installing runtime", ratio)
	})
}

func (m *Manager) getJSON(ctx context.Context, endpoint string, target any) error {
	data, err := m.getBytes(ctx, endpoint, maxCatalogResponseBytes)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("invalid runtime catalog response")
	}
	return nil
}

func (m *Manager) getBytes(ctx context.Context, endpoint string, limit int64) ([]byte, error) {
	if err := m.validateURL(endpoint); err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create runtime request: %w", err)
	}
	request.Header.Set("User-Agent", "dsh-mygo/"+versionLabel())
	response, err := m.httpClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch runtime catalog: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch runtime catalog: HTTP %d", response.StatusCode)
	}
	if err := m.validateURL(response.Request.URL.String()); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read runtime catalog: %w", err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("runtime catalog response is too large")
	}
	return data, nil
}

func (m *Manager) httpClient() *http.Client {
	if m.HTTPClient != nil {
		return m.HTTPClient
	}
	return http.DefaultClient
}

func (m *Manager) validateURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return fmt.Errorf("invalid runtime download URL")
	}
	if parsed.Scheme == "https" && githubHosts[parsed.Hostname()] {
		return nil
	}
	if m.allowInsecure && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost") {
		return nil
	}
	return fmt.Errorf("runtime download URL is not from the allowed release host")
}

func versionLabel() string {
	return version.App
}
