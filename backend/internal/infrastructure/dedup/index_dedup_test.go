package dedup_test

import (
	"testing"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/dedup"
)

func TestIndex_ExactDedup_SHA256(t *testing.T) {
	idx := dedup.NewIndex(false, 0)
	idx.Add(1, "md5a", "sha256a", "", "kick.wav")
	idx.Add(2, "md5b", "sha256b", "", "snare.wav")

	if id, kind, _ := idx.Check("", "sha256a", ""); kind != dedup.Exact || id != 1 {
		t.Errorf("expected Exact/1, got %v/%d", kind, id)
	}
	if id, kind, _ := idx.Check("", "sha256_new", ""); kind != dedup.Unique || id != 0 {
		t.Errorf("expected Unique, got %v/%d", kind, id)
	}
}

func TestIndex_ExactDedup_MD5Fallback(t *testing.T) {
	idx := dedup.NewIndex(false, 0)
	idx.Add(5, "md5x", "", "", "hat.wav")

	if id, kind, _ := idx.Check("md5x", "", ""); kind != dedup.Exact || id != 5 {
		t.Errorf("expected Exact/5 via MD5, got %v/%d", kind, id)
	}
}

func TestIndex_NearDedup_BelowThreshold(t *testing.T) {
	idx := dedup.NewIndex(true, 80)
	// Use identical fingerprints (Hamming distance = 0) to test acoustic match.
	fp := "ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00"
	idx.Add(10, "md5z", "sha256z", fp, "perc.wav")

	if id, kind, _ := idx.Check("", "", fp); kind != dedup.Acoustic || id != 10 {
		t.Errorf("expected Acoustic/10, got %v/%d", kind, id)
	}
}

func TestIndex_NearDedup_Disabled(t *testing.T) {
	idx := dedup.NewIndex(false, 80) // deep = false
	fp := "ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00ff00"
	idx.Add(10, "", "", fp, "fx.wav")

	if _, kind, _ := idx.Check("", "", fp); kind != dedup.Unique {
		t.Error("acoustic dedup should be disabled when deep=false")
	}
}

func TestIndex_Seed(t *testing.T) {
	idx := dedup.NewIndex(false, 0)
	samples := []*domain.Sample{
		{ID: 1, MD5: "aaa", SHA256: "bbb"},
		{ID: 2, MD5: "ccc", SHA256: "ddd"},
	}
	idx.Seed(samples)

	if _, kind, _ := idx.Check("", "bbb", ""); kind != dedup.Exact {
		t.Error("seeded SHA256 should be detected as exact dup")
	}
	if _, kind, _ := idx.Check("ccc", "", ""); kind != dedup.Exact {
		t.Error("seeded MD5 should be detected as exact dup")
	}
}

func TestAccumulator(t *testing.T) {
	acc := dedup.NewAccumulator()
	acc.AddUnique(1000)
	acc.AddUnique(2000)
	acc.AddDuplicate(500)
	acc.NoteProject()
	acc.NoteArchive()

	s := acc.Stats()
	if s.UniqueFiles != 2 {
		t.Errorf("unique = %d, want 2", s.UniqueFiles)
	}
	if s.Duplicates != 1 {
		t.Errorf("duplicates = %d, want 1", s.Duplicates)
	}
	if s.FilesFound != 3 {
		t.Errorf("found = %d, want 3", s.FilesFound)
	}
	if s.BytesSaved != 500 {
		t.Errorf("saved = %d, want 500", s.BytesSaved)
	}
	if s.ProjectsParsed != 1 || s.ArchivesOpened != 1 {
		t.Error("project/archive counters wrong")
	}
}
