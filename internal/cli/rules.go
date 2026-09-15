package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
	"github.com/local/tdl-gui/internal/planner"
	"github.com/spf13/cobra"
)

func (s *rootState) rulesCmd() *cobra.Command {
	c := &cobra.Command{Use: "rules", Short: "保存和复用下载规则"}
	c.AddCommand(&cobra.Command{Use: "list", Short: "列出规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Store.Rules(cmd.Context())
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	c.AddCommand(&cobra.Command{Use: "show ID", Args: oneArg, Short: "显示规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Store.Rule(cmd.Context(), args[0])
		if e != nil {
			return e
		}
		return s.print(v)
	}})
	var r domain.Rule
	var from, to, kinds, includeExt, excludeExt string
	create := &cobra.Command{Use: "create", Short: "创建下载规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if r.AccountID == "" {
			acc, e := a.Store.ActiveAccount(cmd.Context())
			if e != nil {
				return e
			}
			r.AccountID = acc.ID
		}
		r.ID = idgen.New("rule")
		if r.Name == "" {
			r.Name = "下载规则 " + time.Now().Format("2006-01-02 15:04")
		}
		if r.RootDir == "" {
			r.RootDir = a.Settings.DownloadRoot
		}
		if r.Template == "" {
			r.Template = planner.DefaultTemplate
		}
		if r.Order == "" {
			r.Order = "oldest"
		}
		if r.Timezone == "" {
			r.Timezone = time.Local.String()
		}
		r.From, e = parseOptionalTime(from, false)
		if e != nil {
			return e
		}
		r.To, e = parseOptionalTime(to, true)
		if e != nil {
			return e
		}
		r.Kinds = splitCSV(kinds)
		r.IncludeExt = splitCSV(includeExt)
		r.ExcludeExt = splitCSV(excludeExt)
		if e = planner.ValidateRule(r); e != nil {
			return e
		}
		if e = a.Store.SaveRule(cmd.Context(), r); e != nil {
			return e
		}
		return s.print(r)
	}}
	f := create.Flags()
	f.StringVar(&r.Name, "name", "", "规则名称")
	f.StringVar(&r.AccountID, "account", "", "账户 ID")
	f.StringVar(&r.ChatID, "chat", "", "聊天 ID")
	f.StringVar(&r.TopicID, "topic", "", "话题 ID")
	f.StringVar(&from, "from", "", "开始日期或 RFC3339")
	f.StringVar(&to, "to", "", "结束日期或 RFC3339")
	f.IntVar(&r.RecentDays, "recent-days", 0, "最近天数")
	f.IntVar(&r.LastN, "last", 0, "最近 N 个媒体")
	f.Int64Var(&r.MinMessageID, "min-message-id", 0, "消息 ID 下限")
	f.Int64Var(&r.MaxMessageID, "max-message-id", 0, "消息 ID 上限")
	f.StringVar(&kinds, "kinds", "", "媒体类型，逗号分隔")
	f.StringVar(&includeExt, "include-ext", "", "包含扩展名，逗号分隔")
	f.StringVar(&excludeExt, "exclude-ext", "", "排除扩展名，逗号分隔")
	f.StringVar(&r.IncludeKeyword, "include-keyword", "", "包含关键词")
	f.StringVar(&r.ExcludeKeyword, "exclude-keyword", "", "排除关键词")
	f.Int64Var(&r.MinFileSize, "min-size", 0, "单文件最小字节数")
	f.Int64Var(&r.MaxFileSize, "max-size", 0, "单文件最大字节数")
	f.IntVar(&r.MaxFiles, "max-files", 0, "任务最大文件数")
	f.Int64Var(&r.MaxTotalSize, "max-total-size", 0, "任务最大总字节数")
	f.StringVar(&r.Order, "order", "oldest", "oldest 或 newest")
	f.StringVar(&r.Timezone, "timezone", time.Local.String(), "IANA 时区")
	f.StringVar(&r.RootDir, "root", "", "下载根目录")
	f.StringVar(&r.Template, "template", planner.DefaultTemplate, "文件路径模板")
	_ = create.MarkFlagRequired("chat")
	c.AddCommand(create)
	c.AddCommand(&cobra.Command{Use: "delete ID", Args: oneArg, Short: "删除规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		if e = a.Store.DeleteRule(cmd.Context(), args[0]); e != nil {
			return e
		}
		return s.print("已删除规则")
	}})
	var exportPath string
	export := &cobra.Command{Use: "export", Short: "导出全部规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		v, e := a.Store.Rules(cmd.Context())
		if e != nil {
			return e
		}
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		if exportPath == "" {
			fmt.Println(string(b))
			return nil
		}
		return os.WriteFile(exportPath, b, 0o600)
	}}
	export.Flags().StringVar(&exportPath, "file", "", "输出文件，留空写到标准输出")
	c.AddCommand(export)
	importCmd := &cobra.Command{Use: "import FILE", Args: oneArg, Short: "导入规则", RunE: func(cmd *cobra.Command, args []string) error {
		a, e := s.get()
		if e != nil {
			return e
		}
		b, e := os.ReadFile(args[0])
		if e != nil {
			return e
		}
		var rules []domain.Rule
		if e = json.Unmarshal(b, &rules); e != nil {
			var one domain.Rule
			if x := json.Unmarshal(b, &one); x != nil {
				return e
			}
			rules = []domain.Rule{one}
		}
		for i := range rules {
			if rules[i].ID == "" {
				rules[i].ID = idgen.New("rule")
			}
			if e = planner.ValidateRule(rules[i]); e != nil {
				return fmt.Errorf("rule %d: %w", i, e)
			}
			if e = a.Store.SaveRule(cmd.Context(), rules[i]); e != nil {
				return e
			}
		}
		return s.print(map[string]int{"imported": len(rules)})
	}}
	c.AddCommand(importCmd)
	return c
}
func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, x := range parts {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
