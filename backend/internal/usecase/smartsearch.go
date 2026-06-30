package usecase

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// SmartSearchService turns a free-text request like "тёмные агрессивные 808 на
// 140 bpm" into a structured domain.SearchQuery: it recognises mood/genre words
// (mapped to tags), instrument words (mapped to the 40 categories) and tempo
// hints, in both Russian and English. Anything it cannot interpret falls back
// to a plain full-text search so the box is never a dead end.
type SmartSearchService struct {
	samples domain.SampleRepository
}

// NewSmartSearchService wires a smart-search service.
func NewSmartSearchService(samples domain.SampleRepository) *SmartSearchService {
	return &SmartSearchService{samples: samples}
}

// Interpretation reports what the parser understood, for UI feedback.
type Interpretation struct {
	Categories []domain.Category `json:"categories"`
	Tags       []string          `json:"tags"`
	MinBPM     int               `json:"minBpm"`
	MaxBPM     int               `json:"maxBpm"`
	FreeText   string            `json:"freeText"`
}

// SmartResult bundles the matches with the interpretation that produced them.
type SmartResult struct {
	Items          []*domain.Sample `json:"items"`
	Total          int              `json:"total"`
	Interpretation Interpretation   `json:"interpretation"`
}

// moodTerms maps Russian/English mood & genre words to canonical tags. These
// mirror the vocabulary produced by the TagGenerator so a search lines up with
// generated tags.
var moodTerms = map[string]string{
	"dark": "dark", "тёмные": "dark", "темные": "dark", "тёмный": "dark", "темный": "dark",
	"aggressive": "aggressive", "агрессивные": "aggressive", "агрессивный": "aggressive", "злые": "aggressive",
	"emotional": "emotional", "эмоциональные": "emotional", "эмоциональный": "emotional",
	"sad": "sad", "грустные": "sad", "грустный": "sad", "печальные": "sad",
	"melodic": "melodic", "мелодичные": "melodic", "мелодичный": "melodic",
	"trap": "trap", "трэп": "trap", "трап": "trap",
	"rage": "rage", "рейдж": "rage", "рэйдж": "rage",
	"drill": "drill", "дрилл": "drill",
	"cloud": "cloud", "клауд": "cloud",
	"jersey": "jersey", "джерси": "jersey",
	"pluggnb": "pluggnb", "плагг": "pluggnb", "плаг": "pluggnb",
	"futuristic": "futuristic", "футуристичные": "futuristic", "футуристичный": "futuristic",
	"hard": "hard", "жёсткие": "hard", "жесткие": "hard",
	"soft": "soft", "мягкие": "soft", "мягкий": "soft",
	"warm": "warm", "тёплые": "warm", "теплые": "warm",
	"bright": "bright", "яркие": "bright", "яркий": "bright",
	"lofi": "lofi", "лоуфай": "lofi", "лофай": "lofi",
}

// instrumentTerms maps words to one of the 11 categories.
var instrumentTerms = map[string]domain.Category{
	"808": domain.Cat808, "сабы": domain.Cat808, "саб": domain.Cat808,
	"kick": domain.CatKick, "кик": domain.CatKick, "бочка": domain.CatKick, "бочки": domain.CatKick,
	"snare": domain.CatSnare, "снейр": domain.CatSnare, "снэйр": domain.CatSnare, "малый": domain.CatSnare,
	"clap": domain.CatClap, "клэп": domain.CatClap, "клап": domain.CatClap, "хлопок": domain.CatClap,
	"hat": domain.CatHiHat, "hihat": domain.CatHiHat, "хэт": domain.CatHiHat, "хайхэт": domain.CatHiHat, "хай-хэт": domain.CatHiHat,
	"openhat": domain.CatOpenHat, "оупенхэт": domain.CatOpenHat,
	"crash": domain.CatOpenHat, "креш": domain.CatOpenHat,
	"ride": domain.CatOpenHat, "райд": domain.CatOpenHat,
	"cymbal": domain.CatOpenHat, "тарелка": domain.CatOpenHat, "тарелки": domain.CatOpenHat,
	"perc": domain.CatPerc, "перк": domain.CatPerc, "перкуссия": domain.CatPerc, "перкуссии": domain.CatPerc,
	"rim": domain.CatPerc, "римшот": domain.CatPerc,
	"tom": domain.CatPerc, "том": domain.CatPerc,
	"foley": domain.CatPerc, "фоли": domain.CatPerc,
	"vox": domain.CatVox, "vocal": domain.CatVox, "вокал": domain.CatVox, "вокалы": domain.CatVox,
	"chant": domain.CatVox, "чант": domain.CatVox,
	"fx": domain.CatFX, "эффект": domain.CatFX, "эффекты": domain.CatFX,
	"sweep": domain.CatFX, "свип": domain.CatFX,
	"impact": domain.CatFX, "импакт": domain.CatFX,
	"riser": domain.CatFX, "райзер": domain.CatFX,
	"midi": domain.CatFX, "миди": domain.CatFX,
	"texture": domain.CatFX, "текстура": domain.CatFX,
	"ambience": domain.CatFX, "эмбиенс": domain.CatFX,
	"piano": domain.CatLoop, "пиано": domain.CatLoop, "пианино": domain.CatLoop,
	"guitar": domain.CatLoop, "гитара": domain.CatLoop, "гитары": domain.CatLoop,
	"bell": domain.CatLoop, "белл": domain.CatLoop, "колокол": domain.CatLoop,
	"pluck": domain.CatLoop, "плак": domain.CatLoop,
	"synth": domain.CatLoop, "синт": domain.CatLoop, "синтезатор": domain.CatLoop,
	"pad": domain.CatLoop, "пэд": domain.CatLoop, "пад": domain.CatLoop,
	"bass": domain.CatLoop, "бас": domain.CatLoop, "басс": domain.CatLoop,
	"melody": domain.CatLoop, "мелодия": domain.CatLoop, "мелодии": domain.CatLoop,
	"loop": domain.CatLoop, "луп": domain.CatLoop, "лупы": domain.CatLoop,
	"drumloop": domain.CatDrumLoop, "драмлуп": domain.CatDrumLoop,
}

var bpmRe = regexp.MustCompile(`(?:(?:на|at)\s+)?(\d{2,3})\s*(?:bpm|бпм)`)
var bareNumRe = regexp.MustCompile(`\b(\d{2,3})\b`)

// Parse converts NL text into a SearchQuery and a description of what it found.
func (s *SmartSearchService) Parse(text string) (domain.SearchQuery, Interpretation) {
	lower := strings.ToLower(strings.TrimSpace(text))
	interp := Interpretation{}

	// Tempo: prefer an explicit "<n> bpm", else a bare 2–3 digit number.
	if m := bpmRe.FindStringSubmatch(lower); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			interp.MinBPM, interp.MaxBPM = n-3, n+3
		}
		lower = bpmRe.ReplaceAllString(lower, " ")
	} else if m := bareNumRe.FindStringSubmatch(lower); m != nil {
		// Ignore "808" as a tempo — it is an instrument token.
		if m[1] != "808" {
			if n, err := strconv.Atoi(m[1]); err == nil && n >= 40 && n <= 220 {
				interp.MinBPM, interp.MaxBPM = n-3, n+3
			}
		}
	}
	if strings.Contains(lower, "медленны") || strings.Contains(lower, "slow") {
		interp.MaxBPM = 95
	}
	if strings.Contains(lower, "быстры") || strings.Contains(lower, "fast") {
		interp.MinBPM = 140
	}

	seenCat := map[domain.Category]bool{}
	seenTag := map[string]bool{}
	var leftover []string

	for _, tok := range tokenize(lower) {
		if cat, ok := instrumentTerms[tok]; ok {
			if !seenCat[cat] {
				seenCat[cat] = true
				interp.Categories = append(interp.Categories, cat)
			}
			continue
		}
		if tag, ok := moodTerms[tok]; ok {
			if !seenTag[tag] {
				seenTag[tag] = true
				interp.Tags = append(interp.Tags, tag)
			}
			continue
		}
		leftover = append(leftover, tok)
	}

	q := domain.SearchQuery{
		Categories: interp.Categories,
		Tags:       interp.Tags,
		MinBPM:     interp.MinBPM,
		MaxBPM:     interp.MaxBPM,
		Sort:       "used",
		Order:      "desc",
	}
	// Always pass unrecognised tokens to FTS so they narrow results further.
	// If nothing structured was recognised at all, fall back to the original
	// query string so partial CJK/emoji or uncommon names still match.
	if len(leftover) > 0 {
		q.Text = strings.Join(leftover, " ")
		interp.FreeText = q.Text
	} else if len(interp.Categories) == 0 && len(interp.Tags) == 0 && interp.MinBPM == 0 && interp.MaxBPM == 0 {
		q.Text = strings.TrimSpace(text)
		interp.FreeText = q.Text
	}
	return q, interp
}

// Search parses the text and runs the resulting query.
func (s *SmartSearchService) Search(ctx context.Context, text string, limit, offset int) (SmartResult, error) {
	q, interp := s.Parse(text)
	if limit > 0 {
		q.Limit = limit
	}
	q.Offset = offset
	items, total, err := s.samples.Search(ctx, q)
	if err != nil {
		return SmartResult{}, err
	}
	return SmartResult{Items: items, Total: total, Interpretation: interp}, nil
}

// tokenize splits text on non-alphanumeric runes, keeping Cyrillic and digits,
// and preserving the hyphen so "хай-хэт" survives as one token.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		if r == '-' {
			return false
		}
		switch {
		case r >= 'a' && r <= 'z':
			return false
		case r >= '0' && r <= '9':
			return false
		case r >= 'а' && r <= 'я' || r == 'ё':
			return false
		default:
			return true
		}
	})
	return fields
}
