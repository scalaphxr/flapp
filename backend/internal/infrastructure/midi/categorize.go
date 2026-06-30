package midi

import (
	"math"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/classify"
)

// MidiCategory — строковый псевдоним для категории MIDI-клипа.
type MidiCategory = string

// DecisionSource — источник решения о категории.
type DecisionSource = string

const (
	Cat808Bass = "808/Bass"
	CatMelody  = "Melody"
	CatKick    = "Kick"
	CatSnare   = "Snare"
	CatClap    = "Clap"
	CatHiHat   = "Hi-Hat"
	CatOpenHat = "Open Hat"
	CatPerc    = "Perc"
	CatDrums   = "Drums"
	CatFX      = "FX"
	CatOther   = "Other"
)

const (
	SrcName   DecisionSource = "name"
	SrcSample DecisionSource = "sample"
	SrcNotes  DecisionSource = "notes"
)

var drumCatMap = map[domain.Category]MidiCategory{
	domain.CatKick:    CatKick,
	domain.CatSnare:   CatSnare,
	domain.CatClap:    CatClap,
	domain.CatHiHat:   CatHiHat,
	domain.CatOpenHat: CatOpenHat,
	domain.CatPerc:    CatPerc,
}

// Categorize определяет категорию MIDI-клипа.
// ppq — тиков на четверть из FLhd (0 → использует 96 по умолчанию).
func Categorize(chanName, samplePath, plugin string, notes []NoteEvent, ppq int) (MidiCategory, DecisionSource) {
	hay := strings.ToLower(chanName)
	if cat, ok := categorizeByName(hay, chanName, samplePath); ok {
		return cat, SrcName
	}

	sampleBase := strings.ToLower(strings.TrimSuffix(filepath.Base(samplePath), filepath.Ext(samplePath)))
	pluginHay := strings.ToLower(plugin)
	sampleHay := sampleBase + " " + pluginHay
	if cat, ok := categorizeByName(sampleHay, "", samplePath); ok {
		return cat, SrcSample
	}

	// Нотный анализ: барабанные VST (Battery, FPC, Kontakt) правильно получают
	// Kick/HiHat/etc. по паттерну нот. Для сэмплов (ваншотов) результат Melody
	// отбрасывается — категория ваншота определяется звуком, а не высотой нот.
	if len(notes) > 0 {
		if cat, ok := categorizeByNoteScores(notes, ppq); ok {
			if cat != CatMelody || samplePath == "" {
				return cat, SrcNotes
			}
			// samplePath != "" && cat == Melody: ваншот, продолжаем к следующим правилам
		}
	}

	// Чистый VST без сэмпла (Serum, Massive, Kontakt с мелодией…) → Melody.
	// Ваншоты (samplePath != "") сюда не доходят — они уже отфильтрованы выше.
	if plugin != "" && samplePath == "" {
		return CatMelody, SrcSample
	}

	return CatOther, SrcNotes
}

func categorizeByName(hay, chanName, samplePath string) (MidiCategory, bool) {
	for _, r := range nameRules {
		for _, term := range r.terms {
			if strings.Contains(hay, term) {
				if r.cat == CatDrums {
					return resolveDrumType(chanName, samplePath), true
				}
				return r.cat, true
			}
		}
	}
	return "", false
}

func resolveDrumType(chanName, samplePath string) MidiCategory {
	if domCat, ok := classify.ClassifyDrumByName(chanName, samplePath); ok {
		if midiCat, mapped := drumCatMap[domCat]; mapped {
			return midiCat
		}
	}
	return CatDrums
}

type midiRule struct {
	cat   MidiCategory
	terms []string
}

var nameRules = []midiRule{
	{CatDrums, []string{
		"kick", "kik", "kck", "bassdrum", "bass drum",
		"snare", "clap", "rimshot",
		"hihat", "hi-hat", "hi hat", "hi_hat",
		"openhat", "open hat", "open-hat",
		"cymbal", "crash", "ride",
		"perc", "percussion", "tom",
		"shaker", "cowbell",
		"drum", "hat", "hh_", "_hh", "hh-",
		" sn ", "_sn_", "sn_",
		" kd", "_kd",
	}},

	{Cat808Bass, []string{
		"808", "sub bass", "subbass", "sub_bass", "808bass",
		"bassline", "bass line", "bass_line",
		"reese", "wobble",
		" bass", "_bass", "-bass", "bass_",
		" sub", "_sub", "-sub",
		"_bd_", " bd ", "-bd-",
	}},

	{CatFX, []string{
		" fx", "_fx", "-fx", "sfx", "effect", "effects",
		"riser", "sweep", "transition", "impact",
		"automation", "glitch", "noise",
		"rev ", "_rev", " rev",
	}},

	{CatMelody, []string{
		"lead", "melody", "melodic", "topline", "top line", "synth lead", "main synth",
		"pluck", "plk",
		"guitar", "gtr", "acoustic",
		"bell", "bells", "glock", "marimba", "kalimba", "koto", "sitar", "banjo", "harp",
		" pad", "_pad", "-pad", "pad_",
		"atmosphere", "atmos", "texture", "ambient", "ambience", "drone",
		"string", "strings", "choir",
		"arp ", " arp", "_arp", "-arp", "arp_", "arpeggio", "arpegg",
		"chord", "chords", "harmony",
		"piano", "rhodes", "epiano", "e-piano", "wurli",
		"keys", "stab", "progression",
		"synth", "vst", "osc",
	}},
}

// categorizeByNoteScores uses a scoring approach for note-based classification.
func categorizeByNoteScores(notes []NoteEvent, ppq int) (MidiCategory, bool) {
	if len(notes) == 0 {
		return CatOther, false
	}
	if ppq <= 0 {
		ppq = 96
	}

	var minKey, maxKey uint8 = 127, 0
	var totalLen, totalVel uint64
	var minVel, maxVel uint8 = 127, 0
	var spanTicks uint32

	for _, n := range notes {
		if n.Key < minKey {
			minKey = n.Key
		}
		if n.Key > maxKey {
			maxKey = n.Key
		}
		if n.Velocity < minVel {
			minVel = n.Velocity
		}
		if n.Velocity > maxVel {
			maxVel = n.Velocity
		}
		totalLen += uint64(n.LengthTicks)
		totalVel += uint64(n.Velocity)
		end := n.PositionTicks + n.LengthTicks
		if end > spanTicks {
			spanTicks = end
		}
	}

	keyRange := int(maxKey) - int(minKey)
	avgLenTicks := totalLen / uint64(len(notes))
	avgVel := float64(totalVel) / float64(len(notes))
	velRange := int(maxVel) - int(minVel)
	maxPoly := countMaxPolyphony(notes)
	avgLenBeats := float64(avgLenTicks) / float64(ppq)
	barsSpanned := math.Max(0.4, float64(spanTicks)/float64(ppq*4))
	density := float64(len(notes)) / barsSpanned

	sc := map[string]float64{}

	// ── 808/Bass ──────────────────────────────────────────────────────────────
	if minKey < 36 {
		sc[Cat808Bass] += 3.0
	}
	if maxKey < 48 {
		sc[Cat808Bass] += 2.0
	}
	if maxPoly <= 1 && maxKey < 52 {
		sc[Cat808Bass] += 2.0
	}
	if avgLenBeats > 1.0 {
		sc[Cat808Bass] += 2.0
	}
	if velRange < 20 {
		sc[Cat808Bass] += 1.0
	}

	// ── Hi-Hat ────────────────────────────────────────────────────────────────
	if keyRange <= 2 {
		sc[CatHiHat] += 2.5
	}
	if density > 8.0 {
		sc[CatHiHat] += 2.5
	}
	if density > 16.0 {
		sc[CatHiHat] += 2.0
	}
	if avgLenBeats < 0.12 {
		sc[CatHiHat] += 1.5
	}
	if velRange < 35 && density > 6 {
		sc[CatHiHat] += 1.5
	}
	if keyRange == 0 {
		sc[CatHiHat] += 1.5
	}

	// ── Melody ────────────────────────────────────────────────────────────────
	if keyRange > 12 {
		sc[CatMelody] += 2.0
	}
	if keyRange > 24 {
		sc[CatMelody] += 2.0
	}
	if maxPoly >= 2 {
		sc[CatMelody] += 2.0
	}
	if maxPoly >= 3 {
		sc[CatMelody] += 1.0
	}
	if velRange > 40 {
		sc[CatMelody] += 1.5
	}
	if avgLenBeats > 0.25 {
		sc[CatMelody] += 1.0
	}
	if minKey >= 48 && maxKey <= 84 {
		sc[CatMelody] += 2.0
	}
	if avgVel < 105 {
		sc[CatMelody] += 1.0
	}

	// Anti-signals for Melody.
	if minKey < 36 {
		sc[CatMelody] -= 3.0
	}
	if density > 12 {
		sc[CatMelody] -= 1.5
	}
	if keyRange <= 2 {
		sc[CatMelody] -= 2.0
	}

	// Find winner (min threshold 3.0).
	var bestCat string
	var bestScore float64
	for cat, s := range sc {
		if s > bestScore {
			bestScore = s
			bestCat = cat
		}
	}
	if bestScore < 3.0 {
		return CatOther, false
	}
	return bestCat, true
}

func countMaxPolyphony(notes []NoteEvent) int {
	type edge struct {
		tick    uint32
		isStart bool
	}
	edges := make([]edge, 0, len(notes)*2)
	for _, n := range notes {
		edges = append(edges, edge{n.PositionTicks, true})
		edges = append(edges, edge{n.PositionTicks + n.LengthTicks, false})
	}
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].tick != edges[j].tick {
			return edges[i].tick < edges[j].tick
		}
		return !edges[i].isStart && edges[j].isStart
	})
	maxPoly, cur := 0, 0
	for _, e := range edges {
		if e.isStart {
			cur++
			if cur > maxPoly {
				maxPoly = cur
			}
		} else {
			cur--
		}
	}
	return maxPoly
}
