package classify

import (
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// ─────────────────────────────────────────────────────────────────────────────
// ModelClassifier — классификатор на основе JSON-модели
// ─────────────────────────────────────────────────────────────────────────────

// ModelClassifier использует статистически построенную модель.
// Три каскадных сигнала: путь/папка → имя → акустика.
type ModelClassifier struct {
	model *ClassifyModel
	// Отсортированные по убыванию длины ключи для folderSynonyms —
	// позволяют предпочесть «open hat» перед «open» и «hat».
	folderKeys []string
}

// NewModelClassifier создаёт классификатор из загруженной модели.
func NewModelClassifier(m *ClassifyModel) *ModelClassifier {
	keys := make([]string, 0, len(m.FolderSynonyms))
	for k := range m.FolderSynonyms {
		keys = append(keys, k)
	}
	// Длинные совпадения проверяем первыми (специфичность)
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	return &ModelClassifier{model: m, folderKeys: keys}
}

// ─────────────────────────────────────────────────────────────────────────────
// Сигнал 1: папка / путь
// ─────────────────────────────────────────────────────────────────────────────

// folderSignal ищет категорию в сегментах пути (снизу вверх).
// Возвращает категорию и уверенность (9.0 если нашли, 0 иначе).
func (mc *ModelClassifier) folderSignal(relPath string) (string, float64) {
	// Разбиваем на компоненты, идём с предпоследнего (ближайшая к файлу папка)
	parts := strings.FieldsFunc(relPath, func(r rune) bool { return r == '/' || r == '\\' })
	for i := len(parts) - 2; i >= 0; i-- {
		seg := strings.ToLower(strings.TrimSpace(parts[i]))
		// Ищем самое длинное совпадение среди синонимов
		for _, key := range mc.folderKeys {
			if seg == key || strings.Contains(seg, key) {
				if catStr, ok := mc.model.FolderSynonyms[key]; ok {
					// Ближайшая папка сильнее: убывающий вес по глубине
					weight := 9.0 - float64(len(parts)-2-i)*1.5
					if weight < 4.0 {
						weight = 4.0
					}
					return catStr, weight
				}
			}
		}
	}
	return "", 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Сигнал 2: токены имени файла
// ─────────────────────────────────────────────────────────────────────────────

// nameSignal возвращает суммарный вес токенов по категориям.
func (mc *ModelClassifier) nameSignal(name string) map[string]float64 {
	scores := make(map[string]float64)
	tokens := TokenizeFilename(name)
	for _, tok := range tokens {
		weights, ok := mc.model.TokenWeights[tok]
		if !ok {
			continue
		}
		// Конфликтные токены получают дисконт 50%
		discount := 1.0
		if mc.model.ConflictTokens[tok] {
			discount = 0.5
		}
		for cat, w := range weights {
			scores[cat] += w * discount
		}
	}
	return scores
}

// ─────────────────────────────────────────────────────────────────────────────
// Сигнал 3: акустические признаки
// ─────────────────────────────────────────────────────────────────────────────

// акустические признаки с именами для сопоставления с профилем
type featEntry struct {
	name string
	val  float64
}

// acousticSignal вычисляет типичность признаков для каждой категории.
// Типичность: гауссово сходство val с медианой профиля.
// Возвращает карту cat → score (0..1).
func (mc *ModelClassifier) acousticSignal(f domain.AudioFeatures) map[string]float64 {
	scores := make(map[string]float64)
	if !f.Analyzed {
		return scores
	}

	// Базовые признаки (всегда вычислены при Analyzed=true)
	feats := []featEntry{
		{"duration", f.DurationSeconds},
		{"centroid", f.SpectralCentroid},
		{"zcr", f.ZeroCrossRate},
		{"lowRatio", f.LowEnergyRatio},
		{"highRatio", f.HighEnergyRatio},
		{"attack", f.AttackTime},
	}
	// Расширенные признаки (v2 — только если вычислены)
	if f.SpectralFlatness > 0 || f.OnsetCount > 0 {
		feats = append(feats,
			featEntry{"flatness", f.SpectralFlatness},
			featEntry{"crest", f.CrestFactor},
			featEntry{"decay", f.DecayRate},
			featEntry{"onsets", float64(f.OnsetCount)},
			featEntry{"subBass", f.SubBassRatio},
		)
	}

	for catStr, profile := range mc.model.AcousticProfiles {
		total, count := 0.0, 0
		for _, fe := range feats {
			fs, ok := profile.Features[fe.name]
			if !ok {
				continue
			}
			// σ ≈ половина межперцентильного диапазона (≈ 1.28σ для нормального распр.)
			sigma := (fs.P90 - fs.P10) / 2.56
			if sigma < 1e-6 {
				sigma = 1e-6
			}
			diff := (fe.val - fs.Median) / sigma
			typicality := math.Exp(-0.5 * diff * diff)
			total += typicality
			count++
		}
		if count > 0 {
			scores[catStr] = total / float64(count)
		}
	}
	return scores
}

// ─────────────────────────────────────────────────────────────────────────────
// Слияние сигналов → ClassifyResult
// ─────────────────────────────────────────────────────────────────────────────

// ClassifyFull выполняет полную классификацию с расчётом уверенности.
// Каскад: path (вес 3.0) → name (2.0) → acoustic (1.5).
func (mc *ModelClassifier) ClassifyFull(name, relPath string, f domain.AudioFeatures) ClassifyResult {
	const (
		wPath     = 3.0
		wName     = 2.0
		wAcoustic = 1.5
	)

	totals := make(map[string]float64)
	sources := make(map[string]string) // cat → source сигнала
	totalWeightUsed := 0.0

	// Сигнал 1: папка
	if catStr, w := mc.folderSignal(relPath); catStr != "" {
		totals[catStr] += w * wPath / 9.0 // нормируем к wPath
		sources[catStr] = "path"
		totalWeightUsed += wPath
	}

	// Сигнал 2: имя
	nameScores := mc.nameSignal(name)
	if len(nameScores) > 0 {
		best, bestScore := topScore(nameScores)
		if bestScore > 0 {
			totals[best] += bestScore * wName
			if _, has := sources[best]; !has {
				sources[best] = "name"
			}
			totalWeightUsed += wName
		}
	}

	// Сигнал 3: акустика
	acScores := mc.acousticSignal(f)
	if len(acScores) > 0 {
		best, bestScore := topScore(acScores)
		totals[best] += bestScore * wAcoustic
		if _, has := sources[best]; !has {
			sources[best] = "acoustic"
		}
		totalWeightUsed += wAcoustic
	}

	if len(totals) == 0 {
		return ClassifyResult{
			Category: string(domain.CatFX), Confidence: 0.1,
			Source: "fallback", NeedsReview: true,
		}
	}

	// Ищем победителя и вторую кандидатуру
	var ranked []struct {
		cat   string
		score float64
	}
	for cat, sc := range totals {
		ranked = append(ranked, struct {
			cat   string
			score float64
		}{cat, sc})
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	best := ranked[0].cat
	confidence := 0.0
	if totalWeightUsed > 0 {
		confidence = math.Min(1.0, ranked[0].score/totalWeightUsed)
	}

	alt := ""
	if len(ranked) > 1 {
		alt = ranked[1].cat
	}

	// Определяем источник: если несколько сигналов согласны — «merged»
	src := sources[best]
	agrees := 0
	for _, r := range ranked {
		if r.cat == best {
			agrees++
		}
	}
	if totalWeightUsed > wPath && src == "path" {
		src = "merged"
	} else if len(sources) > 1 {
		src = "merged"
	}

	needsReview := confidence < 0.45

	return ClassifyResult{
		Category:    best,
		Confidence:  confidence,
		Source:      src,
		NeedsReview: needsReview,
		Alternative: alt,
	}
}

// topScore возвращает категорию с максимальным счётом.
func topScore(scores map[string]float64) (string, float64) {
	best, bestSc := "", 0.0
	for k, v := range scores {
		if v > bestSc {
			bestSc = v
			best = k
		}
	}
	return best, bestSc
}

// ─────────────────────────────────────────────────────────────────────────────
// Поиск модели на диске при старте рантайма
// ─────────────────────────────────────────────────────────────────────────────

// FindModelPath ищет classify_model.json рядом с бинарником или в CWD.
func FindModelPath() (string, bool) {
	// 1. Рядом с исполняемым файлом (для продакшн-сайдкара)
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "classify_model.json")
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}
	// 2. Рабочая директория
	if _, err := os.Stat("classify_model.json"); err == nil {
		return "classify_model.json", true
	}
	return "", false
}
