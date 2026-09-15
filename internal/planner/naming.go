package planner

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

const DefaultTemplate = `{{.AccountName}}/{{.ChatName}}/{{date .Date "2006-01-02"}}-{{.MessageID}}-{{.OriginalName}}`

type TemplateData struct {
	AccountName  string
	ChatName     string
	ChatID       string
	MessageID    string
	MediaID      string
	Kind         string
	Date         time.Time
	OriginalName string
	BaseName     string
	Extension    string
}

var (
	invalidChars  = regexp.MustCompile(`[<>:"|?*\x00-\x1f]`)
	reservedName  = regexp.MustCompile(`(?i)^(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$`)
	templateCache sync.Map
)

func Render(root, source string, data TemplateData) (string, error) {
	if strings.TrimSpace(source) == "" {
		source = DefaultTemplate
	}
	cached, ok := templateCache.Load(source)
	var t *template.Template
	var err error
	if ok {
		t = cached.(*template.Template)
	} else {
		t, err = template.New("path").Funcs(template.FuncMap{
			"date":    func(v time.Time, layout string) string { return v.Format(layout) },
			"lower":   strings.ToLower,
			"upper":   strings.ToUpper,
			"replace": strings.ReplaceAll,
		}).Option("missingkey=error").Parse(source)
		if err != nil {
			return "", fmt.Errorf("parse filename template: %w", err)
		}
		templateCache.Store(source, t)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render filename template: %w", err)
	}
	raw := strings.ReplaceAll(buf.String(), "\\", "/")
	parts := strings.Split(raw, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		part = cleanSegment(part)
		if part == "" || part == "." || part == ".." {
			continue
		}
		clean = append(clean, part)
	}
	if len(clean) == 0 {
		return "", fmt.Errorf("filename template produced an empty path")
	}
	path := filepath.Join(append([]string{root}, clean...)...)
	rootAbs, _ := filepath.Abs(root)
	pathAbs, _ := filepath.Abs(path)
	if pathAbs != rootAbs && !strings.HasPrefix(strings.ToLower(pathAbs), strings.ToLower(rootAbs)+string(filepath.Separator)) {
		return "", fmt.Errorf("filename template escapes the download root")
	}
	if len([]rune(pathAbs)) > 240 {
		ext := filepath.Ext(pathAbs)
		base := strings.TrimSuffix(filepath.Base(pathAbs), ext)
		dir := filepath.Dir(pathAbs)
		max := 240 - len([]rune(dir)) - len([]rune(ext)) - 1
		if max < 8 {
			return "", fmt.Errorf("download root is too long")
		}
		baseRunes := []rune(base)
		if len(baseRunes) > max {
			base = string(baseRunes[:max])
		}
		pathAbs = filepath.Join(dir, base+ext)
	}
	return pathAbs, nil
}

func cleanSegment(v string) string {
	v = invalidChars.ReplaceAllString(strings.TrimSpace(v), "_")
	v = strings.TrimRight(v, ". ")
	if reservedName.MatchString(v) {
		v = "_" + v
	}
	if v == "" {
		return "_"
	}
	return v
}
