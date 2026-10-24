package runtimes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"dsh-mygo/internal/version"
)

func TestFetchAndInstallDownload(t *testing.T) {
	archivePath := writeArchive(t, validRuntimeEntries(t))
	archive, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(archive)
	sha := hex.EncodeToString(sum[:])
	manifest := validManifest()
	asset := "dsh-mygo-runtime-test.tgz"
	entry := CatalogEntry{
		ID: manifest.ID, DSH: manifest.DSH, Node: manifest.Node, Platform: manifest.Platform,
		Arch: manifest.Arch, ProtocolVersion: manifest.ProtocolVersion, HostVersion: manifest.HostVersion,
		AppMin: version.AppMin, AppMaxExclusive: version.AppMaxExclusive,
		Asset: asset, SHA256: sha, Size: int64(len(archive)),
	}
	catalog, _ := json.Marshal(Catalog{SchemaVersion: 1, Runtimes: []CatalogEntry{entry}})
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/owner/repo/releases", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `[{"tag_name":"runtime-test","draft":false,"assets":[{"name":"runtime-catalog.json","browser_download_url":"%s/catalog","size":%d},{"name":"%s","browser_download_url":"%s/archive","size":%d,"digest":"sha256:%s"}]}]`,
			serverURL(r), len(catalog), asset, serverURL(r), len(archive), sha)
	})
	mux.HandleFunc("/catalog", func(w http.ResponseWriter, r *http.Request) {
		w.Write(catalog)
	})
	mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) {
		w.Write(archive)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	manager := NewManager(t.TempDir(), t.TempDir(), "owner/repo")
	manager.APIBase = server.URL
	manager.HTTPClient = server.Client()
	manager.allowInsecure = true
	if err := manager.Load(); err != nil {
		t.Fatal(err)
	}
	release, err := manager.FetchCatalog(context.Background(), true)
	if err != nil {
		t.Fatalf("FetchCatalog: %v", err)
	}
	if len(release.Catalog.Runtimes) != 1 {
		t.Fatalf("catalog entries = %d", len(release.Catalog.Runtimes))
	}
	installed, err := manager.InstallDownload(context.Background(), release, release.Catalog.Runtimes[0], nil)
	if err != nil {
		t.Fatalf("InstallDownload: %v", err)
	}
	if _, err := ValidateInstalled(installed.Dir, CurrentExpected()); err != nil {
		t.Fatal(err)
	}
	entry.SHA256 = "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	_, err = manager.DownloadRuntime(context.Background(), release, entry, nil)
	if err == nil {
		t.Fatal("DownloadRuntime accepted a bad checksum")
	}
}

func TestFetchCatalogSkipsReleasesWithoutCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"tag_name":"empty","draft":false,"assets":[]}]`)
	}))
	defer server.Close()
	manager := NewManager(t.TempDir(), t.TempDir(), "owner/repo")
	manager.APIBase = server.URL
	manager.HTTPClient = server.Client()
	manager.allowInsecure = true
	if _, err := manager.FetchCatalog(context.Background(), true); err == nil {
		t.Fatal("FetchCatalog accepted a release without a catalog")
	}
}

func serverURL(r *http.Request) string {
	scheme := "http"
	return scheme + "://" + r.Host
}
