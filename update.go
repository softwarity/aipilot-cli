package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type semver struct {
	Major int
	Minor int
	Patch int
}

func parseSemver(v string) (semver, error) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return semver{}, fmt.Errorf("invalid version: %s", v)
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return semver{}, err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return semver{}, err
	}
	patch, err := strconv.Atoi(parts[2])
	if err != nil {
		return semver{}, err
	}
	return semver{major, minor, patch}, nil
}

func (s semver) String() string {
	return fmt.Sprintf("v%d.%d.%d", s.Major, s.Minor, s.Patch)
}

// updateType returns "major", "minor", "patch", or "" if no update needed
func (s semver) updateType(latest semver) string {
	if latest.Major > s.Major {
		return "major"
	}
	if latest.Major < s.Major {
		return ""
	}
	if latest.Minor > s.Minor {
		return "minor"
	}
	if latest.Minor < s.Minor {
		return ""
	}
	if latest.Patch > s.Patch {
		return "patch"
	}
	return ""
}

func getAssetSuffix() string {
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	if goos == "darwin" {
		goos = "macos"
	}

	suffix := goos + "-" + goarch
	if runtime.GOOS == "windows" {
		suffix += ".exe"
	}
	return suffix
}

func checkLatestVersion() (*githubRelease, error) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/softwarity/aipilot-cli/releases/latest")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

func findDownloadURL(release *githubRelease) string {
	suffix := getAssetSuffix()
	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func getExecutablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func downloadAndReplace(downloadURL, exePath string) error {
	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	// Write to temp file in same directory (ensures same filesystem for rename)
	dir := filepath.Dir(exePath)
	tmpFile, err := os.CreateTemp(dir, ".aipilot-cli-update-*")
	if err != nil {
		return fmt.Errorf("cannot create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("download interrupted: %w", err)
	}
	tmpFile.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Replace the binary
	if runtime.GOOS == "windows" {
		// Windows: can't delete running binary, but can rename it
		oldPath := exePath + ".old"
		os.Remove(oldPath)
		if err := os.Rename(exePath, oldPath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("cannot rename current binary: %w", err)
		}
		if err := os.Rename(tmpPath, exePath); err != nil {
			os.Rename(oldPath, exePath) // restore
			os.Remove(tmpPath)
			return fmt.Errorf("cannot install new binary: %w", err)
		}
	} else {
		// Unix: can replace running binary directly
		if err := os.Rename(tmpPath, exePath); err != nil {
			os.Remove(tmpPath)
			return fmt.Errorf("cannot replace binary: %w", err)
		}
	}

	return nil
}

// cleanupOldBinary removes leftover .old file from Windows update
func cleanupOldBinary() {
	if runtime.GOOS != "windows" {
		return
	}
	if exe, err := getExecutablePath(); err == nil {
		os.Remove(exe + ".old")
	}
}

// checkUpdateOnStartup checks for updates at startup.
// Patch: download in background, applied on next launch.
// Minor/Major: blocking download + restart.
func checkUpdateOnStartup() {
	current, err := parseSemver(Version)
	if err != nil {
		return // dev build, skip
	}

	fmt.Printf("%sChecking for updates...%s\r", dim, reset)

	release, err := checkLatestVersion()
	if err != nil {
		fmt.Printf("                       \r") // clear line
		return
	}

	latest, err := parseSemver(release.TagName)
	if err != nil {
		fmt.Printf("                       \r")
		return
	}

	updateType := current.updateType(latest)
	if updateType == "" {
		fmt.Printf("                       \r")
		return
	}

	downloadURL := findDownloadURL(release)
	if downloadURL == "" {
		return
	}

	exePath, err := getExecutablePath()
	if err != nil {
		return
	}

	if updateType == "patch" {
		// Non-blocking: download in background, applied on next launch
		fmt.Printf("%s⬆ %s available, downloading in background...%s\n", dim, latest.String(), reset)
		go func() {
			downloadAndReplace(downloadURL, exePath)
		}()
	} else {
		// Blocking: minor/major update
		fmt.Printf("%s⬆ Update %s → %s available%s\n", cyan, current.String(), latest.String(), reset)
		fmt.Printf("%s  Updating...%s\n", cyan, reset)
		if err := downloadAndReplace(downloadURL, exePath); err != nil {
			fmt.Printf("%s  Update failed: %v%s\n", yellow, err, reset)
			return
		}
		fmt.Printf("%s  ✓ Updated to %s. Restarting...%s\n", green, latest.String(), reset)
		restartSelf(exePath)
	}
}

// forceUpdate performs a blocking update check and install (--update flag)
func forceUpdate() {
	current, err := parseSemver(Version)
	if err != nil {
		fmt.Printf("%sCannot check updates: invalid version %q%s\n", yellow, Version, reset)
		return
	}

	fmt.Printf("Current version: %s\n", current.String())
	fmt.Printf("Checking for updates...\n")

	release, err := checkLatestVersion()
	if err != nil {
		fmt.Printf("%sFailed to check: %v%s\n", red, err, reset)
		return
	}

	latest, err := parseSemver(release.TagName)
	if err != nil {
		fmt.Printf("%sInvalid remote version: %s%s\n", red, release.TagName, reset)
		return
	}

	updateType := current.updateType(latest)
	if updateType == "" {
		fmt.Printf("%s✓ Already up to date (%s)%s\n", green, current.String(), reset)
		return
	}

	downloadURL := findDownloadURL(release)
	if downloadURL == "" {
		fmt.Printf("%sNo binary for %s/%s%s\n", yellow, runtime.GOOS, runtime.GOARCH, reset)
		return
	}

	exePath, err := getExecutablePath()
	if err != nil {
		fmt.Printf("%sCannot determine executable path: %v%s\n", red, err, reset)
		return
	}

	fmt.Printf("Updating %s → %s...\n", current.String(), latest.String())
	if err := downloadAndReplace(downloadURL, exePath); err != nil {
		fmt.Printf("%sFailed to update: %v%s\n", red, err, reset)
		return
	}
	fmt.Printf("%s✓ Updated to %s%s\n", green, latest.String(), reset)
}
