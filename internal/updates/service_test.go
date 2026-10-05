package updates

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (fn roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
func packageZIP(t *testing.T, names []string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, name := range names {
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("new " + name))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func fixture(t *testing.T, scenario string) *Service {
	t.Helper()
	dir := t.TempDir()
	for _, name := range packageFiles {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("old "+name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	names := packageFiles
	if scenario == "traversal" {
		names = append(append([]string{}, names...), "../escape")
	}
	if scenario == "missing-file" {
		names = names[:2]
	}
	pkg := packageZIP(t, names)
	hash := sha256.Sum256(pkg)
	digest := hex.EncodeToString(hash[:])
	if scenario == "mismatch" {
		digest = strings.Repeat("0", 64)
	}
	tag := "v0.2.1"
	if scenario == "old" {
		tag = "v0.1.9"
	}
	name := "TDL-Media-windows-x64-" + strings.TrimPrefix(tag, "v") + ".zip"
	prefix := repository + "/releases/download/" + tag + "/"
	packageURL := prefix + name
	if scenario == "foreign" {
		packageURL = "https://untrusted.example/" + name
	}
	s := New("0.2.0", dir)
	s.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		status := 200
		var payload []byte
		switch r.URL.String() {
		case latestURL:
			assets := []any{map[string]string{"name": name, "browser_download_url": packageURL}}
			if scenario != "missing-checksum" {
				assets = append(assets, map[string]string{"name": name + ".sha256", "browser_download_url": prefix + name + ".sha256"})
			}
			payload, _ = json.Marshal(map[string]any{"tag_name": tag, "body": "update notes", "assets": assets})
			if scenario == "rate-limit" {
				status = 403
			}
		case prefix + name + ".sha256":
			payload = []byte(digest + "  " + name)
			if scenario == "bad-checksum" {
				payload = []byte("invalid")
			}
		case prefix + name:
			payload = pkg
		default:
			status = 404
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})}
	return s
}

func TestSemanticVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{{"v0.2.1", "0.2.0", true}, {"0.10.0", "0.9.9", true}, {"v0.2.0", "0.2.0", false}, {"0.1.9", "0.2.0", false}} {
		got, err := newer(tc.a, tc.b)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	if _, err := newer("0.2.1", "dev"); err == nil {
		t.Fatal("dev build accepted")
	}
}
func TestCheckAndPrepareVerifiedRelease(t *testing.T) {
	s := fixture(t, "success")
	info, err := s.Check(context.Background())
	if err != nil || !info.Available || info.Latest != "0.2.1" {
		t.Fatal(info, err)
	}
	info, err = s.Prepare(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.Latest != "0.2.1" || s.prepared == nil {
		t.Fatal("not prepared")
	}
	if err = validate(*s.prepared); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(s.prepared.StageDir, packageFiles[0]), []byte("tampered"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = validate(*s.prepared); err == nil {
		t.Fatal("modified staging accepted")
	}
}
func TestPrepareFailureLeavesInstalledFilesUntouched(t *testing.T) {
	for _, scenario := range []string{"mismatch", "missing-checksum", "bad-checksum", "missing-file", "traversal", "foreign", "rate-limit", "old"} {
		t.Run(scenario, func(t *testing.T) {
			s := fixture(t, scenario)
			if _, err := s.Prepare(context.Background()); err == nil {
				t.Fatal("invalid update accepted")
			}
			if s.prepared != nil {
				t.Fatal("failed update prepared")
			}
			for _, name := range packageFiles {
				b, _ := os.ReadFile(filepath.Join(s.InstallDir, name))
				if string(b) != "old "+name {
					t.Fatal("installed file changed")
				}
			}
			dirs, _ := filepath.Glob(filepath.Join(s.InstallDir, ".tdl-update-*"))
			if len(dirs) != 0 {
				t.Fatal("failed download left staging")
			}
		})
	}
}
func TestApplyAndRollbackPreserveUserData(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "rollback"}[fail], func(t *testing.T) {
			s := fixture(t, "success")
			if _, err := s.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			dataPath := filepath.Join(s.InstallDir, "user-data.db")
			_ = os.WriteFile(dataPath, []byte("keep"), 0600)
			err := applyFiles(*s.prepared, func() error {
				if fail {
					return errors.New("restart failed")
				}
				return nil
			})
			if (err != nil) != fail {
				t.Fatal(err)
			}
			prefix := "new "
			if fail {
				prefix = "old "
			}
			for _, name := range packageFiles {
				b, e := os.ReadFile(filepath.Join(s.InstallDir, name))
				if e != nil || string(b) != prefix+name {
					t.Fatal(name, string(b), e)
				}
			}
			b, _ := os.ReadFile(dataPath)
			if string(b) != "keep" {
				t.Fatal("user data altered")
			}
		})
	}
}
