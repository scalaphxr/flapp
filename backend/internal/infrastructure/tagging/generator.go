package tagging

import (
	"strings"

	"github.com/flapp/core/internal/domain"
)

// Generator implements domain.TagGenerator. It derives descriptive, searchable
// tags from a sample's name, audio features, BPM, category and duration.
type Generator struct{}

func New() *Generator { return &Generator{} }

// nameKeywords maps lowercase substrings to one or more tags.
// Longer, more specific keys come first (they override shorter matches naturally
// since we stop at the first match per keyword group — but here we add all matches,
// so the map order doesn't restrict combining tags).
var nameKeywords = [][2]string{
	// ── Genre / style ─────────────────────────────────────────────────────────
	{"trap", "trap"},
	{"drill", "drill"},
	{"pluggnb", "pluggnb"},
	{"plugg", "pluggnb"},
	{"jersey", "jersey"},
	{"afro", "afrobeats"},
	{"afrobeat", "afrobeats"},
	{"boom bap", "boom bap"},
	{"boombap", "boom bap"},
	{"lofi", "lofi"},
	{"lo-fi", "lofi"},
	{"lo fi", "lofi"},
	{"rnb", "r&b"},
	{"r&b", "r&b"},
	{"phonk", "phonk"},
	{"rage", "rage"},
	{"cloud", "cloud"},
	{"wave", "wave"},
	{"uk drill", "drill"},
	{"ny drill", "drill"},
	{"dancehall", "dancehall"},
	{"reggaeton", "reggaeton"},
	{"sample flip", "sample flip"},
	{"chopped", "chopped"},
	{"flipped", "sample flip"},

	// ── Mood / character ──────────────────────────────────────────────────────
	{"dark", "dark"},
	{"evil", "dark"},
	{"sinister", "dark"},
	{"ominous", "dark"},
	{"scary", "dark"},
	{"eerie", "dark"},
	{"creepy", "dark"},
	{"sad", "sad"},
	{"melanchol", "sad"},
	{"depress", "sad"},
	{"lonely", "sad"},
	{"emotional", "emotional"},
	{"emotion", "emotional"},
	{"sentimental", "emotional"},
	{"happy", "happy"},
	{"bright", "bright"},
	{"uplifting", "uplifting"},
	{"positive", "uplifting"},
	{"euphoric", "uplifting"},
	{"aggressive", "aggressive"},
	{"aggro", "aggressive"},
	{"angry", "aggressive"},
	{"intense", "aggressive"},
	{"chill", "chill"},
	{"relax", "chill"},
	{"calm", "chill"},
	{"soft", "soft"},
	{"gentle", "soft"},
	{"ambient", "ambient"},
	{"atmos", "atmospheric"},
	{"dreamy", "dreamy"},
	{"ethereal", "ethereal"},
	{"hype", "hype"},
	{"energetic", "energetic"},
	{"epic", "epic"},
	{"cinematic", "cinematic"},
	{"haunting", "haunting"},
	{"nostalgic", "nostalgic"},
	{"hypnotic", "hypnotic"},
	{"romantic", "romantic"},

	// ── Sound quality / texture ────────────────────────────────────────────────
	{"distort", "distorted"},
	{"saturate", "saturated"},
	{"crunch", "crunchy"},
	{"gritty", "gritty"},
	{"rough", "rough"},
	{"dirty", "dirty"},
	{"filthy", "dirty"},
	{"muddy", "muddy"},
	{"clean", "clean"},
	{"crisp", "crispy"},
	{"crispy", "crispy"},
	{"punchy", "punchy"},
	{"heavy", "heavy"},
	{"fat", "heavy"},
	{"thin", "thin"},
	{"airy", "airy"},
	{"spacious", "spacious"},
	{"wide", "wide"},
	{"stereo", "wide"},
	{"mono", "mono"},
	{"dry", "dry"},
	{"wet", "wet"},
	{"reverb", "reverb"},
	{"delay", "delay"},
	{"echo", "echo"},
	{"comp", "compressed"},
	{"limited", "compressed"},
	{"warm", "warm"},
	{"cold", "cold"},
	{"icy", "cold"},
	{"deep", "deep"},
	{"sub", "sub"},
	{"smooth", "smooth"},
	{"silky", "smooth"},
	{"glitch", "glitch"},
	{"stutter", "glitch"},
	{"noisy", "noisy"},
	{"static", "noisy"},
	{"tape", "vintage"},
	{"vinyl", "vintage"},
	{"vintage", "vintage"},
	{"retro", "vintage"},
	{"analog", "analog"},
	{"analogue", "analog"},
	{"digital", "digital"},
	{"lo bit", "lofi"},
	{"8bit", "lofi"},
	{"8-bit", "lofi"},
	{"chopped", "chopped"},
	{"pitched", "pitched"},
	{"detuned", "detuned"},
	{"layered", "layered"},

	// ── Instrument / source ────────────────────────────────────────────────────
	{"piano", "piano"},
	{"rhodes", "piano"},
	{"keys", "keys"},
	{"guitar", "guitar"},
	{"bass guitar", "guitar"},
	{"violin", "strings"},
	{"cello", "strings"},
	{"strings", "strings"},
	{"orchestra", "orchestral"},
	{"brass", "brass"},
	{"trumpet", "brass"},
	{"horn", "brass"},
	{"flute", "woodwind"},
	{"saxophone", "sax"},
	{"sax", "sax"},
	{"choir", "choir"},
	{"vocal", "vocal"},
	{"voice", "vocal"},
	{"808", "808"},
	{"synth", "synth"},
	{"synthesizer", "synth"},
	{"saw", "saw"},
	{"square", "square wave"},
	{"triangle wave", "triangle wave"},
	{"sample", "sample"},
	{"arp", "arp"},
	{"pluck", "pluck"},
	{"bell", "bell"},
	{"pad", "pad"},
	{"lead", "lead"},
	{"bass", "bass"},

	// ── Tempo / energy descriptors ─────────────────────────────────────────────
	{"fast", "fast"},
	{"quick", "fast"},
	{"slow", "slow"},
	{"mid tempo", "mid-tempo"},
	{"uptempo", "uptempo"},
	{"half time", "half-time"},
	{"halftime", "half-time"},

	// ── Mix / production ──────────────────────────────────────────────────────
	{"oneshot", "one-shot"},
	{"one shot", "one-shot"},
	{"loop", "loop"},
	{"fill", "fill"},
	{"break", "break"},
	{"intro", "intro"},
	{"outro", "outro"},
	{"drop", "drop"},
	{"build", "build"},
	{"riser", "riser"},
	{"transition", "transition"},
	{"fx", "fx"},
	{"sfx", "fx"},
}

// Generate returns a de-duplicated, sorted set of lowercase tags.
func (g *Generator) Generate(s *domain.Sample) []string {
	set := map[string]struct{}{}
	add := func(t string) { set[t] = struct{}{} }

	hay := strings.ToLower(s.Name + " " + s.SourceLabel)

	// 1. Name-keyword matching.
	for _, pair := range nameKeywords {
		if strings.Contains(hay, pair[0]) {
			add(pair[1])
		}
	}

	// 2. Audio-feature tags (only when signal was analysed).
	f := s.Features
	if f.Analyzed {
		// Brightness / frequency character.
		switch {
		case f.SpectralCentroid < 250:
			add("sub")
			add("bass")
			add("deep")
		case f.SpectralCentroid < 600:
			add("bass")
			add("deep")
		case f.SpectralCentroid < 1500:
			add("warm")
		case f.SpectralCentroid > 6000:
			add("bright")
			add("crispy")
		case f.SpectralCentroid > 3500:
			add("bright")
		}

		// Low-end dominance.
		if f.LowEnergyRatio > 0.60 {
			add("bass")
			add("deep")
		}
		if f.HighEnergyRatio > 0.45 {
			add("bright")
			add("airy")
		}

		// Noisiness.
		if f.ZeroCrossRate > 0.35 {
			add("noisy")
		}

		// Loudness / dynamic.
		if f.PeakAmplitude > 0.95 && f.RMS > 0.35 {
			add("loud")
			add("heavy")
		} else if f.RMS < 0.03 {
			add("soft")
			add("quiet")
		}

		// Transient character.
		if f.AttackTime >= 0 {
			switch {
			case f.AttackTime < 0.004:
				add("snappy")
				add("tight")
				add("punchy")
			case f.AttackTime < 0.015:
				add("tight")
				add("punchy")
			case f.AttackTime < 0.05:
				add("punchy")
			case f.AttackTime > 0.15:
				add("slow attack")
				add("smooth")
			}
		}

		// Duration character.
		dur := f.DurationSeconds
		switch {
		case dur < 0.1:
			add("one-shot")
			add("short")
		case dur < 0.5:
			add("one-shot")
		case dur < 2.0:
			add("one-shot")
		case dur >= 6.0:
			add("loop")
		}
	}

	// 3. Category-implied tags.
	switch s.Category {
	case domain.Cat808:
		add("808")
		add("bass")
		add("sub")
		add("low")
	case domain.CatKick:
		add("kick")
		add("low")
		if f.Analyzed && f.AttackTime < 0.015 {
			add("punchy")
			add("tight")
		}
	case domain.CatSnare:
		add("snare")
		add("crack")
	case domain.CatClap:
		add("clap")
		add("snap")
	case domain.CatHiHat:
		add("hihat")
		add("hat")
		add("high")
		add("rhythmic")
	case domain.CatOpenHat:
		add("open hat")
		add("cymbal")
		add("high")
	case domain.CatPerc:
		add("perc")
		add("rhythmic")
	case domain.CatVox:
		add("vocal")
		add("voice")
	case domain.CatDrumLoop:
		add("drums")
		add("loop")
		add("beat")
		add("rhythmic")
	case domain.CatLoop:
		add("loop")
		add("melodic")
	case domain.CatFX:
		add("fx")
	}

	// 4. BPM-derived tags.
	if s.BPM > 0 {
		switch {
		case s.BPM < 75:
			add("slow")
		case s.BPM < 90:
			add("slow")
			add("chill")
		case s.BPM < 105:
			add("mid-tempo")
		case s.BPM < 120:
			add("mid-tempo")
		case s.BPM < 135:
			add("trap")
		case s.BPM < 150:
			add("drill")
		default:
			add("fast")
			add("energetic")
		}
	}

	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	return out
}
