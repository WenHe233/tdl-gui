package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/local/tdl-gui/internal/app"
	"github.com/local/tdl-gui/internal/jobs"
	"github.com/spf13/cobra"
)

type rootState struct {
	dataDir string
	json    bool
	version string
	mu      sync.Mutex
	app     *app.Application
}

func New(version string) *cobra.Command { cmd, _ := NewWithCleanup(version); return cmd }
func NewWithCleanup(version string) (*cobra.Command, func() error) {
	s := &rootState{version: version}
	root := &cobra.Command{Use: "tdl-media", Short: "基于 tdl 的 Telegram 媒体浏览与批量下载器", Version: version, SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&s.dataDir, "data-dir", "", "数据目录（默认使用系统用户配置目录）")
	root.PersistentFlags().BoolVar(&s.json, "json", false, "输出 JSON")
	root.AddCommand(s.engineCmd(), s.accountCmd(), s.chatsCmd(), s.mediaCmd(), s.rulesCmd(), s.jobsCmd(), s.configCmd(), s.doctorCmd(), s.workerCmd())
	return root, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.app != nil {
			return s.app.Close()
		}
		return nil
	}
}
func (s *rootState) get() (*app.Application, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.app != nil {
		return s.app, nil
	}
	a, err := app.Open(s.dataDir, func(e jobs.Event) {
		if s.json {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"event": e})
		} else if e.Type == "job.log" {
			fmt.Println(e.Message)
		}
	})
	if err != nil {
		return nil, err
	}
	s.app = a
	return a, nil
}
func (s *rootState) print(v any) error {
	if s.json {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	switch x := v.(type) {
	case string:
		fmt.Println(x)
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(b))
	}
	return nil
}
func oneArg(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("%s 需要一个参数", cmd.CommandPath())
	}
	return nil
}
