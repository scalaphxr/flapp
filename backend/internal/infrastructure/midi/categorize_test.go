package midi

import (
	"testing"
)

func TestCategorizeByName(t *testing.T) {
	cases := []struct {
		chanName string
		wantCat  MidiCategory
		wantSrc  DecisionSource
	}{
		{"808 Sub", Cat808Bass, SrcName},
		{"Kick Layer", CatKick, SrcName},
		{"Snare 2", CatSnare, SrcName},
		{"HiHat closed", CatHiHat, SrcName},
		{"Open Hat OH", CatOpenHat, SrcName},
		{"Clap 1", CatClap, SrcName},
		{"Perc Shaker", CatPerc, SrcName},
		{"drum machine", CatDrums, SrcName},
		{"Lead Synth", CatMelody, SrcName},
		{"Melody", CatMelody, SrcName},
		{"Piano Chords", CatMelody, SrcName},
		{"Chord Stab", CatMelody, SrcName},
		{"Pluck Lead", CatMelody, SrcName},
		{"Bell Melody", CatMelody, SrcName},
		{"Pad Atmosphere", CatMelody, SrcName},
		{"Arp Synth", CatMelody, SrcName},
		{"FX Riser", CatFX, SrcName},
		{"Sweep", CatFX, SrcName},
	}

	for _, c := range cases {
		cat, src := Categorize(c.chanName, "", "", nil, 96)
		if cat != c.wantCat {
			t.Errorf("Categorize(%q): cat=%q want=%q", c.chanName, cat, c.wantCat)
		}
		if src != c.wantSrc {
			t.Errorf("Categorize(%q): src=%q want=%q", c.chanName, src, c.wantSrc)
		}
	}
}

func TestCategorizeByNameDrumSubtypes(t *testing.T) {
	cases := []struct {
		chanName string
		want     MidiCategory
	}{
		{"kick hard", CatKick},
		{"snare tight", CatSnare},
		{"clap dry", CatClap},
		{"hihat 16th", CatHiHat},
		{"open hat long", CatOpenHat},
		{"percussion tom", CatPerc},
		{"cymbal crash", CatOpenHat},
	}
	for _, c := range cases {
		cat, _ := Categorize(c.chanName, "", "", nil, 96)
		if cat != c.want {
			t.Errorf("drum subtype %q: got %q, want %q", c.chanName, cat, c.want)
		}
	}
}

func TestCategorizeBySample(t *testing.T) {
	cat, src := Categorize("Sampler #3", `C:\Samples\808\deep_808.wav`, "", nil, 96)
	if cat != Cat808Bass {
		t.Errorf("cat=%q want 808/Bass (via sample path)", cat)
	}
	if src != SrcSample {
		t.Errorf("src=%q want 'sample'", src)
	}

	cat2, src2 := Categorize("Slot 1", `D:\Packs\Kicks\kick_hard.wav`, "", nil, 96)
	if cat2 != CatKick {
		t.Errorf("cat=%q want Kick (via sample kick path)", cat2)
	}
	if src2 != SrcSample {
		t.Errorf("src=%q want 'sample'", src2)
	}

	cat3, _ := Categorize("Sampler", `C:\Drums\snare_crack.wav`, "", nil, 96)
	if cat3 != CatSnare {
		t.Errorf("cat=%q want Snare (via sample snare path)", cat3)
	}
}

func TestCategorizeByNotes808(t *testing.T) {
	// Low pitch + monophonic + long notes → 808/Bass.
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 480, Key: 24, Velocity: 100},
		{PositionTicks: 480, LengthTicks: 480, Key: 28, Velocity: 100},
		{PositionTicks: 960, LengthTicks: 960, Key: 26, Velocity: 100},
	}
	cat, src := Categorize("Sampler", "", "", notes, 96)
	if cat != Cat808Bass {
		t.Errorf("cat=%q want 808/Bass (by notes)", cat)
	}
	if src != SrcNotes {
		t.Errorf("src=%q want 'notes'", src)
	}
}

func TestCategorizeByNotesDrums(t *testing.T) {
	// Narrow range + many short notes at C2-C#2 → not a melody.
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 48, Key: 36, Velocity: 100},
		{PositionTicks: 96, LengthTicks: 48, Key: 36, Velocity: 100},
		{PositionTicks: 192, LengthTicks: 48, Key: 37, Velocity: 90},
		{PositionTicks: 288, LengthTicks: 48, Key: 36, Velocity: 100},
	}
	cat, _ := Categorize("Sampler", "", "", notes, 96)
	if cat == CatMelody {
		t.Errorf("cat=%q, short low-range notes should not be Melody", cat)
	}
}

func TestCategorizeByNotesMelody(t *testing.T) {
	// Wide tonal range → Melody.
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 240, Key: 60, Velocity: 100},
		{PositionTicks: 240, LengthTicks: 240, Key: 67, Velocity: 100},
		{PositionTicks: 480, LengthTicks: 240, Key: 72, Velocity: 100},
		{PositionTicks: 720, LengthTicks: 240, Key: 69, Velocity: 100},
	}
	cat, _ := Categorize("Sampler", "", "", notes, 96)
	if cat != CatMelody {
		t.Errorf("cat=%q want Melody (by notes, wide range)", cat)
	}
}

func TestCategorizeNoDefault808(t *testing.T) {
	// Mid-register notes must NOT default to 808/Bass.
	notes := []NoteEvent{
		{PositionTicks: 0, LengthTicks: 240, Key: 48, Velocity: 100},
		{PositionTicks: 240, LengthTicks: 240, Key: 52, Velocity: 90},
	}
	cat, _ := Categorize("Sampler", "", "", notes, 96)
	if cat == Cat808Bass {
		t.Errorf("mid-range notes must NOT default to 808/Bass, got %q", cat)
	}
}
