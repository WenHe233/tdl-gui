// Package updates installs the paired portable GUI/worker release, preserving user data.
package updates

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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repository = "https://github.com/WenHe233/tdl-gui"
const latestURL = "https://api.github.com/repos/WenHe233/tdl-gui/releases/latest"
const maxPackageBytes = int64(256 << 20)

var versionPattern = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)
var packageFiles = []string{"tdl-media.exe", "tdl-media-gui.exe", "README.md", "LICENSE"}

type Info struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`
	PackageURL  string `json:"-"`
	ChecksumURL string `json:"-"`
	FileName    string `json:"-"`
}
type Result struct {
	Version string `json:"version"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}
type manifest struct {
	InstallDir    string            `json:"installDir"`
	StageDir      string            `json:"stageDir"`
	Version       string            `json:"version"`
	GUIProcess    int               `json:"guiProcess"`
	WorkerProcess int               `json:"workerProcess"`
	Hashes        map[string]string `json:"hashes"`
}
type Service struct {
	Current    string
	InstallDir string
	Client     *http.Client
	mu         sync.Mutex
	prepared   *manifest
}

func New(current, installDir string) *Service {
	return &Service{Current: current, InstallDir: installDir, Client: &http.Client{Timeout: 10 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) > 10 {
			return errors.New("更新下载重定向无效")
		}
		return nil
	}}}
}
func newer(candidate, current string) (bool, error) {
	a, b := versionPattern.FindStringSubmatch(candidate), versionPattern.FindStringSubmatch(current)
	if a == nil || b == nil {
		return false, errors.New("开发版本不能执行自动更新")
	}
	for i := 1; i <= 3; i++ {
		x, e := strconv.ParseUint(a[i], 10, 32)
		if e != nil {
			return false, e
		}
		y, e := strconv.ParseUint(b[i], 10, 32)
		if e != nil {
			return false, e
		}
		if x != y {
			return x > y, nil
		}
	}
	return false, nil
}
func (s *Service) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "TDL-Media/"+s.Current)
	req.Header.Set("Accept", "application/vnd.github+json")
	response, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("检测或下载更新失败：HTTP %d", response.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("更新文件超过大小上限")
	}
	return b, nil
}
func (s *Service) Check(ctx context.Context) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	b, err := s.get(ctx, latestURL, 2<<20)
	if err != nil {
		return Info{}, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err = json.Unmarshal(b, &release); err != nil {
		return Info{}, err
	}
	available, err := newer(release.Tag, s.Current)
	if err != nil {
		return Info{}, err
	}
	if release.Draft || release.Prerelease {
		return Info{}, errors.New("更新源未提供正式版本")
	}
	version := strings.TrimPrefix(release.Tag, "v")
	info := Info{Current: s.Current, Latest: version, Available: available, Notes: release.Body, URL: repository + "/releases/tag/" + release.Tag, FileName: "TDL-Media-windows-x64-" + version + ".zip"}
	prefix := repository + "/releases/download/" + release.Tag + "/"
	for _, asset := range release.Assets {
		if asset.Name == info.FileName && asset.URL == prefix+info.FileName {
			info.PackageURL = asset.URL
		}
		if asset.Name == info.FileName+".sha256" && asset.URL == prefix+info.FileName+".sha256" {
			info.ChecksumURL = asset.URL
		}
	}
	if available && (info.PackageURL == "" || info.ChecksumURL == "") {
		return Info{}, errors.New("新版本的便携包或校验文件尚未就绪，请稍后重试")
	}
	return info, nil
}
func checksum(data []byte, name string) (string, error) {
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name && len(fields[0]) == 64 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return strings.ToLower(fields[0]), nil
			}
		}
	}
	return "", errors.New("更新包缺少有效的 SHA256 校验值")
}
func hashFile(path string) (string, error) {
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

func (s *Service) Prepare(ctx context.Context) (Info, error) {
	if !s.mu.TryLock() {
		return Info{}, errors.New("正在准备更新")
	}
	defer s.mu.Unlock()
	s.prepared = nil
	for _, name := range packageFiles[:2] {
		if st, err := os.Stat(filepath.Join(s.InstallDir, name)); err != nil || st.IsDir() {
			return Info{}, errors.New("请在解压后的便携包中更新，GUI 和后台程序需要位于同一目录")
		}
	}
	stage, err := os.MkdirTemp(s.InstallDir, ".tdl-update-")
	if err != nil {
		return Info{}, fmt.Errorf("程序目录不可写，请将便携包移到可写目录：%w", err)
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stage)
		}
	}()
	info, err := s.Check(ctx)
	if err != nil {
		return Info{}, err
	}
	if !info.Available {
		return info, errors.New("当前已是最新版本")
	}
	checks, err := s.get(ctx, info.ChecksumURL, 64<<10)
	if err != nil {
		return Info{}, err
	}
	want, err := checksum(checks, info.FileName)
	if err != nil {
		return Info{}, err
	}
	// Stream the package to disk instead of retaining an entire executable ZIP in memory.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.PackageURL, nil)
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("User-Agent", "TDL-Media/"+s.Current)
	response, err := s.Client.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return Info{}, fmt.Errorf("下载更新失败：HTTP %d", response.StatusCode)
	}
	path := filepath.Join(stage, "package.zip")
	f, err := os.Create(path)
	if err != nil {
		return Info{}, err
	}
	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(response.Body, maxPackageBytes+1))
	closeErr := f.Close()
	if copyErr != nil {
		return Info{}, copyErr
	}
	if closeErr != nil {
		return Info{}, closeErr
	}
	if n > maxPackageBytes {
		return Info{}, errors.New("更新包过大")
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		return Info{}, errors.New("更新包 SHA256 校验失败，已停止更新")
	}
	hashes, err := extract(path, stage)
	if err != nil {
		return Info{}, err
	}
	s.prepared = &manifest{InstallDir: s.InstallDir, StageDir: stage, Version: info.Latest, Hashes: hashes}
	keep = true
	return info, nil
}

func extract(path, stage string) (map[string]string, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	allowed := map[string]bool{}
	for _, name := range packageFiles {
		allowed[name] = true
	}
	hashes := map[string]string{}
	var total uint64
	for _, entry := range z.File {
		if !allowed[entry.Name] || hashes[entry.Name] != "" || entry.Mode()&os.ModeSymlink != 0 || entry.FileInfo().IsDir() {
			return nil, fmt.Errorf("更新包包含不允许的文件：%s", entry.Name)
		}
		total += entry.UncompressedSize64
		if total > 512<<20 {
			return nil, errors.New("更新解压大小超过上限")
		}
		in, err := entry.Open()
		if err != nil {
			return nil, err
		}
		out, err := os.OpenFile(filepath.Join(stage, entry.Name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
		if err != nil {
			in.Close()
			return nil, err
		}
		_, copyErr := io.Copy(out, io.LimitReader(in, 512<<20+1))
		in.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return nil, copyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		hash, err := hashFile(filepath.Join(stage, entry.Name))
		if err != nil {
			return nil, err
		}
		hashes[entry.Name] = hash
	}
	for _, name := range packageFiles {
		if hashes[name] == "" {
			return nil, fmt.Errorf("更新包缺少 %s", name)
		}
	}
	return hashes, nil
}

func (s *Service) Result() *Result {
	b, err := os.ReadFile(filepath.Join(s.InstallDir, ".tdl-update-result.json"))
	if err != nil {
		return nil
	}
	var result Result
	if json.Unmarshal(b, &result) != nil {
		return nil
	}
	return &result
}
