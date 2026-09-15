package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
)

func (s *rootState) engineCmd() *cobra.Command {
	c := &cobra.Command{Use: "engine", Short: "安装和管理官方 tdl 引擎"}
	c.AddCommand(&cobra.Command{Use: "status", Short: "显示当前引擎", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Engine.Active(cmd.Context())
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "check", Short: "检查官方最新版本", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		latest, e := a.Engine.Latest(cmd.Context())
		if e != nil {
			return e
		}
		active, _ := a.Engine.Active(cmd.Context())
		return s.print(map[string]any{"latest": latest, "installed": active.Version, "updateAvailable": active.Version != "" && active.Version != latest})
	}})
	var version string
	install := &cobra.Command{Use: "install", Short: "下载并安装官方 tdl", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Engine.Install(cmd.Context(), version)
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	install.Flags().StringVar(&version, "version", "latest", "版本号或 latest")
	c.AddCommand(install)
	c.AddCommand(&cobra.Command{Use: "list", Short: "列出本机引擎版本", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Engine.Installed(cmd.Context())
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "use VERSION_OR_PATH", Short: "切换版本或指定已有 tdl", Args: oneArg, RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Engine.Use(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print("已切换 tdl 引擎")
	}})
	c.AddCommand(&cobra.Command{Use: "rollback", Short: "回退到上一个已安装版本", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Engine.Rollback(cmd.Context()); e != nil {
			return e
		}
		return s.print("已回退 tdl 引擎")
	}})
	return c
}

func activeEnginePath(ctx context.Context, s *rootState) (string, error) {
	a, e := s.get()
	if e != nil {
		return "", e
	}
	v, e := a.Engine.Active(ctx)
	if e != nil {
		return "", fmt.Errorf("tdl 引擎不可用: %w", e)
	}
	return v.Path, nil
}
