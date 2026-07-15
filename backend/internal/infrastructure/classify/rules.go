package classify

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// keywordRule maps lowercase substrings to a category.
type keywordRule struct {
	cat   domain.Category
	terms []string
}

// nameRules: evaluated top-to-bottom with substring matching (containsTerm).
// More specific / longer terms come first so shorter substrings don't steal.
var nameRules = []keywordRule{

	// ── Explicit drum-loop / fill markers ─────────────────────────────────────
	{domain.CatDrumLoop, []string{
		"drum loop", "drumloop", "drum_loop",
		"drum fill", "drumfill", "drum_fill",
		"top loop", "toploop", "top_loop",
		"beat loop", "groove loop",
		"breakbeat", "break beat", "amen break",
		"drums_top", "drums top",
		"stems", "[stem]",
	}},

	// ── Explicit loop suffix (any musical type) ────────────────────────────────
	{domain.CatLoop, []string{
		"melody loop", "melodyloop", "melodic loop",
		"music loop", "chord loop",
		" loop", "_loop", "-loop", "loop_", "loop-",
		"fullloop", "full loop", "full_loop",
		"[starter]", "[phrase]",
	}},

	// ── Sub / 808 ─────────────────────────────────────────────────────────────
	// "bd" here specifically means the Roland TR-808 Bass Drum sound used as
	// the sustained 808 sub-bass instrument (the genre's namesake) — the
	// convention in this trap/hip-hop library, confirmed by the user against
	// their real files. It is NOT the generic GM "Bass Drum = Kick" shorthand.
	{domain.Cat808, []string{
		"808",
		"sub bass", "subbass", "sub_bass",
		"808 bass", "808bass",
		"_bd_", "_bd.", "-bd-", " bd_", " bd.",
		" sub ", "_sub_", "-sub-", "sub_", "-sub", "_sub.",
	}},

	// ── Kick ──────────────────────────────────────────────────────────────────
	{domain.CatKick, []string{
		"kick", "kik", "kck",
		"bassdrum", "bass drum", "base drum",
		"_kd_", "_kd.", "-kd-", "kd_", " kd_", " kd.",
		" boom", "_boom", "-boom",
	}},

	// ── Snare ─────────────────────────────────────────────────────────────────
	{domain.CatSnare, []string{
		"snare", "snr", "snare roll",
		" sn ", "_sn_", "_sn.", "-sn-", "_sn-",
		"sn_", "sn-", "sn.",
	}},

	// ── Clap / rim ────────────────────────────────────────────────────────────
	{domain.CatClap, []string{
		"clap", "claps", "handclap", "hand clap",
		"rimshot", "rim shot", "rim_shot",
		"sidestick", "side stick",
		" rim ", "_rim_", "_rim.", "rim_",
		"snap", "finger snap",
		"_cl_", "_cl.", " cl_", " cl.", "cl_", "cl-",
	}},

	// ── Open hat / cymbal / crash (before closed hat) ─────────────────────────
	{domain.CatOpenHat, []string{
		"open hat", "openhat", "open_hat", "open-hat",
		"ophat", "op hat",
		"open hi hat", "open hihat", "open hi-hat",
		"crash", "ride", "cymbal", "cym_", "_cym",
		"splash", "china", "stack",
		"_oh_", "_oh.", "-oh-", "oh_", "-oh_", " oh_", " oh.", "_oh-",
		"_cr_", "_cr.", "-cr-", "cr_", " cr_", " cr.",
		"_rd_", "_rd.", "-rd-", "rd_", " rd_", " rd.",
	}},

	// ── Closed hi-hat ─────────────────────────────────────────────────────────
	{domain.CatHiHat, []string{
		"hihat", "hi-hat", "hi hat", "hi_hat",
		"closed hat", "closedhat", "closed_hat",
		"chh", "clhat",
		"_hh_", "_hh.", "-hh-", "hh_", "-hh_", "_hh-",
		"hh.", "hh-",
	}},

	// ── Percussion ────────────────────────────────────────────────────────────
	{domain.CatPerc, []string{
		"perc", "percussion",
		"tom", "floor tom", "rack tom",
		"shaker", "tamb", "tambourine",
		"conga", "bongo", "bongos",
		"cowbell", "woodblock", "wood block",
		"clave", "triangle", "agogo", "cabasa", "guiro",
		"maraca", "cajon", "djembe",
		"foley", "footstep",
		"vinyl crackle", "record crackle",
		" hit", "_hit", "-hit", "hit_", "hit-",
		"_pc_", "_pc.", " pc_", " pc.",
		"scratch", "scratches",
	}},

	// ── Vocals / voice ────────────────────────────────────────────────────────
	{domain.CatVox, []string{
		"vocal", "vocals", "lead vox",
		"chant", "shout", "yell", "scream",
		"verse", "hook", "chorus",
		"acapella", "acapela", "a capella",
		"adlib", "ad lib", "ad-lib",
		"vocal chop", "vox chop",
		"voice", "voices",
		"moan", "breath",
		"_what", " what", "-what",
		"_yeah", " yeah", "-yeah",
		"_yah", " yah",
		"_hey", " hey",
		"_ayy", " ayy",
		"_brr", " brr",
		"_skrr", " skrr",
		// ch = chant (user convention; "chh" double-h caught separately as hi-hat)
		"_ch_", "_ch.", " ch_", " ch.", "-ch-", "-ch_",
	}},

	// ── FX / atmospheric / transitional ───────────────────────────────────────
	{domain.CatFX, []string{
		"riser", "rise", "uplifter", "uplift",
		"build up", "buildup", "build_up",
		"downlifter", "downer",
		"drop fx", "dropfx",
		"sweep", "whoosh", "swoosh",
		"transition",
		"impact", "slam", "braam", "brahm",
		"hit fx", "hitfx",
		" fx", "_fx", "-fx", "fx_", "fx-", "sfx",
		"effect", "effects",
		"glitch", "stutter",
		"reverse", "reversed", "_rev", " rev ",
		"texture", "drone",
		"noise floor", "noise_floor",
		"ambience", "ambient",
		"atmosphere", "atmos",
		"midi", ".mid", "_midi", " midi",
		"crowd", "wobble", "wobb",
		"brass",
	}},

	// ── Melodic instrument names ───────────────────────────────────────────────
	// Checked LAST. If the name has no explicit loop marker and duration < 4 s,
	// the audio classifier decides instead of forcing CatLoop.
	{domain.CatLoop, []string{
		"piano", "rhodes", "keys", "grand", "epiano", "e-piano", "wurli", "wurlitzer",
		"guitar", "gtr", "acoustic gtr",
		"bell", "bells", "glock", "glockenspiel", "chime", "kalimba", "marimba",
		"pluck", "plk",
		"synth", "synthesizer",
		"supersaw",
		" arp", "_arp", "-arp", "arpeggio",
		"bassline", "bass line", "bass_line",
		"reese",
		"melody", "melodic",
		"chord", "chords", "harmony",
		"progression", "topline", "top line",
		" pad", "_pad", "-pad", "pad_", "pad-",
		" saw", "_saw", "-saw",
		" stab", "_stab", "-stab",
		"strings", "string",
		" lead", "_lead", "-lead",
	}},
}

// abbreviationRules are matched with word-boundary awareness (containsWord).
var abbreviationRules = []keywordRule{
	// bd = 808 (TR-808 bass drum used as the sub instrument); sub too
	{domain.Cat808, []string{"bd", "sub"}},
	// kick abbreviations
	{domain.CatKick, []string{"kk"}},
	// sd = snare drum
	{domain.CatSnare, []string{"sn", "sd"}},
	// cl = clap, sc = snareclap
	{domain.CatClap, []string{"cl", "sc"}},
	// oh = open hat, cr = crash, rd = ride
	{domain.CatOpenHat, []string{"oh", "cr", "rd"}},
	// hh before hat so "hh" is caught as a unit
	{domain.CatHiHat, []string{"hh", "hat"}},
	// ch = chant/vocal; vox and ad-lib words
	{domain.CatVox, []string{"ch", "vox", "what", "yeah", "ayy", "hey", "brr"}},
}

// ── Folder-path scanner ───────────────────────────────────────────────────────

var folderCategoryMap = map[string]domain.Category{
	"808": domain.Cat808, "808s": domain.Cat808,
	"sub": domain.Cat808, "sub bass": domain.Cat808, "bd": domain.Cat808,

	"kick": domain.CatKick, "kicks": domain.CatKick,
	"bass drum": domain.CatKick, "bassdrum": domain.CatKick,

	"snare": domain.CatSnare, "snares": domain.CatSnare,

	"clap": domain.CatClap, "claps": domain.CatClap,
	"rimshot": domain.CatClap, "rimshots": domain.CatClap, "rimz": domain.CatClap,
	"rims": domain.CatClap, "rim": domain.CatClap, "rim shots": domain.CatClap,

	"hh": domain.CatHiHat, "hi hat": domain.CatHiHat, "hi hats": domain.CatHiHat,
	"hihat": domain.CatHiHat, "hihats": domain.CatHiHat, "hi_hat": domain.CatHiHat,
	"hi-hat": domain.CatHiHat, "hi-hats": domain.CatHiHat,
	"closed hat": domain.CatHiHat, "closedhat": domain.CatHiHat,

	"oh": domain.CatOpenHat, "open hat": domain.CatOpenHat, "open hats": domain.CatOpenHat,
	"openhat": domain.CatOpenHat, "open-hat": domain.CatOpenHat, "op hat": domain.CatOpenHat,
	"crash": domain.CatOpenHat, "crashes": domain.CatOpenHat,
	"cymbal": domain.CatOpenHat, "cymbals": domain.CatOpenHat,
	"oh & crashes": domain.CatOpenHat,

	"perc": domain.CatPerc, "percs": domain.CatPerc, "percussion": domain.CatPerc,
	"scratch": domain.CatPerc, "scratches": domain.CatPerc,
	"shaker": domain.CatPerc, "shakers": domain.CatPerc,
	"tom": domain.CatPerc, "toms": domain.CatPerc,

	"fx": domain.CatFX, "fxs": domain.CatFX, "sfx": domain.CatFX,
	"effects": domain.CatFX, "riser": domain.CatFX, "risers": domain.CatFX,

	"vox": domain.CatVox, "vocals": domain.CatVox, "vocal": domain.CatVox,
	"chants": domain.CatVox, "voices": domain.CatVox,

	"loop": domain.CatLoop, "loops": domain.CatLoop, "loopkit": domain.CatLoop,
	"melody loops": domain.CatLoop,
	"pluck": domain.CatLoop, "plucks": domain.CatLoop,
	"pad": domain.CatLoop, "pads": domain.CatLoop,
	"lead": domain.CatLoop, "leads": domain.CatLoop,
	"synth": domain.CatLoop, "strings": domain.CatLoop,
	"melody": domain.CatLoop, "melodies": domain.CatLoop,
	"bell": domain.CatLoop, "bells": domain.CatLoop,

	"bass": domain.Cat808, // "bass" folder = sub bass / 808 sounds

	"drum loops": domain.CatDrumLoop, "loops drums": domain.CatDrumLoop,
}

// folderKeywordsByLength holds folderCategoryMap keys ordered longest-first
// so the substring fallback in classifyByFolderPath is deterministic and
// always prefers the most specific keyword. Go randomises map iteration
// order on every run, so ranging over folderCategoryMap directly (the old
// behaviour) picked an arbitrary winner whenever a folder name contained more
// than one keyword as a substring (e.g. "808 Kicks" matches both "808" and
// "kick") — this produced different classifications for the same file across
// runs, matching the reported "sometimes" mis-tagging.
var folderKeywordsByLength = sortedFolderKeywords()

func sortedFolderKeywords() []string {
	keys := make([]string, 0, len(folderCategoryMap))
	for k := range folderCategoryMap {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j] // stable tiebreak between equal-length keys
	})
	return keys
}

func classifyByFolderPath(relPath string) (domain.Category, float64) {
	parts := strings.Split(filepath.ToSlash(relPath), "/")
	for i := len(parts) - 2; i >= 0 && i >= len(parts)-5; i-- {
		dir := strings.ToLower(strings.TrimSpace(parts[i]))
		if cat, ok := folderCategoryMap[dir]; ok {
			weight := 9.0 - float64(len(parts)-2-i)*1.5
			if weight < 4.0 {
				weight = 4.0
			}
			return cat, weight
		}
		for _, keyword := range folderKeywordsByLength {
			if len(keyword) >= 3 && strings.Contains(dir, keyword) {
				return folderCategoryMap[keyword], 5.0
			}
		}
	}
	return "", 0
}

// ── Dash/space prefix classifier ──────────────────────────────────────────────

type prefixRule struct {
	prefix string
	cat    domain.Category
}

var dashPrefixRules = []prefixRule{
	{"open hat", domain.CatOpenHat},
	{"hi hat", domain.CatHiHat},
	{"hihat", domain.CatHiHat},
	{"808", domain.Cat808},
	{"snare", domain.CatSnare},
	{"clap", domain.CatClap},
	{"kick", domain.CatKick},
	{"perc", domain.CatPerc},
	{"sfx", domain.CatFX},
	{"fx", domain.CatFX},
	{"vox", domain.CatVox},
	{"strings", domain.CatLoop},
	{"melody", domain.CatLoop},
	{"synth", domain.CatLoop},
	{"brass", domain.CatFX},
	{"pluck", domain.CatLoop},
	{"lead", domain.CatLoop},
	{"pad", domain.CatLoop},
	{"bass", domain.Cat808},
	{"hh", domain.CatHiHat},
	{"oh", domain.CatOpenHat},
	{"sn", domain.CatSnare},
}

func classifyByDashPrefix(name string) (domain.Category, bool) {
	lower := strings.ToLower(name)
	for _, rule := range dashPrefixRules {
		if strings.HasPrefix(lower, rule.prefix+" - ") {
			return rule.cat, true
		}
		if strings.HasPrefix(lower, rule.prefix+" ") {
			return rule.cat, true
		}
	}
	return "", false
}

// ── Abbreviation token classifier ─────────────────────────────────────────────

type abbrRule struct {
	token string
	cat   domain.Category
}

var abbreviationTokenMap = []abbrRule{
	{"bd", domain.Cat808},   // bd = 808 (TR-808 bass drum used as the sub instrument)
	{"sc", domain.CatClap},  // sc = snareclap
	{"sn", domain.CatSnare},
	{"sd", domain.CatSnare},
	{"oh", domain.CatOpenHat},
	{"hh", domain.CatHiHat},
	{"kd", domain.CatKick},
	{"cr", domain.CatOpenHat},
	{"rd", domain.CatOpenHat},
	{"chh", domain.CatHiHat},
}

func classifyByAbbreviations(haystack string) (domain.Category, bool) {
	for _, rule := range abbreviationTokenMap {
		if containsWord(haystack, rule.token) {
			return rule.cat, true
		}
	}
	return "", false
}

// ── BPM / loop pattern detector ───────────────────────────────────────────────

var (
	bpmPattern  = regexp.MustCompile(`\b\d{2,3}[\s_]?bpm\b`)
	bpmPattern2 = regexp.MustCompile(`\b\d{2,3}[\s_]bpm[\s_]`)
	starterTag  = regexp.MustCompile(`\[starter\]`)
	phraseTag   = regexp.MustCompile(`\[phrase\]`)
	keyNotation = regexp.MustCompile(`\s*[\(\[]?[a-g][#b]?(maj|min|m|phryg)?\]?\s*$|\s+[a-g][#b]?(maj|min)\s*$|\s+[a-g][#b]?\d\s*$`)
	drumsTopPat = regexp.MustCompile(`[\s_]drums?[\s_]top[\s_]?|drums_top`)
	drumsPat    = regexp.MustCompile(`[\s_]drums?[\s_]`)
)

func detectLoopByPattern(name string) (isLoop, isDrum bool) {
	lower := strings.ToLower(name)
	hasBPM := bpmPattern.MatchString(lower) || bpmPattern2.MatchString(lower)
	hasStarter := starterTag.MatchString(lower)
	hasPhrase := phraseTag.MatchString(lower)
	hasDrumsTop := drumsTopPat.MatchString(lower)
	hasDrums := drumsPat.MatchString(lower)

	if hasDrumsTop || (hasBPM && hasDrums) {
		return true, true
	}
	if hasBPM || hasStarter || hasPhrase {
		return true, false
	}
	return false, false
}

// ── Suffix-word classifier ────────────────────────────────────────────────────

type suffixRule struct {
	suffix string
	cat    domain.Category
}

var suffixWordRules = []suffixRule{
	{" bass", domain.Cat808},
	{"_bass", domain.Cat808},
	{"-bass", domain.Cat808},
	{" bell", domain.CatLoop},
	{"_bell", domain.CatLoop},
	{" lead", domain.CatLoop},
	{"_lead", domain.CatLoop},
	{" pluck", domain.CatLoop},
	{"_pluck", domain.CatLoop},
	{" pad", domain.CatLoop},
	{"_pad", domain.CatLoop},
	{" synth", domain.CatLoop},
	{"_synth", domain.CatLoop},
	{" chant", domain.CatVox},
	{"_chant", domain.CatVox},
	{" scratch", domain.CatPerc},
	{"_scratch", domain.CatPerc},
}

func classifyBySuffixWord(name string) (domain.Category, bool) {
	base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
	base = keyNotation.ReplaceAllString(base, "")
	base = strings.TrimRight(base, " _-")
	for _, rule := range suffixWordRules {
		if strings.HasSuffix(base, rule.suffix) {
			return rule.cat, true
		}
	}
	return "", false
}

// ── Kick vs 808 disambiguation ────────────────────────────────────────────────

// resolveKickVs808 handles the most common source of Kick/808 confusion:
// sample names that carry BOTH an explicit kick word and "808" styling, e.g.
// "808 Kick.wav", "Trap Kick 808 Bright.wav". Producers use "808" as a
// style/character tag across a whole kit (808 Kick, 808 Snare, 808 Hi Hat…),
// not exclusively for the sustained sub-bass instrument, so an explicit kick
// word should win over a bare "808" tag. It only backs off when the name also
// carries unambiguous sub-bass phrasing ("sub bass", "808 bass", "bassline"…),
// in which case the normal passes below decide.
//
// Deliberately excludes "bd": in this (trap/808) library "bd" means the
// TR-808 bass drum used as the sub instrument itself, i.e. 808 — not a Kick
// abbreviation. Confirmed against real library files.
func resolveKickVs808(haystack string) (domain.Category, bool) {
	hasKickWord := containsTerm(haystack, "kick") || containsTerm(haystack, "kik") ||
		containsTerm(haystack, "kck") || containsTerm(haystack, "bassdrum") ||
		containsTerm(haystack, "bass drum") || containsTerm(haystack, "base drum")
	if !hasKickWord {
		return "", false
	}
	hasSubBassWord := containsTerm(haystack, "sub bass") || containsTerm(haystack, "subbass") ||
		containsTerm(haystack, "sub_bass") || containsTerm(haystack, "808 bass") ||
		containsTerm(haystack, "808bass") || containsTerm(haystack, "bassline") ||
		containsTerm(haystack, "bass line") || containsTerm(haystack, "bass_line") ||
		containsWord(haystack, "sub")
	if hasSubBassWord {
		return "", false
	}
	return domain.CatKick, true
}

// ── Public multi-pass classifier ──────────────────────────────────────────────

// ClassifyByName runs six sequential passes and returns (cat, score, ok).
// Score: 9.0 = folder (near-certain) → 5.0 = keyword.
func ClassifyByName(name, relPath string) (domain.Category, float64, bool) {
	if cat, score := classifyByFolderPath(relPath); score > 0 {
		return cat, score, true
	}
	lower := strings.ToLower(name + " " + relPath)
	if cat, ok := resolveKickVs808(lower); ok {
		return cat, 8.0, true
	}
	if cat, ok := classifyByDashPrefix(name); ok {
		return cat, 9.0, true
	}
	if cat, ok := classifyByAbbreviations(lower); ok {
		return cat, 7.0, true
	}
	if isLoop, isDrum := detectLoopByPattern(name); isLoop {
		if isDrum {
			return domain.CatDrumLoop, 7.0, true
		}
		return domain.CatLoop, 7.0, true
	}
	if cat, ok := classifyByName(lower); ok {
		return cat, 5.0, true
	}
	return "", 0, false
}

// ── Private keyword classifier (Pass 5 + existing tests) ─────────────────────

func classifyByName(haystack string) (domain.Category, bool) {
	for _, rule := range nameRules {
		for _, term := range rule.terms {
			if containsTerm(haystack, term) {
				return rule.cat, true
			}
		}
	}
	for _, rule := range abbreviationRules {
		for _, abbr := range rule.terms {
			if containsWord(haystack, abbr) {
				return rule.cat, true
			}
		}
	}
	return domain.CatFX, false
}

// ── drumCategories ────────────────────────────────────────────────────────────

var drumCategories = map[domain.Category]bool{
	domain.CatKick:    true,
	domain.CatSnare:   true,
	domain.CatClap:    true,
	domain.CatOpenHat: true,
	domain.CatHiHat:   true,
	domain.CatPerc:    true,
}

// ClassifyDrumByName returns a specific drum category for a channel name / sample path.
func ClassifyDrumByName(name, samplePath string) (domain.Category, bool) {
	hay := strings.ToLower(name + " " + samplePath)
	for _, rule := range nameRules {
		if !drumCategories[rule.cat] {
			continue
		}
		for _, term := range rule.terms {
			if containsTerm(hay, term) {
				return rule.cat, true
			}
		}
	}
	for _, rule := range abbreviationRules {
		if !drumCategories[rule.cat] {
			continue
		}
		for _, abbr := range rule.terms {
			if containsWord(hay, abbr) {
				return rule.cat, true
			}
		}
	}
	for _, rule := range abbreviationTokenMap {
		if !drumCategories[rule.cat] {
			continue
		}
		if containsWord(hay, rule.token) {
			return rule.cat, true
		}
	}
	return "", false
}

// ── String helpers ────────────────────────────────────────────────────────────

func containsTerm(haystack, term string) bool {
	n, m := len(haystack), len(term)
	if m == 0 || m > n {
		return false
	}
	for i := 0; i+m <= n; i++ {
		if haystack[i:i+m] == term {
			return true
		}
	}
	return false
}

func containsWord(haystack, word string) bool {
	n, m := len(haystack), len(word)
	if m == 0 || m > n {
		return false
	}
	for i := 0; i+m <= n; i++ {
		if haystack[i:i+m] != word {
			continue
		}
		if i > 0 && isAlpha(haystack[i-1]) {
			continue
		}
		if i+m < n && isAlpha(haystack[i+m]) {
			continue
		}
		return true
	}
	return false
}

func isAlpha(c byte) bool {
	return c >= 'a' && c <= 'z'
}
