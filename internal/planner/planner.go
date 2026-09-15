package planner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/local/tdl-gui/internal/domain"
	"github.com/local/tdl-gui/internal/idgen"
)

type MediaSource interface {
	Media(context.Context, string, string) ([]domain.Media, error)
}

type Planner struct{ source MediaSource }

func New(source MediaSource) *Planner { return &Planner{source: source} }

func (p *Planner) Build(ctx context.Context, rule domain.Rule, account domain.Account, chat domain.Chat) (domain.DownloadPlan, error) {
	items, err := p.source.Media(ctx, rule.AccountID, rule.ChatID)
	if err != nil {
		return domain.DownloadPlan{}, err
	}
	if rule.RecentDays > 0 {
		rule.From = time.Now().In(location(rule.Timezone)).AddDate(0, 0, -rule.RecentDays)
	}
	lastSet:=map[string]bool{}
	if rule.LastN>0 { candidates:=make([]domain.Media,0,len(items));for _,m:=range items{if rejectReason(m,rule)==""{candidates=append(candidates,m)}};sort.SliceStable(candidates,func(i,j int)bool{return candidates[i].Date.After(candidates[j].Date)});if len(candidates)>rule.LastN{candidates=candidates[:rule.LastN]};for _,m:=range candidates{lastSet[m.MessageID]=true} }
	if rule.Order == "newest" {
		sort.SliceStable(items, func(i, j int) bool { return items[i].Date.After(items[j].Date) })
	}
	if rule.Order == "" || rule.Order == "oldest" {
		sort.SliceStable(items, func(i, j int) bool { return items[i].Date.Before(items[j].Date) })
	}
	plan := domain.DownloadPlan{ID: idgen.New("plan"), RuleID: rule.ID, AccountID: rule.AccountID, ChatID: rule.ChatID, CreatedAt: time.Now().UTC(), MessageCount: len(items)}
	uniqueAll:=map[string]bool{};for _,m:=range items{id:=m.MediaID;if id==""{id=m.MessageID};uniqueAll[id]=true};plan.UniqueFiles=len(uniqueAll)
	seen := map[string]string{}
	included := 0
	var bytes int64
	for _, m := range items {
		pi := domain.PlanItem{Media: m, Selected: true, Status: "selected", CanonicalID: m.MediaID}
		if pi.CanonicalID == "" {
			pi.CanonicalID = m.MessageID
		}
		if rule.LastN > 0 && !lastSet[m.MessageID] {
			pi.Selected = false
			pi.Status = "filtered"
			pi.Reason = "不在最近媒体数范围内"
			plan.Items = append(plan.Items, pi)
			continue
		}
		if canonical, ok := seen[pi.CanonicalID]; ok {
			pi.Selected = false
			pi.Status = "duplicate"
			pi.Reason = "同一聊天内已由消息 " + canonical + " 代表"
			plan.Items = append(plan.Items, pi)
			continue
		}
		seen[pi.CanonicalID] = m.MessageID
		if reason := rejectReason(m, rule); reason != "" {
			pi.Selected = false
			pi.Status = "filtered"
			pi.Reason = reason
			plan.Items = append(plan.Items, pi)
			continue
		}
		name := m.FileName
		if name == "" {
			name = "media_" + m.MessageID + m.Extension
		}
		target, renderErr := Render(rule.RootDir, rule.Template, TemplateData{AccountName: account.DisplayName, ChatName: chat.VisibleName, ChatID: chat.ID, MessageID: m.MessageID, MediaID: m.MediaID, Kind: m.Kind, Date: m.Date, OriginalName: name, BaseName: strings.TrimSuffix(name, filepath.Ext(name)), Extension: filepath.Ext(name)})
		if renderErr != nil {
			return domain.DownloadPlan{}, renderErr
		}
		pi.TargetPath = target
		if rule.MaxFiles > 0 && included >= rule.MaxFiles {
			pi.Selected = false
			pi.Status = "limit"
			pi.Reason = "超过任务文件数上限"
			plan.Items = append(plan.Items, pi)
			continue
		}
		if rule.MaxTotalSize > 0 && bytes+m.Size > rule.MaxTotalSize {
			pi.Selected = false
			pi.Status = "limit"
			pi.Reason = "超过任务总大小上限"
			plan.Items = append(plan.Items, pi)
			continue
		}
		if existingValid(m, target) {
			pi.Existing = true
			pi.Selected = false
			pi.Status = "existing"
			pi.Reason = "已有完整文件"
			plan.ExistingFiles++
			plan.Items = append(plan.Items, pi)
			continue
		}
		included++
		bytes += m.Size
		plan.SelectedFiles++
		plan.SelectedBytes += m.Size
		plan.Items = append(plan.Items, pi)
	}
	return plan, nil
}

func rejectReason(m domain.Media, r domain.Rule) string {
	if !r.From.IsZero() && m.Date.Before(r.From) {
		return "早于开始时间"
	}
	if !r.To.IsZero() {
		end := r.To
		if end.Hour() == 0 && end.Minute() == 0 && end.Second() == 0 {
			end = end.Add(24*time.Hour - time.Nanosecond)
		}
		if m.Date.After(end) {
			return "晚于结束时间"
		}
	}
	id, _ := strconv.ParseInt(m.MessageID, 10, 64)
	if r.MinMessageID > 0 && id < r.MinMessageID {
		return "消息 ID 小于下限"
	}
	if r.MaxMessageID > 0 && id > r.MaxMessageID {
		return "消息 ID 大于上限"
	}
	if len(r.Kinds) > 0 && !containsFold(r.Kinds, m.Kind) {
		return "媒体类型不匹配"
	}
	ext := strings.TrimPrefix(strings.ToLower(m.Extension), ".")
	if len(r.IncludeExt) > 0 && !containsFold(r.IncludeExt, ext) {
		return "扩展名不在包含列表"
	}
	if containsFold(r.ExcludeExt, ext) {
		return "扩展名在排除列表"
	}
	hay := strings.ToLower(m.FileName + "\n" + m.Caption)
	if r.IncludeKeyword != "" && !strings.Contains(hay, strings.ToLower(r.IncludeKeyword)) {
		return "不含指定关键词"
	}
	if r.ExcludeKeyword != "" && strings.Contains(hay, strings.ToLower(r.ExcludeKeyword)) {
		return "含排除关键词"
	}
	if r.MinFileSize > 0 && m.Size < r.MinFileSize {
		return "文件小于最小值"
	}
	if r.MaxFileSize > 0 && m.Size > r.MaxFileSize {
		return "文件大于最大值"
	}
	return ""
}
func containsFold(v []string, s string) bool {
	for _, x := range v {
		if strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(x), "."), strings.TrimPrefix(strings.TrimSpace(s), ".")) {
			return true
		}
	}
	return false
}
func existingValid(m domain.Media, target string) bool {
	candidates := []string{m.LocalPath, target}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() == m.Size {
			return true
		}
	}
	return false
}
func location(name string) *time.Location {
	if name != "" {
		if l, err := time.LoadLocation(name); err == nil {
			return l
		}
	}
	return time.Local
}

func ValidateRule(r domain.Rule) error {
	if r.AccountID == "" || r.ChatID == "" {
		return fmt.Errorf("accountId and chatId are required")
	}
	if r.RootDir == "" {
		return fmt.Errorf("rootDir is required")
	}
	if len(r.IncludeExt) > 0 && len(r.ExcludeExt) > 0 {
		return fmt.Errorf("includeExt and excludeExt cannot both be set")
	}
	if r.MaxFileSize > 0 && r.MinFileSize > r.MaxFileSize {
		return fmt.Errorf("minFileSize cannot exceed maxFileSize")
	}
	return nil
}
