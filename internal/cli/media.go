package cli

import (
	"fmt"
	"time"

	"github.com/local/tdl-gui/internal/catalog"
	"github.com/spf13/cobra"
)

func (s *rootState) mediaCmd() *cobra.Command {
	c := &cobra.Command{Use: "media", Short: "扫描、浏览和预览媒体"}
	var accountID, topic, from, to string
	var last int
	var rescan bool
	scan := &cobra.Command{Use: "scan CHAT_ID", Args: oneArg, Short: "扫描聊天媒体", RunE: func(cmd *cobra.Command, args []string) error {
		app, e := s.get()
		if e != nil {
			return e
		}
		a, e := s.resolveAccount(cmd, accountID)
		if e != nil {
			return e
		}
		f, e := parseOptionalTime(from, false)
		if e != nil {
			return e
		}
		t, e := parseOptionalTime(to, true)
		if e != nil {
			return e
		}
		v, e := app.Catalog.Scan(cmd.Context(), a, catalog.ScanOptions{ChatID: args[0], TopicID: topic, From: f, To: t, LastN: last, Rescan:rescan})
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	scan.Flags().StringVar(&accountID, "account", "", "账户 ID")
	scan.Flags().StringVar(&topic, "topic", "", "话题 ID")
	scan.Flags().StringVar(&from, "from", "", "开始日期或 RFC3339 时间")
	scan.Flags().StringVar(&to, "to", "", "结束日期或 RFC3339 时间")
	scan.Flags().IntVar(&last, "last", 0, "只扫描最近 N 个媒体")
	scan.Flags().BoolVar(&rescan, "rescan", false, "忽略增量游标并重新扫描历史")
	c.AddCommand(scan)
	var listAccount string
	list := &cobra.Command{Use: "list CHAT_ID", Args: oneArg, Short: "列出已索引媒体", RunE: func(cmd *cobra.Command, args []string) error {
		app, e := s.get()
		if e != nil {
			return e
		}
		a, e := s.resolveAccount(cmd, listAccount)
		if e != nil {
			return e
		}
		v, e := app.Store.Media(cmd.Context(), a.ID, args[0])
		if e != nil {
			return e
		}
		return s.print(v)
	}}
	list.Flags().StringVar(&listAccount, "account", "", "账户 ID")
	c.AddCommand(list)
	preview := &cobra.Command{Use: "preview RULE_ID", Args: oneArg, Short: "从保存的规则生成固定下载清单", RunE: func(cmd *cobra.Command, args []string) error {
		app, e := s.get()
		if e != nil {
			return e
		}
		r, e := app.Store.Rule(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		a, e := app.Store.Account(cmd.Context(), r.AccountID)
		if e != nil {
			return e
		}
		ch, e := app.Store.Chat(cmd.Context(), r.AccountID, r.ChatID)
		if e != nil {
			return e
		}
		p, e := app.Planner.Build(cmd.Context(), r, a, ch)
		if e != nil {
			return e
		}
		if e = app.Store.SavePlan(cmd.Context(), p); e != nil {
			return e
		}
		return s.print(p)
	}}
	c.AddCommand(preview)
	return c
}

func parseOptionalTime(v string, endOfDay bool) (time.Time, error) {
	if v == "" {
		return time.Time{}, nil
	}
	if t, e := time.Parse(time.RFC3339, v); e == nil {
		return t, nil
	}
	t, e := time.ParseInLocation("2006-01-02", v, time.Local)
	if e != nil {
		return time.Time{}, fmt.Errorf("invalid date %q; use YYYY-MM-DD or RFC3339", v)
	}
	if endOfDay {
		return t.Add(24*time.Hour - time.Nanosecond), nil
	}
	return t, nil
}
