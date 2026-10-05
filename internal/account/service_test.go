package account

import (
	"context"
	"github.com/local/tdl-gui/internal/store"
	"path/filepath"
	"testing"
)

func TestChineseDisplayNamesHaveDistinctNamespaces(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	service := New(st, nil)
	ctx := context.Background()
	a, err := service.Add(ctx, "个人账户", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := service.Add(ctx, "工作账户", "")
	if err != nil {
		t.Fatal(err)
	}
	if a.Namespace == b.Namespace || a.Namespace == "-" || b.Namespace == "-" {
		t.Fatal(a, b)
	}
	explicit, err := service.Add(ctx, "指定名称", "saved-session")
	if err != nil || explicit.Namespace != "saved-session" {
		t.Fatal(explicit, err)
	}
}
