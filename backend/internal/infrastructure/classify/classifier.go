package classify

import (
	"strings"

	"github.com/flapp/core/internal/domain"
)

// Classifier implements domain.Classifier.
type Classifier struct{}

func New() *Classifier { return &Classifier{} }

// Classify returns the category and whether it was decided from audio analysis.
func (c *Classifier) Classify(name, relPath string, f domain.AudioFeatures) (domain.Category, bool) {
	scores := make(map[domain.Category]float64)

	// Signal 1: name-based multi-pass (folder, dash-prefix, abbreviations, BPM, keywords).
	if cat, score, ok := ClassifyByName(name, relPath); ok {
		// Melodic instrument names (synth, pad, bass, stab…) on one-shots are NOT loops.
		// If audio is analyzed, cross-check before committing to Loop unless the name
		// itself already confirms a loop (explicit "loop" word or a BPM tag).
		if cat == domain.CatLoop && !hasLoopMarker(strings.ToLower(name+" "+relPath)) {
			// Short one-shot, OR a long sample with almost no onsets (e.g. a
			// released 808/bass note with a multi-second tail) — duration alone
			// mis-tags those as Loop, but a single onset is never a "repeating
			// pattern". A real loop repeats (onsets > 2) regardless of length.
			isOneShot := f.DurationSeconds < 4.0 || (f.OnsetCount > 0 && f.OnsetCount <= 2)
			if f.Analyzed && isOneShot {
				if audioCat, ok2 := classifyByAudio(f); ok2 && !audioCat.IsLoop() {
					return audioCat, true
				}
				if audioCat, ok2 := bestNonLoopAudioCategory(f); ok2 {
					return audioCat, true
				}
				// Audio analyzed but ambiguous — one-shot → FX, not Loop.
				return domain.CatFX, false
			}
		}
		// Имя весит в 4× тяжелее аудио: max аудио ~14, min имя 5×4=20.
		// Это предотвращает ситуацию «PERC → HiHat» из-за акустики.
		scores[cat] += score * 4.0
	}

	// Signal 2: suffix-word detection (e.g., "earthquake bass" → 808).
	if cat, ok := classifyBySuffixWord(name); ok {
		scores[cat] += 6.0
	}

	// Signal 3: audio features.
	// Use new score-based approach when extended features are present;
	// fall back to legacy hard-decision classifier for older analysed files.
	if f.Analyzed {
		if f.SpectralFlatness > 0 || f.OnsetCount > 0 {
			for cat, s := range audioScores(f) {
				scores[cat] += s
			}
		} else {
			if audioCat, ok := classifyByAudio(f); ok {
				scores[audioCat] += 8.0
			}
		}
	}

	// (No separate top-level duration heuristic here: audioScores already adds
	// its own dur>=4.0/dur>=8.0 → Loop bonus above when analysed with v2
	// features, which is true for virtually all files today. A second,
	// identical duration bonus here double-counted the same evidence and was
	// confirmed (via a confusion-matrix run against a 66k-file real drumkit
	// library) to be the main driver of 808/FX/Drum Loop samples with a long
	// tail being over-classified as Loop. The "nothing scored at all" case is
	// still covered by the duration-based fallback below.)

	// Find winner.
	var bestCat domain.Category
	var bestScore float64
	for cat, s := range scores {
		if s > bestScore {
			bestScore = s
			bestCat = cat
		}
	}
	if bestCat == "" || bestScore < 1.0 {
		if f.DurationSeconds >= 4.0 {
			return domain.CatLoop, f.Analyzed
		}
		return domain.CatFX, false
	}

	fromAudio := f.Analyzed && audioScores(f)[bestCat] > 0
	return bestCat, fromAudio
}

// hasLoopMarker reports whether the haystack contains an explicit indicator
// that the file is a looping phrase rather than a one-shot: an explicit loop
// word, or a BPM/[starter]/[phrase] tag (producers tag tempo on loopable
// content, rarely on single hits).
func hasLoopMarker(s string) bool {
	if strings.Contains(s, "loop") ||
		strings.Contains(s, "fill") ||
		strings.Contains(s, "groove") ||
		strings.Contains(s, "phrase") {
		return true
	}
	isLoop, _ := detectLoopByPattern(s)
	return isLoop
}

// bestNonLoopAudioCategory picks the highest-scoring category from the
// signal-level audioScores heuristics, excluding Loop/Drum Loop. Used when a
// name-based Loop guess has already been ruled out (short one-shot, or a long
// sample with too few onsets to be a repeating pattern) so the fallback
// mustn't re-derive Loop from duration alone the way the legacy
// classifyByAudio does.
func bestNonLoopAudioCategory(f domain.AudioFeatures) (domain.Category, bool) {
	var best domain.Category
	var bestScore float64
	for cat, s := range audioScores(f) {
		if cat.IsLoop() {
			continue
		}
		if s > bestScore {
			bestScore = s
			best = cat
		}
	}
	if best == "" || bestScore <= 0 {
		return "", false
	}
	return best, true
}

// audioScores returns per-category scores from signal-level features.
// All thresholds tuned for 44.1/48 kHz material at real drumkit scale.
func audioScores(f domain.AudioFeatures) map[domain.Category]float64 {
	s := make(map[domain.Category]float64)
	dur := f.DurationSeconds
	centroid := f.SpectralCentroid
	zcr := f.ZeroCrossRate
	lowR := f.LowEnergyRatio
	highR := f.HighEnergyRatio
	flat := f.SpectralFlatness
	crest := f.CrestFactor
	decay := f.DecayRate
	onsets := f.OnsetCount
	subBass := f.SubBassRatio
	fastAttack := f.AttackTime >= 0 && f.AttackTime < 0.025

	add := func(cat domain.Category, v float64) { s[cat] += v }

	// ── Hi-Hat ───────────────────────────────────────────────────────────────
	if centroid > 6000 {
		add(domain.CatHiHat, 2.0)
	}
	if centroid > 9000 {
		add(domain.CatHiHat, 2.0)
	}
	if flat > 0 && flat > 0.5 {
		add(domain.CatHiHat, 2.0)
	}
	if flat > 0 && flat > 0.7 {
		add(domain.CatHiHat, 2.0)
	}
	if zcr > 0.25 {
		add(domain.CatHiHat, 1.5)
	}
	if highR > 0.5 {
		add(domain.CatHiHat, 1.5)
	}
	// Real-library duration medians: Hi-Hat 0.19s vs Open Hat 1.12s (P10 0.36s) —
	// centroid/flatness/zcr are nearly identical between the two, so duration
	// is the actual separating signal. 0.15 sat below the Hi-Hat median itself
	// (missing over half of real Hi-Hats); 0.25 stays comfortably under Open
	// Hat's P10.
	if dur < 0.25 {
		add(domain.CatHiHat, 2.0)
	}
	if decay > 0 && decay < 0.08 {
		add(domain.CatHiHat, 1.5)
	}
	// Anti: sustained sounds are open hats / cymbals, not closed hats.
	if dur > 0.3 {
		add(domain.CatHiHat, -2.0)
	}

	// ── Open Hat / Cymbal ─────────────────────────────────────────────────────
	if centroid > 5000 && zcr > 0.18 && dur > 0.25 {
		add(domain.CatOpenHat, 3.0)
	}
	if flat > 0 && centroid > 5000 && flat > 0.3 {
		add(domain.CatOpenHat, 2.0)
	}
	if centroid > 5000 && dur > 0.5 && flat > 0 && flat > 0.3 {
		add(domain.CatOpenHat, 1.5)
	}
	if dur < 0.25 {
		add(domain.CatOpenHat, -1.5)
	}

	// ── Kick ──────────────────────────────────────────────────────────────────
	if fastAttack {
		add(domain.CatKick, 2.0)
	}
	if crest > 0 && crest > 8 {
		add(domain.CatKick, 2.0)
	}
	if crest > 0 && crest > 15 {
		add(domain.CatKick, 2.0)
	}
	if lowR > 0.40 && centroid < 700 {
		add(domain.CatKick, 2.5)
	}
	if decay > 0 && decay < 0.12 {
		add(domain.CatKick, 1.5)
	}
	if onsets > 0 && onsets == 1 {
		add(domain.CatKick, 1.0)
	}
	if dur < 0.8 {
		add(domain.CatKick, 1.0)
	}
	if subBass > 0 && subBass > 0.2 && centroid < 400 {
		add(domain.CatKick, 1.5)
	}
	if flat > 0 && flat > 0.55 {
		add(domain.CatKick, -2.5)
	}
	if zcr > 0.20 {
		add(domain.CatKick, -2.0)
	}

	// ── 808 / Sub ─────────────────────────────────────────────────────────────
	if subBass > 0 && subBass > 0.30 {
		add(domain.Cat808, 3.0)
	}
	if subBass > 0 && subBass > 0.50 {
		add(domain.Cat808, 2.0)
	}
	if centroid < 400 {
		add(domain.Cat808, 2.0)
	}
	if centroid < 250 {
		add(domain.Cat808, 2.0)
	}
	if lowR > 0.55 {
		add(domain.Cat808, 2.0)
	}
	if flat > 0 && flat < 0.15 && centroid < 300 {
		add(domain.Cat808, 2.0)
	}
	if dur > 0.4 {
		add(domain.Cat808, 1.0)
	}
	if decay > 0 && decay > 0.3 {
		add(domain.Cat808, 2.0)
	}
	if decay > 0 && decay > 0.5 {
		add(domain.Cat808, 1.0)
	}
	// Anti: fast decay + single onset → more kick-like.
	if (decay > 0 && decay < 0.10) && (onsets == 0 || onsets == 1) {
		add(domain.Cat808, -2.0)
	}
	if centroid > 800 {
		add(domain.Cat808, -1.0)
	}

	// ── Snare ─────────────────────────────────────────────────────────────────
	if centroid >= 1000 && centroid <= 5000 {
		add(domain.CatSnare, 1.5)
	}
	if zcr > 0.15 {
		add(domain.CatSnare, 1.0)
	}
	if flat > 0 && flat > 0.25 && flat < 0.65 {
		add(domain.CatSnare, 1.5)
	}
	if fastAttack {
		add(domain.CatSnare, 1.0)
	}
	if dur > 0.1 && dur < 0.8 {
		add(domain.CatSnare, 1.0)
	}

	// ── Clap ──────────────────────────────────────────────────────────────────
	if centroid >= 2000 && centroid <= 7000 {
		add(domain.CatClap, 1.0)
	}
	if flat > 0 && flat > 0.50 {
		add(domain.CatClap, 2.0)
	}
	if f.AttackTime > 0 && f.AttackTime < 0.005 {
		add(domain.CatClap, 2.5)
	}
	if dur < 0.20 {
		add(domain.CatClap, 1.0)
	}
	if dur < 0.10 {
		add(domain.CatClap, 1.0)
	}

	// ── Perc ──────────────────────────────────────────────────────────────────
	if centroid >= 400 && centroid <= 4000 && dur < 2.0 {
		add(domain.CatPerc, 2.0)
	}
	if crest > 0 && crest > 5 && dur < 1.5 {
		add(domain.CatPerc, 1.0)
	}

	// ── Vox ───────────────────────────────────────────────────────────────────
	if centroid >= 500 && centroid <= 3000 && flat > 0.05 && flat < 0.4 && dur > 0.1 {
		add(domain.CatVox, 1.5)
	}

	// ── Loop / Drum Loop ──────────────────────────────────────────────────────
	if onsets >= 4 {
		add(domain.CatLoop, 2.0)
	}
	if onsets >= 8 {
		add(domain.CatDrumLoop, 2.0)
	}
	if dur >= 4.0 {
		add(domain.CatLoop, 2.0)
	}
	if dur >= 8.0 {
		add(domain.CatLoop, 2.0)
	}
	if dur >= 4.0 && onsets >= 4 && centroid > 2000 {
		add(domain.CatDrumLoop, 3.0)
		add(domain.CatLoop, -1.0)
	}
	// Fallback for files where OnsetCount was not yet computed:
	// preserve the old ZCR/centroid heuristic for long files.
	if dur >= 4.0 && onsets == 0 {
		if zcr > 0.16 || centroid > 3000 {
			add(domain.CatDrumLoop, 5.0)
		}
	}

	return s
}

// classifyByAudio is the legacy hard-decision classifier used when only the
// original 7 features are available (SpectralFlatness and OnsetCount are zero).
func classifyByAudio(f domain.AudioFeatures) (domain.Category, bool) {
	dur := f.DurationSeconds
	centroid := f.SpectralCentroid
	zcr := f.ZeroCrossRate
	lowR := f.LowEnergyRatio
	highR := f.HighEnergyRatio
	fastAttack := f.AttackTime >= 0 && f.AttackTime < 0.025

	if centroid > 7000 && zcr > 0.30 && dur < 0.20 {
		return domain.CatHiHat, true
	}
	if centroid > 5000 && zcr > 0.20 {
		if dur <= 0.30 {
			return domain.CatHiHat, true
		}
		return domain.CatOpenHat, true
	}
	if highR > 0.45 && centroid > 4500 && dur < 0.35 {
		return domain.CatHiHat, true
	}
	if lowR > 0.60 && centroid < 500 && dur > 0.25 {
		return domain.Cat808, true
	}
	if lowR > 0.40 && centroid < 600 && fastAttack && dur < 1.5 {
		return domain.CatKick, true
	}
	if centroid < 400 && fastAttack && dur < 0.6 && lowR > 0.30 {
		return domain.CatKick, true
	}
	if zcr > 0.18 && centroid >= 1000 && centroid <= 6000 && dur < 1.2 {
		if fastAttack && dur < 0.10 {
			return domain.CatClap, true
		}
		return domain.CatSnare, true
	}
	if centroid < 2000 && zcr < 0.12 && dur < 2.5 {
		return domain.CatPerc, true
	}
	if dur >= 4.0 {
		if zcr > 0.16 || centroid > 3000 {
			return domain.CatDrumLoop, true
		}
		return domain.CatLoop, true
	}
	return domain.CatFX, false
}
