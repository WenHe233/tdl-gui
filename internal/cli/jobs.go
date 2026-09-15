package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (s *rootState) jobsCmd() *cobra.Command {
	c := &cobra.Command{Use: "jobs", Short: "创建和管理下载任务"}
	c.AddCommand(&cobra.Command{Use: "create PLAN_ID", Args: oneArg, Short: "从固定清单创建任务", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Jobs.Create(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "run JOB_ID", Args: oneArg, Short: "运行或恢复任务", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Jobs.Run(cmd.Context(), args[0]); e != nil {
			return e
		}
		j, items, e := a.Store.Job(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return s.print(map[string]any{"job": j, "items": items})
	}})
	c.AddCommand(&cobra.Command{Use: "retry JOB_ID", Args: oneArg, Short: "重试失败项", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		return a.Jobs.Run(cmd.Context(), args[0])
	}})
	c.AddCommand(&cobra.Command{Use: "list", Short: "列出任务", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Store.Jobs(cmd.Context())
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "show JOB_ID", Args: oneArg, Short: "显示任务及文件结果", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		j, items, e := a.Store.Job(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return s.print(map[string]any{"job": j, "items": items})
	}})
	c.AddCommand(&cobra.Command{Use: "pause JOB_ID", Args: oneArg, Short: "暂停运行中的任务", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if !a.Jobs.Pause(args[0]) {
			return fmt.Errorf("任务当前未在此进程运行")
		}
		return s.print("已请求暂停")
	}})
	c.AddCommand(&cobra.Command{Use: "cancel JOB_ID", Args: oneArg, Short: "取消任务", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Jobs.Cancel(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print("已取消任务")
	}})
	return c
}
