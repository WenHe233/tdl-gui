package cli

import (
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

func (s *rootState) configCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "查看和修改设置"}
	c.AddCommand(&cobra.Command{Use: "show", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		return s.print(a.Settings)
	}})
	c.AddCommand(&cobra.Command{Use: "set KEY VALUE", Args: func(cmd *cobra.Command, args []string) error {
		if len(args) != 2 {
			return cmd.Help()
		}
		return nil
	}, RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.SetConfig(cmd.Context(), args[0], args[1]); e != nil {
			return e
		}
		return s.print("配置已保存，下一次启动生效")
	}})
	return c
}
func (s *rootState) doctorCmd() *cobra.Command {
	return &cobra.Command{Use: "doctor", Short: "检查运行环境和数据目录", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		checks := map[string]any{"version": s.version, "os": runtime.GOOS, "arch": runtime.GOARCH, "dataDir": a.Paths.Root, "dataDirWritable": writable(a.Paths.Root), "database": "ok"}
		if v, e := a.Engine.Active(cmd.Context()); e == nil {
			checks["engine"] = v
			checks["engineReady"] = true
		} else {
			checks["engineReady"] = false
			checks["engineError"] = e.Error()
		}
		if acc, e := a.Store.ActiveAccount(cmd.Context()); e == nil {
			checks["activeAccount"] = acc
			checks["accountConfigured"] = true
		} else {
			checks["accountConfigured"] = false
		}
		return s.print(checks)
	}}
}
func writable(dir string) bool {
	f, e := os.CreateTemp(dir, "doctor-")
	if e != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
