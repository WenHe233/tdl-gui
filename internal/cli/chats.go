package cli

import (
	"github.com/local/tdl-gui/internal/domain"
	"github.com/spf13/cobra"
)

func (s *rootState) resolveAccount(cmd *cobra.Command, id string) (domain.Account, error) {
	a, e := s.get()
	if e != nil {
		return domain.Account{}, e
	}
	if id != "" {
		return a.Store.Account(cmd.Context(), id)
	}
	return a.Store.ActiveAccount(cmd.Context())
}
func (s *rootState) chatsCmd() *cobra.Command {
	c := &cobra.Command{Use: "chats", Short: "浏览 Telegram 聊天"}
	var accountID, query string
	list := &cobra.Command{Use: "list", Short: "列出缓存的聊天", RunE: func(cmd *cobra.Command, args []string) error {
		app, e := s.get()
		if e != nil {
			return e
		}
		a, e := s.resolveAccount(cmd, accountID)
		if e != nil {
			return e
		}
		v, e := app.Store.Chats(cmd.Context(), a.ID, query)
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	list.Flags().StringVar(&accountID, "account", "", "账户 ID，默认当前账户")
	list.Flags().StringVar(&query, "query", "", "按名称或用户名搜索")
	c.AddCommand(list)
	var refreshAccount string
	refresh := &cobra.Command{Use: "refresh", Short: "从 Telegram 刷新聊天列表", RunE: func(cmd *cobra.Command, args []string) error {
		app, e := s.get()
		if e != nil {
			return e
		}
		a, e := s.resolveAccount(cmd, refreshAccount)
		if e != nil {
			return e
		}
		v, e := app.Catalog.RefreshChats(cmd.Context(), a)
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	refresh.Flags().StringVar(&refreshAccount, "account", "", "账户 ID，默认当前账户")
	c.AddCommand(refresh)
	return c
}
