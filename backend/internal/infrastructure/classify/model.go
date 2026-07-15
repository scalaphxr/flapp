package classify

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
)

// ─────────────────────────────────────────────────────────────────────────────
// Структуры модели
// ─────────────────────────────────────────────────────────────────────────────

// ClassifyModel — обученная конфигурация классификатора.
// Генерируется cmd/dataset-analyzer, загружается при старте рантайма.
type ClassifyModel struct {
	Version     string  `json:"version"`
	BuildDate   string  `json:"buildDate"`
	DatasetSize int     `json:"datasetSize"`
	TrainSize   int     `json:"trainSize"`
	TestSize    int     `json:"testSize"`
	TestAccuracy float64 `json:"testAccuracy"`

	// TokenWeights: токен → категория → вес (= P(cat|token) * специфичность токена).
	// Вес отражает «насколько этот токен указывает именно на эту категорию».
	TokenWeights   map[string]map[string]float64 `json:"tokenWeights"`

	// ConflictTokens: токены, значимые сразу в нескольких категориях.
	// По ним решает акустика, а не имя.
	ConflictTokens map[string]bool `json:"conflictTokens"`

	// FolderSynonyms: ключевые слова имени папки (нижний регистр) → категория.
	// Самый надёжный сигнал.
	FolderSynonyms map[string]string `json:"folderSynonyms"`

	// AcousticProfiles: категория → профиль акустических признаков.
	AcousticProfiles map[string]AcousticProfile `json:"acousticProfiles"`
}

// AcousticProfile — статистический профиль признаков для одной категории.
type AcousticProfile struct {
	Count    int                     `json:"count"`
	Features map[string]FeatureStats `json:"features"` // имя признака → статистика
}

// FeatureStats — описательная статистика одного признака.
type FeatureStats struct {
	Mean   float64 `json:"mean"`
	Stddev float64 `json:"stddev"`
	Median float64 `json:"median"` // P50
	P10    float64 `json:"p10"`    // 10-й перцентиль
	P90    float64 `json:"p90"`    // 90-й перцентиль
}

// ClassifyResult — полный результат классификации с уверенностью.
type ClassifyResult struct {
	Category    string  `json:"category"`
	Confidence  float64 `json:"confidence"`  // 0..1
	Source      string  `json:"source"`      // "path"|"name"|"acoustic"|"merged"|"fallback"
	NeedsReview bool    `json:"needsReview"` // выделить для ручной проверки
	Alternative string  `json:"alternative,omitempty"` // вторая кандидатура
}

// ─────────────────────────────────────────────────────────────────────────────
// Загрузка / сохранение
// ─────────────────────────────────────────────────────────────────────────────

// LoadModel читает JSON-модель с диска.
func LoadModel(path string) (*ClassifyModel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m ClassifyModel
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// SaveModel записывает модель в JSON-файл.
func SaveModel(m *ClassifyModel, path string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ─────────────────────────────────────────────────────────────────────────────
// Токенизатор — общий для CLI-анализатора и рантайма
// ─────────────────────────────────────────────────────────────────────────────

// Разделители в именах файлов: нецифро-не-буквенные символы.
var sepRe = regexp.MustCompile(`[^a-z0-9]+`)

// noiseTokens — частые слова, не несущие семантической нагрузки.
var noiseTokens = map[string]bool{
	"wav": true, "mp3": true, "flac": true, "aif": true, "aiff": true,
	"the": true, "and": true, "of": true, "a": true, "an": true,
	"sample": true, "samples": true, "pack": true, "kit": true,
	"free": true, "vol": true, "version": true,
}

// TokenizeFilename разбивает имя файла (без пути и расширения) на токены.
// Правила:
//   - нижний регистр
//   - разбивка по любым нецифро-не-буквенным символам
//   - дополнительно: переходы букв↔цифры («snare01» → «snare», «01»)
//   - фильтр: длина ≤1, чисто числовые (кроме «808»), шумовые токены
//
// Токены длиной ≤2 при такой токенизации уже являются отдельными словами
// (т.к. разбиваем по разделителям) — правило word-boundary соблюдается автоматически.
func TokenizeFilename(name string) []string {
	// Убираем расширение
	if idx := strings.LastIndexByte(name, '.'); idx > 0 {
		name = name[:idx]
	}
	name = strings.ToLower(name)

	// Заменяем все разделители на пробел
	name = sepRe.ReplaceAllString(name, " ")

	// Дополнительно: вставляем пробел на границах буква↔цифра
	// чтобы «snare01» → «snare 01», «808bd» → «808 bd»
	var sb strings.Builder
	runes := []rune(name)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			prevIsLetter := (prev >= 'a' && prev <= 'z')
			currIsDigit := (r >= '0' && r <= '9')
			prevIsDigit := (prev >= '0' && prev <= '9')
			currIsLetter := (r >= 'a' && r <= 'z')
			if (prevIsLetter && currIsDigit) || (prevIsDigit && currIsLetter) {
				sb.WriteRune(' ')
			}
		}
		sb.WriteRune(r)
	}
	name = sb.String()

	parts := strings.Fields(name)
	var tokens []string
	for _, p := range parts {
		if len(p) <= 1 {
			continue // однобуквенные — шум (ноты C, D, Eb…)
		}
		if isAllDigits(p) && p != "808" {
			continue // чисто числовые (01, 02, 140…) кроме «808»
		}
		if noiseTokens[p] {
			continue
		}
		tokens = append(tokens, p)
	}
	return tokens
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────────────────────
// Базовый словарь синонимов имён папок (используется и в CLI, и в рантайме)
// ─────────────────────────────────────────────────────────────────────────────

// BaseFolderSynonyms — стартовое отображение ключевых слов папки → категория.
// Ключи: нижний регистр. Значения: строковые имена категорий domain.Category.
// Приоритет: длинные совпадения проверяются первыми (в runtime обходим по длине убывая).
var BaseFolderSynonyms = map[string]string{
	// 808 / Sub Bass
	// "bd" = TR-808 bass drum used as the 808 sub instrument (confirmed
	// against real library data: every "BD" folder across multiple
	// independent kits contains 808/sub content, e.g. "attack 808.wav",
	// "808 bricks.wav", "bwheezySub.wav") — not a Kick abbreviation here.
	"808": "808", "808s": "808", "sub": "808", "sub bass": "808",
	"subbass": "808", "bass sub": "808", "bd": "808",

	// Kick
	"kick": "Kick", "kicks": "Kick", "kick drum": "Kick", "kickdrum": "Kick",
	"bassdrum": "Kick", "bass drum": "Kick",

	// Snare
	"snare": "Snare", "snares": "Snare", "snr": "Snare",

	// Clap
	"clap": "Clap", "claps": "Clap", "handclap": "Clap",
	"rimshot": "Clap", "rimshots": "Clap", "rimz": "Clap",
	"rims": "Clap", "rim": "Clap", "rim shots": "Clap",

	// Hi-Hat (closed)
	"hihat": "Hi-Hat", "hi-hat": "Hi-Hat", "hi hat": "Hi-Hat", "hi-hats": "Hi-Hat",
	"hihats": "Hi-Hat", "hat": "Hi-Hat", "hats": "Hi-Hat",
	"closed hat": "Hi-Hat", "closed hihat": "Hi-Hat",
	"closed": "Hi-Hat",

	// Open Hat / Cymbal
	"open hat": "Open Hat", "openhat": "Open Hat", "open hats": "Open Hat",
	"oh": "Open Hat", "open hi hat": "Open Hat", "op hat": "Open Hat",
	"crash": "Open Hat", "ride": "Open Hat", "cymbal": "Open Hat",
	"cymbals": "Open Hat",

	// Perc
	"perc": "Perc", "percussion": "Perc", "percs": "Perc",
	"shaker": "Perc", "tom": "Perc", "toms": "Perc",
	"tambourine": "Perc", "conga": "Perc", "bongo": "Perc",

	// Vox
	"vox": "Vox", "vocal": "Vox", "vocals": "Vox",
	"voice": "Vox", "voices": "Vox", "adlib": "Vox", "ad lib": "Vox",
	"chant": "Vox",

	// FX
	"fx": "FX", "sfx": "FX", "riser": "FX", "risers": "FX",
	"downlifter": "FX", "impact": "FX", "sweep": "FX",
	"transition": "FX", "texture": "FX", "foley": "FX",

	// Loop (melodic)
	"loop": "Loop", "loops": "Loop", "loopkit": "Loop", "melody loop": "Loop",
	"bell": "Loop", "bells": "Loop",

	// Drum Loop
	"drum loop": "Drum Loop", "drum loops": "Drum Loop",
	"drumloop": "Drum Loop", "breaks": "Drum Loop",
	"break": "Drum Loop", "breakbeats": "Drum Loop",
}
