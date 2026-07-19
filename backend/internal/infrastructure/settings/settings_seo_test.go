package settings

import (
	"strings"
	"testing"
)

func TestDefaultsSEO(t *testing.T) {
	d := Defaults()
	if !d.YtRosterAutoGrow {
		t.Error("YtRosterAutoGrow should default to true")
	}
	if d.YtKeywordRoster != "" {
		t.Errorf("YtKeywordRoster should default to empty, got %q", d.YtKeywordRoster)
	}
	for _, ph := range []string{"{keywords}", "{hashtags}", "{authors}", "{bpm}"} {
		if !strings.Contains(d.YtDescription, ph) {
			t.Errorf("default description missing %s", ph)
		}
	}
}
