package usecase

import (
	"testing"

	"github.com/flapp/core/internal/domain"
)

func TestRenameTransforms(t *testing.T) {
	smp := &domain.Sample{Name: "01_Dark Trap Loop 02.wav", BPM: 140, KeyName: "Am", Category: domain.CatDrumLoop, Tags: []string{"dark"}}

	cases := []struct {
		name string
		ops  []RenameOp
		want string
	}{
		{"upper", []RenameOp{{Type: OpUpper}}, "01_DARK TRAP LOOP 02.wav"},
		{"lower", []RenameOp{{Type: OpLower}}, "01_dark trap loop 02.wav"},
		{"title", []RenameOp{{Type: OpLower}, {Type: OpTitle}}, "01_Dark Trap Loop 02.wav"},
		{"strip leading", []RenameOp{{Type: OpStripLeadingNum}}, "Dark Trap Loop 02.wav"},
		{"strip trailing", []RenameOp{{Type: OpStripTrailingNum}}, "01_Dark Trap Loop.wav"},
		{"prefix", []RenameOp{{Type: OpPrefix, Text: "MyKit_"}}, "MyKit_01_Dark Trap Loop 02.wav"},
		{"suffix", []RenameOp{{Type: OpSuffix, Text: "_DRY"}}, "01_Dark Trap Loop 02_DRY.wav"},
		{"replace", []RenameOp{{Type: OpReplace, From: "Trap", To: "Drill"}}, "01_Dark Drill Loop 02.wav"},
		{"regex", []RenameOp{{Type: OpRegexReplace, From: `\d+`, To: "#"}}, "#_Dark Trap Loop #.wav"},
		{"strip both then trim", []RenameOp{{Type: OpStripLeadingNum}, {Type: OpStripTrailingNum}, {Type: OpTrim}}, "Dark Trap Loop.wav"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := applyRename(smp, RenameSpec{Ops: c.ops})
			if got != c.want {
				t.Errorf("applyRename = %q, want %q", got, c.want)
			}
		})
	}
}

func TestRenameRemoveSpecial(t *testing.T) {
	smp := &domain.Sample{Name: "Kick!!! @#$ (final).wav"}
	got := applyRename(smp, RenameSpec{Ops: []RenameOp{{Type: OpRemoveSpecial}}})
	if got != "Kick final.wav" {
		t.Errorf("remove special = %q, want %q", got, "Kick final.wav")
	}
}

func TestInsertAroundBPM(t *testing.T) {
	withBPM := &domain.Sample{Name: "Melody 140bpm.wav"}
	got := applyRename(withBPM, RenameSpec{Ops: []RenameOp{{Type: OpInsertBeforeBPM, Text: "Dark"}}})
	if got != "Melody Dark 140bpm.wav" {
		t.Errorf("before bpm = %q", got)
	}
	got = applyRename(withBPM, RenameSpec{Ops: []RenameOp{{Type: OpInsertAfterBPM, Text: "Am"}}})
	if got != "Melody 140bpm Am.wav" {
		t.Errorf("after bpm = %q", got)
	}

	noBPM := &domain.Sample{Name: "Melody.wav", BPM: 150}
	got = applyRename(noBPM, RenameSpec{Ops: []RenameOp{{Type: OpInsertAfterBPM, Text: "X"}}})
	if got != "Melody 150BPM X.wav" {
		t.Errorf("after bpm fallback = %q", got)
	}
}

func TestSmartMarketingName(t *testing.T) {
	smp := &domain.Sample{Name: "raw_808_thing.wav", Category: domain.Cat808, BPM: 140, KeyName: "Am", Tags: []string{"dark", "trap"}}
	got := applyRename(smp, RenameSpec{Ops: []RenameOp{{Type: OpSmartMarketing}}})
	want := "Dark 808 - 140BPM - Am.wav"
	if got != want {
		t.Errorf("smart = %q, want %q", got, want)
	}
}

func TestSmartSearchParse(t *testing.T) {
	s := NewSmartSearchService(nil)

	q, interp := s.Parse("тёмные агрессивные 808 на 140 bpm")
	if len(q.Categories) != 1 || q.Categories[0] != domain.Cat808 {
		t.Errorf("categories = %v, want [808]", q.Categories)
	}
	if !containsStr(interp.Tags, "dark") || !containsStr(interp.Tags, "aggressive") {
		t.Errorf("tags = %v, want dark+aggressive", interp.Tags)
	}
	if q.MinBPM != 137 || q.MaxBPM != 143 {
		t.Errorf("bpm range = %d..%d, want 137..143", q.MinBPM, q.MaxBPM)
	}
	if q.Text != "" {
		t.Errorf("text should be empty when structured, got %q", q.Text)
	}

	q2, _ := s.Parse("dark melodic piano")
	if len(q2.Categories) != 1 || q2.Categories[0] != domain.CatLoop {
		t.Errorf("categories = %v, want [Loop]", q2.Categories)
	}

	// Unrecognised phrase -> full text fallback.
	q3, interp3 := s.Parse("zzz weird name")
	if q3.Text == "" || interp3.FreeText == "" {
		t.Errorf("expected free-text fallback, got %+v", q3)
	}
}

func containsStr(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
