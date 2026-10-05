package engine

import (
	"context"
	"io"
	"net/http"
	"runtime"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (fn transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestInstallRequiresUsableChecksum(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows installer")
	}
	for _, scenario := range []string{"missing", "unavailable", "unlisted", "malformed", "mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			m := New(t.TempDir(), nil)
			m.client = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
				body := "package"
				status := 200
				if strings.Contains(r.URL.Path, "/releases/") {
					extra := `,{"name":"checksums.txt","browser_download_url":"https://fixture/checks"}`
					if scenario == "missing" {
						extra = ""
					}
					body = `{"tag_name":"v0.20.4","assets":[{"name":"tdl_Windows_64bit.zip","browser_download_url":"https://fixture/package"}` + extra + `]}`
				} else if r.URL.Path == "/checks" {
					switch scenario {
					case "unavailable":
						status = 500
					case "unlisted":
						body = "abc  another.zip"
					case "malformed":
						body = "xyz  tdl_Windows_64bit.zip"
					default:
						body = strings.Repeat("0", 64) + "  tdl_Windows_64bit.zip"
					}
				}
				return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})}
			if _, err := m.Install(context.Background(), "v0.20.4"); err == nil {
				t.Fatal("unverified install succeeded")
			}
		})
	}
}
