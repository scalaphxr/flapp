package storage

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/flapp/core/internal/domain"
)

func TestSearchTagsAnyMatchRanked(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	mustAdd := func(name string, tags []string) int64 {
		id, err := store.Samples.Upsert(ctx, &domain.Sample{
			Name: name, Path: "/tmp/" + name, Ext: "wav",
			Category: domain.Cat808, Tags: tags,
		})
		if err != nil {
			t.Fatalf("upsert %s: %v", name, err)
		}
		return id
	}

	both := mustAdd("both.wav", []string{"dark", "aggressive"})
	darkOnly := mustAdd("dark_only.wav", []string{"dark"})
	neither := mustAdd("neither.wav", []string{"bright"})
	_ = neither

	// Before the fix this required ALL tags (strict AND) and would return
	// only `both`. It must now also surface the partial match `darkOnly`,
	// with the fuller match ranked first.
	items, total, err := store.Samples.Search(ctx, domain.SearchQuery{
		Tags: []string{"dark", "aggressive"}, Limit: 50,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2 (both.wav + dark_only.wav)", total)
	}
	if len(items) != 2 || items[0].ID != both || items[1].ID != darkOnly {
		ids := make([]int64, len(items))
		for i, it := range items {
			ids[i] = it.ID
		}
		t.Fatalf("items = %v, want [%d(both, 2 tags), %d(darkOnly, 1 tag)] in that order", ids, both, darkOnly)
	}
}

func TestSearchDefaultTagAndOrderUnaffected(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	if _, err := store.Samples.Upsert(ctx, &domain.Sample{
		Name: "kick.wav", Path: "/tmp/kick.wav", Ext: "wav", Category: domain.CatKick,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// A plain query with no tags/text must still work (regression guard for
	// the sampleOrderBy signature change).
	items, total, err := store.Samples.Search(ctx, domain.SearchQuery{Limit: 50, Sort: "name", Order: "asc"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Name != "kick.wav" {
		t.Fatalf("unexpected result: total=%d items=%v", total, items)
	}
}
