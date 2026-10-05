package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func (s *Service) Schedule(guiPID int, guiPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return errors.New("一键更新仅支持 Windows x64 便携版")
	}
	if s.prepared == nil {
		return errors.New("请先下载并校验更新")
	}
	expected, _ := filepath.Abs(filepath.Join(s.InstallDir, "tdl-media-gui.exe"))
	provided, _ := filepath.Abs(guiPath)
	if guiPID <= 0 || !strings.EqualFold(expected, provided) {
		return errors.New("更新目标与当前便携包不一致")
	}
	m := *s.prepared
	m.GUIProcess = guiPID
	m.WorkerProcess = os.Getpid()
	if err := validate(m); err != nil {
		return err
	}
	current, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(m.StageDir, "updater.exe")
	in, err := os.Open(current)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(helper, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	path := filepath.Join(m.StageDir, "update.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		return err
	}
	cmd := exec.Command(helper, "internal-apply-update", path)
	background(cmd)
	if err = cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	s.prepared = nil
	return nil
}

func validate(m manifest) error {
	install, err := filepath.Abs(m.InstallDir)
	if err != nil {
		return err
	}
	stage, err := filepath.Abs(m.StageDir)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(stage), install) || !strings.HasPrefix(filepath.Base(stage), ".tdl-update-") {
		return errors.New("更新暂存目录无效")
	}
	if len(m.Hashes) != len(packageFiles) {
		return errors.New("更新清单文件数无效")
	}
	for _, name := range packageFiles {
		st, err := os.Lstat(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return errors.New("更新文件类型无效")
		}
		got, err := hashFile(filepath.Join(stage, name))
		if err != nil {
			return err
		}
		if got != m.Hashes[name] {
			return fmt.Errorf("更新文件已改变：%s", name)
		}
	}
	return nil
}

func RunHelper(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var m manifest
	if err = json.Unmarshal(b, &m); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Dir(self), m.StageDir) {
		return errors.New("更新助手必须位于暂存目录")
	}
	if err = validate(m); err != nil {
		return err
	}
	err = func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		if err := waitProcess(ctx, m.WorkerProcess); err != nil {
			return err
		}
		if err := waitProcess(ctx, m.GUIProcess); err != nil {
			return err
		}
		checkCtx, stop := context.WithTimeout(context.Background(), 20*time.Second)
		defer stop()
		check := exec.CommandContext(checkCtx, filepath.Join(m.StageDir, "tdl-media.exe"), "--version")
		background(check)
		output, err := check.Output()
		if err != nil {
			return fmt.Errorf("新版本后台启动检查失败：%w", err)
		}
		if !strings.HasSuffix(strings.TrimSpace(string(output)), "version "+m.Version) {
			return errors.New("更新包版本不一致")
		}
		return applyFiles(m, func() error { return restartGUI(m.InstallDir) })
	}()
	result := Result{Version: m.Version, Success: err == nil}
	if err != nil {
		result.Error = err.Error()
	}
	data, _ := json.Marshal(result)
	_ = os.WriteFile(filepath.Join(m.InstallDir, ".tdl-update-result.json"), data, 0600)
	if err != nil {
		_ = restartGUI(m.InstallDir)
	}
	return err
}

// Only the four release files are replaced; account data and downloads are untouched.
func applyFiles(m manifest, restart func() error) error {
	if err := validate(m); err != nil {
		return err
	}
	backup := filepath.Join(m.StageDir, "backup")
	if err := os.Mkdir(backup, 0700); err != nil {
		return err
	}
	var moved, installed []string
	rollback := func(cause error) error {
		var failures []string
		for _, name := range installed {
			if err := os.Remove(filepath.Join(m.InstallDir, name)); err != nil && !os.IsNotExist(err) {
				failures = append(failures, err.Error())
			}
		}
		for i := len(moved) - 1; i >= 0; i-- {
			name := moved[i]
			if err := os.Rename(filepath.Join(backup, name), filepath.Join(m.InstallDir, name)); err != nil {
				failures = append(failures, err.Error())
			}
		}
		if len(failures) > 0 {
			return fmt.Errorf("更新失败：%v；恢复旧版本失败，请从 %s 恢复：%s", cause, backup, strings.Join(failures, "; "))
		}
		return fmt.Errorf("更新失败，已恢复旧版本：%w", cause)
	}
	for _, name := range packageFiles {
		old := filepath.Join(m.InstallDir, name)
		if _, err := os.Stat(old); err == nil {
			if err = os.Rename(old, filepath.Join(backup, name)); err != nil {
				return rollback(err)
			}
			moved = append(moved, name)
		} else if !os.IsNotExist(err) {
			return rollback(err)
		}
	}
	for _, name := range packageFiles {
		if err := os.Rename(filepath.Join(m.StageDir, name), filepath.Join(m.InstallDir, name)); err != nil {
			return rollback(err)
		}
		installed = append(installed, name)
	}
	if err := restart(); err != nil {
		return rollback(err)
	}
	return nil
}

func restartGUI(dir string) error {
	cmd := exec.Command(filepath.Join(dir, "tdl-media-gui.exe"))
	cmd.Dir = dir
	background(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return fmt.Errorf("新窗口提前退出：%v", err)
	case <-time.After(3 * time.Second):
		return nil
	}
}
