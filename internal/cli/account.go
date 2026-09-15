package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (s *rootState) accountCmd() *cobra.Command {
	c := &cobra.Command{Use: "account", Short: "管理 Telegram 账户"}
	var namespace string
	add := &cobra.Command{Use: "add NAME", Args: oneArg, Short: "创建账户配置", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Accounts.Add(cmd.Context(), args[0], namespace)
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	add.Flags().StringVar(&namespace, "namespace", "", "tdl 账户命名空间")
	c.AddCommand(add)
	c.AddCommand(&cobra.Command{Use: "list", Short: "列出账户", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Store.Accounts(cmd.Context())
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "use ID", Args: oneArg, Short: "切换活动账户", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Store.SetActiveAccount(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print("已切换账户")
	}})
	var method, desktop, passcode string
	login := &cobra.Command{Use: "login ID", Args: oneArg, Short: "登录账户（交互式）", RunE: func(cmd *cobra.Command, args []string) error {
		if _, e := activeEnginePath(cmd.Context(), s); e != nil {
			return e
		}
		a, e := s.get()
		if e != nil {
			return e
		}
		return a.Accounts.Login(cmd.Context(), args[0], method, desktop, passcode)
	}}
	login.Flags().StringVar(&method, "method", "qr", "qr、code 或 desktop")
	login.Flags().StringVar(&desktop, "desktop", "", "Telegram Desktop 程序或 tdata 目录")
	login.Flags().StringVar(&passcode, "passcode", "", "Telegram Desktop 本地密码")
	c.AddCommand(login)
	c.AddCommand(&cobra.Command{Use: "verify ID", Args: oneArg, Short: "验证登录状态", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Accounts.Verify(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print("登录状态有效")
	}})
	c.AddCommand(&cobra.Command{Use: "remove ID", Args: oneArg, Short: "删除本地账户索引", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Store.DeleteAccount(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print(fmt.Sprintf("已删除账户 %s 的本地索引", args[0]))
	}})
	return c
}
