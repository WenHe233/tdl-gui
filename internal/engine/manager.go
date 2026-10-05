package engine

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
)

const BaselineVersion = "v0.20.4"
const releasesURL = "https://api.github.com/repos/iyear/tdl/releases"

type Settings interface {
	SetSetting(context.Context, string, string) error
	GetSetting(context.Context, string) (string, error)
}
type Manager struct {
	dir      string
	settings Settings
	client   *http.Client
}

func New(dir string, settings Settings) *Manager {
	return &Manager{dir: dir, settings: settings, client: &http.Client{Timeout: 10 * time.Minute}}
}

type release struct {
	TagName    string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}
type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

func (m *Manager) Latest(ctx context.Context) (string, error) {
	r, err := m.fetch(ctx, releasesURL+"/latest")
	if err != nil {
		return "", err
	}
	return r.TagName, nil
}
func (m *Manager) Installed(ctx context.Context) ([]domain.EngineVersion, error) {
	active, _ := m.settings.GetSetting(ctx, "engine.version")
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, err
	}
	var out []domain.EngineVersion
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := filepath.Join(m.dir, e.Name(), exeName())
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			out = append(out, domain.EngineVersion{Version: e.Name(), Path: p, InstalledAt: st.ModTime(), Active: e.Name() == active})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}
func (m *Manager) Active(ctx context.Context) (domain.EngineVersion, error) {
	if custom, err := m.settings.GetSetting(ctx, "engine.path"); err == nil && custom != "" {
		st, e := os.Stat(custom)
		if e != nil {
			return domain.EngineVersion{}, fmt.Errorf("configured engine: %w", e)
		}
		return domain.EngineVersion{Version: "custom", Path: custom, InstalledAt: st.ModTime(), Active: true}, nil
	}
	v, err := m.settings.GetSetting(ctx, "engine.version")
	if err != nil || v == "" {
		return domain.EngineVersion{}, errors.New("tdl engine is not installed; run `tdl-media engine install`")
	}
	p := filepath.Join(m.dir, v, exeName())
	st, err := os.Stat(p)
	if err != nil {
		return domain.EngineVersion{}, fmt.Errorf("active engine %s: %w", v, err)
	}
	return domain.EngineVersion{Version: v, Path: p, InstalledAt: st.ModTime(), Active: true}, nil
}
func (m *Manager) Use(ctx context.Context, versionOrPath string) error {
	if filepath.IsAbs(versionOrPath) || strings.ContainsAny(versionOrPath, `/\`) {
		p, err := filepath.Abs(versionOrPath)
		if err != nil {
			return err
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("engine executable not found: %s", p)
		}
		if err := m.settings.SetSetting(ctx, "engine.path", p); err != nil {
			return err
		}
		return m.settings.SetSetting(ctx, "engine.version", "")
	}
	p := filepath.Join(m.dir, versionOrPath, exeName())
	if _, err := os.Stat(p); err != nil {
		return fmt.Errorf("engine version %s is not installed", versionOrPath)
	}
	if err := m.settings.SetSetting(ctx, "engine.path", ""); err != nil {
		return err
	}
	return m.settings.SetSetting(ctx, "engine.version", versionOrPath)
}
func (m *Manager) Install(ctx context.Context, version string) (domain.EngineVersion, error) {
	url := releasesURL + "/latest"
	if version != "" && version != "latest" {
		if !strings.HasPrefix(version, "v") {
			version = "v" + version
		}
		url = releasesURL + "/tags/" + version
	}
	r, err := m.fetch(ctx, url)
	if err != nil {
		return domain.EngineVersion{}, err
	}
	download, checksum := selectAssets(r.Assets)
	if download.BrowserDownloadURL == "" {
		return domain.EngineVersion{}, fmt.Errorf("release %s has no Windows x64 asset", r.TagName)
	}
	tmpDir, err := os.MkdirTemp(m.dir, "install-")
	if err != nil {
		return domain.EngineVersion{}, err
	}
	defer os.RemoveAll(tmpDir)
	pkg := filepath.Join(tmpDir, download.Name)
	if err = m.download(ctx, download.BrowserDownloadURL, pkg); err != nil {
		return domain.EngineVersion{}, err
	}
	if checksum.BrowserDownloadURL == "" {
		return domain.EngineVersion{}, fmt.Errorf("发布缺少 SHA256 校验文件")
	}
	checks := filepath.Join(tmpDir, checksum.Name)
	if err = m.download(ctx, checksum.BrowserDownloadURL, checks); err != nil {
		return domain.EngineVersion{}, fmt.Errorf("下载校验文件: %w", err)
	}
	want, err := checksumFor(checks, download.Name)
	if err != nil || len(want) != 64 {
		return domain.EngineVersion{}, fmt.Errorf("发布校验文件缺少有效的 %s SHA256", download.Name)
	}
	if _, err = hex.DecodeString(want); err != nil {
		return domain.EngineVersion{}, fmt.Errorf("无效 SHA256: %w", err)
	}
	got, err := fileSHA(pkg)
	if err != nil {
		return domain.EngineVersion{}, err
	}
	if want != "" && !strings.EqualFold(want, got) {
		return domain.EngineVersion{}, fmt.Errorf("checksum mismatch for %s", download.Name)
	}
	extracted := filepath.Join(tmpDir, exeName())
	if strings.HasSuffix(strings.ToLower(pkg), ".zip") {
		if err = extractExecutable(pkg, extracted); err != nil {
			return domain.EngineVersion{}, err
		}
	} else {
		if err = copyFile(pkg, extracted); err != nil {
			return domain.EngineVersion{}, err
		}
	}
	destDir := filepath.Join(m.dir, r.TagName)
	if err = os.MkdirAll(destDir, 0o700); err != nil {
		return domain.EngineVersion{}, err
	}
	dest := filepath.Join(destDir, exeName())
	next := dest + ".new"
	if err = copyFile(extracted, next); err != nil {
		return domain.EngineVersion{}, err
	}
	if err = os.Rename(next, dest); err != nil {
		return domain.EngineVersion{}, err
	}
	if err = m.Use(ctx, r.TagName); err != nil {
		return domain.EngineVersion{}, err
	}
	st, _ := os.Stat(dest)
	return domain.EngineVersion{Version: r.TagName, Path: dest, SHA256: got, InstalledAt: st.ModTime(), Active: true}, nil
}
func (m *Manager) Rollback(ctx context.Context) error {
	versions, err := m.Installed(ctx)
	if err != nil {
		return err
	}
	for _, v := range versions {
		if !v.Active {
			return m.Use(ctx, v.Version)
		}
	}
	return errors.New("no previous engine version is installed")
}

func (m *Manager) fetch(ctx context.Context, url string) (release, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "tdl-media")
	res, err := m.client.Do(req)
	if err != nil {
		return release{}, err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return release{}, fmt.Errorf("GitHub release API returned %s", res.Status)
	}
	var r release
	err = json.NewDecoder(res.Body).Decode(&r)
	return r, err
}
func (m *Manager) download(ctx context.Context, url, path string) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "tdl-media")
	res, err := m.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("download returned %s", res.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(f, res.Body)
	closeErr := f.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}
func selectAssets(v []asset) (asset, asset) {
	var pkg, checks asset
	archNames := []string{runtime.GOARCH}
	if runtime.GOARCH == "amd64" {
		archNames = append(archNames, "x86_64", "64bit", "64-bit")
	}
	if runtime.GOARCH == "arm64" {
		archNames = append(archNames, "aarch64", "arm64")
	}
	for _, a := range v {
		n := strings.ToLower(a.Name)
		if strings.Contains(n, "checksum") || strings.Contains(n, "sha256") {
			checks = a
			continue
		}
		archMatch := false
		for _, candidate := range archNames {
			if strings.Contains(n, candidate) {
				archMatch = true
				break
			}
		}
		if runtime.GOOS == "windows" && strings.Contains(n, "windows") && archMatch && (strings.HasSuffix(n, ".zip") || strings.HasSuffix(n, ".exe")) {
			pkg = a
		}
	}
	return pkg, checks
}
func checksumFor(path, name string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[len(fields)-1], "*") == name {
			return fields[0], nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found", name)
}
func fileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func extractExecutable(path, dest string) error {
	z, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer z.Close()
	for _, f := range z.File {
		if strings.EqualFold(filepath.Base(f.Name), exeName()) {
			r, err := f.Open()
			if err != nil {
				return err
			}
			defer r.Close()
			o, err := os.Create(dest)
			if err != nil {
				return err
			}
			_, cpErr := io.Copy(o, r)
			closeErr := o.Close()
			if cpErr != nil {
				return cpErr
			}
			return closeErr
		}
	}
	return errors.New("archive does not contain tdl executable")
}
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	_, cpErr := io.Copy(out, in)
	closeErr := out.Close()
	if cpErr != nil {
		return cpErr
	}
	return closeErr
}
func exeName() string {
	if runtime.GOOS == "windows" {
		return "tdl.exe"
	}
	return "tdl"
}
