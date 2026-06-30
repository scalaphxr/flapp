package usecase

import (
	"testing"

	"github.com/flapp/core/internal/domain"
)

// buildTestProject строит минимальный domain.Project с тремя каналами:
//   0 — сэмплер со снэйром (SamplePath задан, IsEmptySampler=false);
//   1 — пустой сэмплер (IsEmptySampler=true);
//   2 — плагин Kontakt (Plugin задан, IsEmptySampler=false).
//
// В паттерне 0 есть по одной ноте на каждый из этих каналов.
func buildTestProject() *domain.Project {
	return &domain.Project{
		Name: "test_project",
		PPQ:  96,
		BPM:  120,
		Channels: []domain.FLPChannel{
			{Index: 0, Name: "Snare", Kind: "sampler", SamplePath: `C:\Samples\snare 01.wav`, IsEmptySampler: false},
			{Index: 1, Name: "Empty Slot", Kind: "sampler", SamplePath: "", IsEmptySampler: true},
			{Index: 2, Name: "Kontakt Strings", Kind: "plugin", Plugin: "Kontakt 7", IsEmptySampler: false},
		},
		Notes: []domain.FLPNote{
			{Position: 0, Length: 96, RackChan: 0, Key: 40, Velocity: 100, PatternIndex: 0, PatternName: "Pat 1"},
			{Position: 0, Length: 96, RackChan: 1, Key: 60, Velocity: 100, PatternIndex: 0, PatternName: "Pat 1"},
			{Position: 0, Length: 96, RackChan: 2, Key: 60, Velocity: 100, PatternIndex: 0, PatternName: "Pat 1"},
		},
	}
}

// TestProcessProjectFiltersEmptySamplers — основной тест фильтра.
func TestProcessProjectFiltersEmptySamplers(t *testing.T) {
	outDir := t.TempDir()
	svc := &MidiExtractService{} // processProject не использует extractor/parser

	// ignoreEmpty=true: пустой сэмплер (канал 1) должен быть отсеян.
	clips := svc.processProject(buildTestProject(), outDir, "flp", "test", true)
	if len(clips) != 2 {
		t.Errorf("ignoreEmpty=true: ожидалось 2 клипа, получено %d", len(clips))
	}
	for _, c := range clips {
		if c.ChannelIndex == 1 {
			t.Errorf("пустой сэмплер (channel 1) попал в клипы: %+v", c)
		}
	}

	// ignoreEmpty=false: все три группы должны дать клипы.
	clips2 := svc.processProject(buildTestProject(), outDir, "flp", "test", false)
	if len(clips2) != 3 {
		t.Errorf("ignoreEmpty=false: ожидалось 3 клипа, получено %d", len(clips2))
	}
}

// TestSamplerWithSoundIsNotFiltered — сэмплер с загруженным сэмплом не фильтруется.
func TestSamplerWithSoundIsNotFiltered(t *testing.T) {
	outDir := t.TempDir()
	svc := &MidiExtractService{}

	proj := &domain.Project{
		Name: "snare_test",
		PPQ:  96,
		BPM:  120,
		Channels: []domain.FLPChannel{
			{Index: 0, Name: "Snare", Kind: "sampler", SamplePath: `C:\Samples\snare 01.wav`, IsEmptySampler: false},
		},
		Notes: []domain.FLPNote{
			{Position: 0, Length: 96, RackChan: 0, Key: 40, Velocity: 100, PatternIndex: 0, PatternName: "Main"},
		},
	}

	clips := svc.processProject(proj, outDir, "flp", "test", true)
	if len(clips) != 1 {
		t.Errorf("сэмплер со звуком должен дать 1 клип, получено %d", len(clips))
	}
	if len(clips) == 1 && clips[0].SamplePath == "" {
		t.Errorf("клип должен иметь SamplePath, но он пустой")
	}
}

// TestPluginChannelIsNotFiltered — плагин (Kontakt) не фильтруется даже без SamplePath.
func TestPluginChannelIsNotFiltered(t *testing.T) {
	outDir := t.TempDir()
	svc := &MidiExtractService{}

	proj := &domain.Project{
		Name: "plugin_test",
		PPQ:  96,
		BPM:  120,
		Channels: []domain.FLPChannel{
			{Index: 0, Name: "Kontakt", Kind: "plugin", Plugin: "Kontakt 7", SamplePath: "", IsEmptySampler: false},
		},
		Notes: []domain.FLPNote{
			{Position: 0, Length: 96, RackChan: 0, Key: 60, Velocity: 100, PatternIndex: 0, PatternName: "Main"},
		},
	}

	clips := svc.processProject(proj, outDir, "flp", "test", true)
	if len(clips) != 1 {
		t.Errorf("плагин-канал должен дать 1 клип, получено %d", len(clips))
	}
}

// TestEmptySamplerWithNotesIsFiltered — пустой сэмплер с нотами в пианоролле → отфильтрован.
func TestEmptySamplerWithNotesIsFiltered(t *testing.T) {
	outDir := t.TempDir()
	svc := &MidiExtractService{}

	proj := &domain.Project{
		Name: "empty_filter_test",
		PPQ:  96,
		BPM:  120,
		Channels: []domain.FLPChannel{
			// Только пустой сэмплер — никаких звуков не загружено.
			{Index: 0, Name: "Phantom", Kind: "sampler", SamplePath: "", IsEmptySampler: true},
		},
		Notes: []domain.FLPNote{
			{Position: 0, Length: 96, RackChan: 0, Key: 60, Velocity: 100, PatternIndex: 0, PatternName: "Main"},
			{Position: 96, Length: 96, RackChan: 0, Key: 62, Velocity: 90, PatternIndex: 0, PatternName: "Main"},
		},
	}

	clips := svc.processProject(proj, outDir, "flp", "test", true)
	if len(clips) != 0 {
		t.Errorf("пустой сэмплер с нотами должен дать 0 клипов, получено %d", len(clips))
	}
}
