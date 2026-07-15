package classify

import (
	"testing"

	"github.com/flapp/core/internal/domain"
)

func TestClassifyByName(t *testing.T) {
	c := New()
	cases := []struct {
		name string
		want domain.Category
	}{
		// 808 / sub
		{"808 deep sub F.wav", domain.Cat808},
		{"sub_bass_C.wav", domain.Cat808},
		{"808 Bass.wav", domain.Cat808},
		{"808 Sub Slide.wav", domain.Cat808},
		// bd = TR-808 bass drum used as the sub instrument in this library (not Kick)
		{"bd_01.wav", domain.Cat808},
		{"BD_heavy.wav", domain.Cat808},
		{"BD 01.wav", domain.Cat808},
		// kick (no bd, pure kick)
		{"kick_punchy_01.wav", domain.CatKick},
		{"bassdrum_hard.wav", domain.CatKick},
		// kick vs 808 collisions: an explicit "kick" word wins over a bare "808" style tag
		{"808 Kick.wav", domain.CatKick},
		{"Kick 808 Deep.wav", domain.CatKick},
		{"Trap Kick 808 Bright.wav", domain.CatKick},
		{"Kick_BD_808.wav", domain.CatKick},
		// snare — sn as abbreviation
		{"snare_01.wav", domain.CatSnare},
		{"sn_tight.wav", domain.CatSnare},   // sn_ prefix
		{"SN.wav", domain.CatSnare},          // exact "sn.wav" → sn.
		{"sn 01.wav", domain.CatSnare},       // sn as word (space after)
		{"drum_sn.wav", domain.CatSnare},     // sn at end after _
		{"sn-crispy.wav", domain.CatSnare},   // sn- prefix
		// clap
		{"clap_layered.wav", domain.CatClap},
		{"cl_01.wav", domain.CatClap},        // cl as word
		{"rimshot_crack.wav", domain.CatClap},
		// open hat / cymbal
		{"open_hat_long.wav", domain.CatOpenHat},
		{"ride_warm.wav", domain.CatOpenHat},
		{"crash_heavy.wav", domain.CatOpenHat},
		{"oh_01.wav", domain.CatOpenHat},     // oh_ prefix
		{"perc_oh.wav", domain.CatOpenHat},   // _oh suffix → nameRules "_oh."? no → abbreviationRule "oh"
		{"OH.wav", domain.CatOpenHat},        // oh as word
		// closed hi-hat — hh abbreviation and hat as word
		{"hh_closed.wav", domain.CatHiHat},   // hh as word
		{"hh01.wav", domain.CatHiHat},        // hh as word (digit after)
		{"hat_01.wav", domain.CatHiHat},      // hat as word
		{"chh_01.wav", domain.CatHiHat},      // chh in nameRules
		// ch = chant/vox (user convention, NOT hi-hat)
		{"ch_vox.wav", domain.CatVox},        // ch as word at start
		{"CH.wav", domain.CatVox},            // ch as word
		{"ch_tight.wav", domain.CatVox},      // ch as word
		// vox / vocals
		{"vox_adlib_yeah.wav", domain.CatVox},
		{"what_adlib.wav", domain.CatVox},  // "what" word → vox
		// loops
		{"top_drum_loop_140.wav", domain.CatDrumLoop},
		{"piano_loop_140.wav", domain.CatLoop},
		{"melodic loop Cmin.wav", domain.CatLoop},
		// fx
		{"riser_long_tail.wav", domain.CatFX},
		{"project_bounce.mid", domain.CatFX},
	}
	for _, tc := range cases {
		got, fromAudio := c.Classify(tc.name, "", domain.AudioFeatures{})
		if got != tc.want {
			t.Errorf("Classify(%q) = %q, want %q", tc.name, got, tc.want)
		}
		if fromAudio {
			t.Errorf("Classify(%q) should be name-based, not audio", tc.name)
		}
	}
}

func TestWordBoundaryMatching(t *testing.T) {
	// "hat" must NOT match inside "what"
	if containsWord("what.wav", "hat") {
		t.Error(`"what.wav" should NOT match word "hat"`)
	}
	// "hat" MUST match standalone
	if !containsWord("hat_01.wav", "hat") {
		t.Error(`"hat_01.wav" should match word "hat"`)
	}
	if !containsWord("drum hat.wav", "hat") {
		t.Error(`"drum hat.wav" should match word "hat"`)
	}
	// "sn" must NOT match inside "snare"
	if containsWord("snare_01.wav", "sn") {
		t.Error(`"snare_01.wav" should NOT match word "sn" (caught by "snare" rule first, but boundary test)`)
	}
	// "sn" as standalone
	if !containsWord("sn 01.wav", "sn") {
		t.Error(`"sn 01.wav" should match word "sn"`)
	}
	if !containsWord("sn.wav", "sn") {
		t.Error(`"sn.wav" should match word "sn"`)
	}
	// "bd" standalone
	if !containsWord("bd_01.wav", "bd") {
		t.Error(`"bd_01.wav" should match word "bd"`)
	}
	if containsWord("abduction.wav", "bd") {
		t.Error(`"abduction.wav" should NOT match word "bd"`)
	}
	// "ch" standalone vs inside "chh"
	if containsWord("chh_01.wav", "ch") {
		t.Error(`"chh_01.wav" should NOT match word "ch" (followed by "h")`)
	}
	if !containsWord("ch_01.wav", "ch") {
		t.Error(`"ch_01.wav" should match word "ch"`)
	}
	// "oh" standalone
	if !containsWord("perc_oh.wav", "oh") {
		t.Error(`"perc_oh.wav" should match word "oh"`)
	}
	if containsWord("another.wav", "oh") {
		t.Error(`"another.wav" should NOT match word "oh"`)
	}
}

func TestClassifyByAudioFallback(t *testing.T) {
	c := New()

	// deep sustained low end → 808
	sub := domain.AudioFeatures{Analyzed: true, DurationSeconds: 1.5, SpectralCentroid: 120, LowEnergyRatio: 0.75, ZeroCrossRate: 0.01, AttackTime: 0.05}
	if got, fromAudio := c.Classify("unknown_xx.wav", "", sub); got != domain.Cat808 || !fromAudio {
		t.Errorf("sub fallback = %q (audio=%v), want 808 (audio=true)", got, fromAudio)
	}

	// bright short noisy → hihat
	hat := domain.AudioFeatures{Analyzed: true, DurationSeconds: 0.08, SpectralCentroid: 9000, HighEnergyRatio: 0.6, ZeroCrossRate: 0.45, AttackTime: 0.001}
	if got, fromAudio := c.Classify("xx.wav", "", hat); got != domain.CatHiHat || !fromAudio {
		t.Errorf("hat fallback = %q (audio=%v), want Hi-Hat (audio=true)", got, fromAudio)
	}

	// long open hat (2 s sustain) must NOT become a drum loop
	openhat := domain.AudioFeatures{Analyzed: true, DurationSeconds: 2.2, SpectralCentroid: 8000, ZeroCrossRate: 0.35, HighEnergyRatio: 0.5}
	if got, _ := c.Classify("unknown.wav", "", openhat); got != domain.CatOpenHat {
		t.Errorf("long open hat fallback = %q, want Open Hat", got)
	}

	// genuinely long + noisy → drum loop (>= 6 s)
	dloop := domain.AudioFeatures{Analyzed: true, DurationSeconds: 8.0, SpectralCentroid: 4000, ZeroCrossRate: 0.22}
	if got, _ := c.Classify("xx.wav", "", dloop); got != domain.CatDrumLoop {
		t.Errorf("drum loop fallback = %q, want Drum Loop", got)
	}

	// medium-length file (3 s) with no clear features → must NOT be Loop
	mid := domain.AudioFeatures{Analyzed: true, DurationSeconds: 3.0, SpectralCentroid: 1800, ZeroCrossRate: 0.05}
	if got, _ := c.Classify("xx.wav", "", mid); got == domain.CatLoop {
		t.Errorf("medium file should not be Loop, got %q", got)
	}
}

func TestShortInstrumentNameNotLoop(t *testing.T) {
	c := New()
	// 1-second synth stab should NOT be CatLoop
	stab := domain.AudioFeatures{Analyzed: true, DurationSeconds: 1.0, SpectralCentroid: 3000, ZeroCrossRate: 0.10}
	got, _ := c.Classify("synth_stab_01.wav", "", stab)
	if got == domain.CatLoop {
		t.Errorf("synth_stab_01.wav (1s) classified as Loop, want FX or other")
	}

	// 8-second pad → Loop
	got2, _ := c.Classify("synth_pad_long.wav", "", domain.AudioFeatures{Analyzed: true, DurationSeconds: 8.0, SpectralCentroid: 2500, ZeroCrossRate: 0.05})
	if got2 != domain.CatLoop {
		t.Errorf("synth_pad_long.wav (8s) = %q, want Loop", got2)
	}
}

func TestLongOneShotNotLoop(t *testing.T) {
	c := New()

	// A long sub/bass note (single onset, multi-second release tail) must NOT
	// become a Loop just because duration >= 4s. Duration alone used to be the
	// only guard, so a released 808/bass one-shot with a long tail (very common
	// — trap 808s often ring out for 4-8s) was force-classified as Loop.
	longOneShot := domain.AudioFeatures{
		Analyzed: true, DurationSeconds: 6.0, OnsetCount: 1,
		SpectralCentroid: 150, LowEnergyRatio: 0.7, SubBassRatio: 0.4,
	}
	got, _ := c.Classify("reese_bassline_G.wav", "", longOneShot)
	if got == domain.CatLoop {
		t.Errorf("reese_bassline_G.wav (6s, 1 onset) classified as Loop, want 808/Kick/FX")
	}

	// Same keyword family, but genuinely repeating (many onsets) → stays Loop.
	realLoop := domain.AudioFeatures{
		Analyzed: true, DurationSeconds: 6.0, OnsetCount: 12,
		SpectralCentroid: 1200, ZeroCrossRate: 0.1,
	}
	got2, _ := c.Classify("reese_bassline_G.wav", "", realLoop)
	if got2 != domain.CatLoop {
		t.Errorf("reese_bassline_G.wav (6s, 12 onsets) = %q, want Loop", got2)
	}

	// A BPM tag in the name is itself a loop confirmation: it must survive the
	// one-shot safety net even when the audio profile alone looks ambiguous.
	bpmTagged := domain.AudioFeatures{Analyzed: true, DurationSeconds: 2.0, SpectralCentroid: 2000, ZeroCrossRate: 0.05}
	got3, _ := c.Classify("synth 140 bpm.wav", "", bpmTagged)
	if got3 != domain.CatLoop {
		t.Errorf("synth 140 bpm.wav (tagged, 2s) = %q, want Loop (BPM tag confirms loop)", got3)
	}
}

func TestFolderClassificationDeterministic(t *testing.T) {
	// Folder names that contain more than one category keyword as a substring
	// (e.g. "808 Kicks" contains both "808" and "kicks") must resolve the same
	// way every call. classifyByFolderPath used to range directly over a Go
	// map for the substring fallback — map iteration order is randomised on
	// every range, even within a single process — so this could silently pick
	// a different category from one call to the next.
	for i := 0; i < 20; i++ {
		cat, _, ok := ClassifyByName("Deep 01.wav", "Drums/808 Kicks/Deep 01.wav")
		if !ok || cat != domain.CatKick {
			t.Fatalf("run %d: folder '808 Kicks' = %q (ok=%v), want Kick", i, cat, ok)
		}
	}
}
