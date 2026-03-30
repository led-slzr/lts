package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"lts-revamp/internal/version"
)

const (
	releasesURL = "https://api.github.com/repos/led-slzr/lts/releases/latest"
	httpTimeout = 15 * time.Second
)

// Release holds the relevant fields from a GitHub release.
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset holds a single release asset.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Result is returned by Check and Update.
type Result struct {
	CurrentVersion string
	LatestVersion  string
	UpdateAvail    bool
	Updated        bool
	Err            error
}

// ShouldCheck returns true if a check is due (last check was >24h ago).
func ShouldCheck(lastCheckUnix int64) bool {
	if lastCheckUnix == 0 {
		return true
	}
	return time.Since(time.Unix(lastCheckUnix, 0)) > 24*time.Hour
}

// Check queries GitHub for the latest release and reports whether an update is available.
func Check() Result {
	r := Result{CurrentVersion: version.Version}

	rel, err := fetchLatestRelease()
	if err != nil {
		r.Err = err
		return r
	}

	r.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
	r.UpdateAvail = isNewer(r.LatestVersion, r.CurrentVersion)
	return r
}

// Update checks for a new release and, if available, downloads and replaces the current binary.
func Update() Result {
	r := Result{CurrentVersion: version.Version}

	rel, err := fetchLatestRelease()
	if err != nil {
		r.Err = err
		return r
	}

	r.LatestVersion = strings.TrimPrefix(rel.TagName, "v")
	r.UpdateAvail = isNewer(r.LatestVersion, r.CurrentVersion)
	if !r.UpdateAvail {
		return r
	}

	assetName := expectedAssetName()
	var downloadURL string
	for _, a := range rel.Assets {
		if a.Name == assetName {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		r.Err = fmt.Errorf("no matching asset %s in release %s", assetName, rel.TagName)
		return r
	}

	if err := downloadAndReplace(downloadURL); err != nil {
		r.Err = fmt.Errorf("update failed: %w", err)
		return r
	}

	r.Updated = true
	return r
}

func fetchLatestRelease() (*Release, error) {
	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Get(releasesURL)
	if err != nil {
		return nil, fmt.Errorf("fetch release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var rel Release
	// Limit response body to 1MB to prevent memory exhaustion
	limited := io.LimitReader(resp.Body, 1<<20)
	if err := json.NewDecoder(limited).Decode(&rel); err != nil {
		return nil, fmt.Errorf("decode release: %w", err)
	}
	return &rel, nil
}

// expectedAssetName returns the archive name matching the current OS/arch.
// Must match goreleaser's name_template: lts_{{ .Os }}_{{ .Arch }}.tar.gz
func expectedAssetName() string {
	return fmt.Sprintf("lts_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
}

// downloadAndReplace fetches the tar.gz, extracts the "lts" binary, and replaces the running executable.
func downloadAndReplace(url string) error {
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("binary not found in archive")
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if filepath.Base(hdr.Name) == "lts" && hdr.Typeflag == tar.TypeReg {
			return replaceBinary(tr)
		}
	}
}

// replaceBinary atomically replaces the current executable with the contents of r.
func replaceBinary(r io.Reader) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	// Write to a temp file next to the executable
	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, "lts-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpPath, exe); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

// isNewer returns true if latest is a higher semver than current.
// Expects versions without "v" prefix, e.g. "2.4.0".
func isNewer(latest, current string) bool {
	lp := parseSemver(latest)
	cp := parseSemver(current)
	for i := 0; i < 3; i++ {
		if lp[i] > cp[i] {
			return true
		}
		if lp[i] < cp[i] {
			return false
		}
	}
	return false
}

func parseSemver(v string) [3]int {
	v = strings.TrimPrefix(v, "v")
	parts := strings.SplitN(v, ".", 3)
	var result [3]int
	for i, p := range parts {
		if i >= 3 {
			break
		}
		fmt.Sscanf(p, "%d", &result[i])
	}
	return result
}
