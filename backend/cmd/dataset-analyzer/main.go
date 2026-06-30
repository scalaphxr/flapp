// dataset-analyzer — CLI-утилита для статистического анализа размеченного
// датасета драмкитов и генерации файла classify_model.json.
//
// Запуск:
//
//	dataset-analyzer -dir "E:\FL\Data\Patches\DRUMKITS" -out classify_model.json
//	dataset-analyzer -dir "E:\FL\Data\Patches\DRUMKITS" -out classify_model.json -workers 8
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/audio"
	"github.com/flapp/core/internal/infrastructure/classify"
)

func main() {
	dir := flag.String("dir", "", "путь к папке с драмкитами (обязательно)")
	out := flag.String("out", "classify_model.json", "путь к выходному файлу модели")
	workers := flag.Int("workers", runtime.NumCPU(), "количество параллельных воркеров анализа аудио")
	testRatio := flag.Float64("test", 0.2, "доля тестовой выборки (0..1)")
	flag.Parse()

	if *dir == "" {
		fmt.Fprintln(os.Stderr, "ошибка: укажите -dir")
		flag.Usage()
		os.Exit(1)
	}

	fmt.Printf("Dataset Analyzer for Flapp\n")
	fmt.Printf("===========================\n")
	fmt.Printf("Директория:  %s\n", *dir)
	fmt.Printf("Выход:       %s\n", *out)
	fmt.Printf("Воркеры:     %d\n", *workers)
	fmt.Printf("Тест-доля:   %.0f%%\n\n", *testRatio*100)

	// ── Шаг A: разметка датасета ──────────────────────────────────────────────
	fmt.Println("Шаг A — разметка по именам папок...")
	labeled, unknown := walkDataset(*dir)
	if len(labeled) == 0 {
		fmt.Fprintln(os.Stderr, "не найдено размеченных аудиофайлов, проверьте путь")
		os.Exit(1)
	}
	printDistribution(labeled, unknown)

	// ── Стратифицированный split 80/20 ────────────────────────────────────────
	trainSet, testSet := stratifiedSplit(labeled, *testRatio)
	fmt.Printf("Разбивка: %d обучение / %d тест\n\n", len(trainSet), len(testSet))

	// ── Шаг B: словарь токенов из имён ────────────────────────────────────────
	fmt.Println("Шаг B — анализ токенов имён файлов...")
	tokenWeights, conflictTokens := buildTokenWeights(trainSet)
	printTopTokens(tokenWeights, conflictTokens)

	// ── Шаг C: акустические профили ───────────────────────────────────────────
	fmt.Printf("\nШаг C — акустический анализ %d обучающих файлов (воркеры: %d)...\n",
		len(trainSet), *workers)
	trainFeats := analyzeFiles(trainSet, *workers)
	profiles := buildAcousticProfiles(trainFeats)
	printAcousticProfiles(profiles)

	// ── Шаг D: сборка и сохранение модели ────────────────────────────────────
	model := &classify.ClassifyModel{
		Version:          "1.0",
		BuildDate:        time.Now().Format("2006-01-02"),
		DatasetSize:      len(labeled),
		TrainSize:        len(trainSet),
		TestSize:         len(testSet),
		TokenWeights:     tokenWeights,
		ConflictTokens:   conflictTokens,
		FolderSynonyms:   classify.BaseFolderSynonyms,
		AcousticProfiles: profiles,
	}

	// ── Валидация на тестовой выборке ─────────────────────────────────────────
	fmt.Printf("\nВалидация на %d тестовых файлах...\n", len(testSet))
	testFeats := analyzeFiles(testSet, *workers)
	accuracy, cm := evaluate(model, testFeats)
	model.TestAccuracy = accuracy

	printConfusionMatrix(cm)
	fmt.Printf("\n→ Общая точность на тест-выборке: %.1f%% (%d/%d)\n",
		accuracy*100, int(accuracy*float64(len(testFeats))), len(testFeats))

	if err := classify.SaveModel(model, *out); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка записи модели: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\nМодель сохранена в: %s\n", *out)
}

// ─────────────────────────────────────────────────────────────────────────────
// Структуры
// ─────────────────────────────────────────────────────────────────────────────

type labeledFile struct {
	path     string
	name     string // только имя файла (без пути)
	category string // domain.Category как строка
	relPath  string // путь относительно корня датасета
}

type analyzedFile struct {
	lf       labeledFile
	features domain.AudioFeatures
}

// ─────────────────────────────────────────────────────────────────────────────
// Шаг A: обход директории и разметка
// ─────────────────────────────────────────────────────────────────────────────

// audioExts — поддерживаемые форматы (те, что умеет audio.Analyzer).
var audioExts = map[string]bool{
	".wav": true, ".aiff": true, ".aif": true, ".mp3": true,
}

func walkDataset(root string) (labeled []labeledFile, unknown int) {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !audioExts[ext] {
			return nil
		}
		relPath, _ := filepath.Rel(root, path)
		cat := inferCategory(relPath)
		if cat == "" {
			unknown++
			return nil
		}
		labeled = append(labeled, labeledFile{
			path:     path,
			name:     d.Name(),
			category: cat,
			relPath:  relPath,
		})
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка обхода директории: %v\n", err)
	}
	return labeled, unknown
}

// inferCategory определяет категорию по сегментам пути (снизу вверх).
// Возвращает пустую строку, если не удалось определить.
func inferCategory(relPath string) string {
	parts := strings.FieldsFunc(relPath, func(r rune) bool { return r == '/' || r == '\\' })
	// Начинаем с предпоследнего (ближайший к файлу каталог)
	for i := len(parts) - 2; i >= 0; i-- {
		seg := strings.ToLower(strings.TrimSpace(parts[i]))
		// Ищем самое длинное совпадение
		bestLen, bestCat := 0, ""
		for key, cat := range classify.BaseFolderSynonyms {
			if (seg == key || strings.Contains(seg, key)) && len(key) > bestLen {
				bestLen = len(key)
				bestCat = cat
			}
		}
		if bestCat != "" {
			return bestCat
		}
	}
	return ""
}

func printDistribution(labeled []labeledFile, unknown int) {
	counts := make(map[string]int)
	for _, f := range labeled {
		counts[f.category]++
	}
	total := len(labeled)
	fmt.Printf("Распределение датасета (всего %d файлов, пропущено unknown: %d):\n", total, unknown)
	cats := sortedKeys(counts)
	for _, cat := range cats {
		n := counts[cat]
		fmt.Printf("  %-12s %5d  (%.1f%%)\n", cat+":", n, float64(n)/float64(total)*100)
	}
	fmt.Println()
}

// ─────────────────────────────────────────────────────────────────────────────
// Разбивка на train/test (стратифицированная)
// ─────────────────────────────────────────────────────────────────────────────

func stratifiedSplit(files []labeledFile, testRatio float64) (train, test []labeledFile) {
	byCategory := make(map[string][]labeledFile)
	for _, f := range files {
		byCategory[f.category] = append(byCategory[f.category], f)
	}

	rng := rand.New(rand.NewSource(42)) // детерминированное начальное состояние
	for _, group := range byCategory {
		// Перемешиваем внутри категории
		rng.Shuffle(len(group), func(i, j int) { group[i], group[j] = group[j], group[i] })
		n := int(math.Round(float64(len(group)) * testRatio))
		if n < 1 && len(group) > 0 {
			n = 1
		}
		test = append(test, group[:n]...)
		train = append(train, group[n:]...)
	}
	return train, test
}

// ─────────────────────────────────────────────────────────────────────────────
// Шаг B: токены и веса
// ─────────────────────────────────────────────────────────────────────────────

// buildTokenWeights строит карту токен → категория → вес.
// Вес = P(cat|token) * специфичность(token).
// conflictTokens: token → true, если специфичность < 0.6.
func buildTokenWeights(files []labeledFile) (
	weights map[string]map[string]float64,
	conflicts map[string]bool,
) {
	// Считаем freq[token][category] = число файлов категории с этим токеном
	freq := make(map[string]map[string]int)
	df := make(map[string]int) // document frequency: число файлов с токеном

	for _, f := range files {
		tokens := classify.TokenizeFilename(f.name)
		seen := make(map[string]bool)
		for _, tok := range tokens {
			if seen[tok] {
				continue // один токен — один раз на файл
			}
			seen[tok] = true
			if freq[tok] == nil {
				freq[tok] = make(map[string]int)
			}
			freq[tok][f.category]++
			df[tok]++
		}
	}

	N := float64(len(files))
	weights = make(map[string]map[string]float64)
	conflicts = make(map[string]bool)

	for tok, catCounts := range freq {
		dfTok := float64(df[tok])
		// IDF-буст: редкие токены несут больше информации
		idf := math.Log2(N/dfTok + 1)

		catWeights := make(map[string]float64)
		maxP := 0.0
		for cat, cnt := range catCounts {
			p := float64(cnt) / dfTok // P(cat|token)
			catWeights[cat] = p * idf
			if p > maxP {
				maxP = p
			}
		}
		// Специфичность: насколько токен «принадлежит» одной категории
		if maxP < 0.6 {
			conflicts[tok] = true
		}

		// Нормируем веса так, чтобы максимум = maxP * idf
		// (уже нормировано — сохраняем как есть)
		weights[tok] = catWeights
	}
	return weights, conflicts
}

func printTopTokens(weights map[string]map[string]float64, conflicts map[string]bool) {
	// Для каждой категории — топ-10 дискриминативных токенов
	// Агрегируем: для каждого токена ищем категорию с максимальным весом
	type entry struct {
		tok    string
		weight float64
	}
	byCat := make(map[string][]entry)
	for tok, catW := range weights {
		for cat, w := range catW {
			byCat[cat] = append(byCat[cat], entry{tok, w})
		}
	}

	// Отмечаем конфликтный «bd»
	fmt.Println("Топ токены по категориям (топ-10 по весу):")
	for _, cat := range sortedCats() {
		entries := byCat[cat]
		sort.Slice(entries, func(i, j int) bool { return entries[i].weight > entries[j].weight })
		var parts []string
		for i, e := range entries {
			if i >= 10 {
				break
			}
			s := fmt.Sprintf("%s(%.2f)", e.tok, e.weight)
			if conflicts[e.tok] {
				s += "⚠"
			}
			parts = append(parts, s)
		}
		fmt.Printf("  %-12s %s\n", cat+":", strings.Join(parts, ", "))
	}

	// Особое предупреждение про «bd»
	if bdWeights, ok := weights["bd"]; ok && len(bdWeights) > 1 {
		fmt.Println("\n⚠  Токен «bd» встречается в нескольких категориях:")
		for cat, w := range bdWeights {
			fmt.Printf("     %-12s вес=%.2f\n", cat, w)
		}
		fmt.Println("   Проверьте, как именно в вашем датасете называются 808 vs Kick.")
	}
	fmt.Println()
}

// ─────────────────────────────────────────────────────────────────────────────
// Параллельный аудиоанализ
// ─────────────────────────────────────────────────────────────────────────────

func analyzeFiles(files []labeledFile, numWorkers int) []analyzedFile {
	analyzer := audio.NewAnalyzer()
	ctx := context.Background()

	type job struct{ lf labeledFile }
	type result struct {
		lf  labeledFile
		f   domain.AudioFeatures
		err error
	}

	jobs := make(chan job, len(files))
	results := make(chan result, len(files))

	// Отправляем задачи
	for _, lf := range files {
		jobs <- job{lf}
	}
	close(jobs)

	var done atomic.Int64
	total := int64(len(files))
	start := time.Now()

	// Запускаем воркеры
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				feat, _, err := analyzer.AnalyzeAll(ctx, j.lf.path)
				results <- result{j.lf, feat, err}
				n := done.Add(1)
				if n%200 == 0 || n == total {
					elapsed := time.Since(start).Seconds()
					rate := float64(n) / elapsed
					fmt.Printf("  прогресс: %d/%d (%.0f файл/с)\r", n, total, rate)
				}
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var analyzed []analyzedFile
	for r := range results {
		if r.err == nil {
			analyzed = append(analyzed, analyzedFile{r.lf, r.f})
		}
	}
	fmt.Printf("\n  проанализировано %d/%d файлов за %.1fs\n",
		len(analyzed), len(files), time.Since(start).Seconds())
	return analyzed
}

// ─────────────────────────────────────────────────────────────────────────────
// Шаг C: акустические профили
// ─────────────────────────────────────────────────────────────────────────────

func buildAcousticProfiles(files []analyzedFile) map[string]classify.AcousticProfile {
	// Накапливаем значения признаков по категориям
	type featSlices struct {
		duration, centroid, zcr, lowR, highR, attack float64
		flatness, crest, decay, onsets, subBass       float64
	}
	raw := make(map[string][]featSlices)

	for _, af := range files {
		if !af.features.Analyzed {
			continue
		}
		f := af.features
		raw[af.lf.category] = append(raw[af.lf.category], featSlices{
			duration: f.DurationSeconds,
			centroid: f.SpectralCentroid,
			zcr:      f.ZeroCrossRate,
			lowR:     f.LowEnergyRatio,
			highR:    f.HighEnergyRatio,
			attack:   f.AttackTime,
			flatness: f.SpectralFlatness,
			crest:    f.CrestFactor,
			decay:    f.DecayRate,
			onsets:   float64(f.OnsetCount),
			subBass:  f.SubBassRatio,
		})
	}

	profiles := make(map[string]classify.AcousticProfile)
	for cat, slices := range raw {
		n := len(slices)
		featureNames := []string{
			"duration", "centroid", "zcr", "lowRatio", "highRatio",
			"attack", "flatness", "crest", "decay", "onsets", "subBass",
		}
		getterFuncs := []func(featSlices) float64{
			func(s featSlices) float64 { return s.duration },
			func(s featSlices) float64 { return s.centroid },
			func(s featSlices) float64 { return s.zcr },
			func(s featSlices) float64 { return s.lowR },
			func(s featSlices) float64 { return s.highR },
			func(s featSlices) float64 { return s.attack },
			func(s featSlices) float64 { return s.flatness },
			func(s featSlices) float64 { return s.crest },
			func(s featSlices) float64 { return s.decay },
			func(s featSlices) float64 { return s.onsets },
			func(s featSlices) float64 { return s.subBass },
		}

		featStats := make(map[string]classify.FeatureStats)
		for fi, fname := range featureNames {
			vals := make([]float64, n)
			for i, s := range slices {
				vals[i] = getterFuncs[fi](s)
			}
			featStats[fname] = computeStats(vals)
		}
		profiles[cat] = classify.AcousticProfile{Count: n, Features: featStats}
	}
	return profiles
}

// computeStats вычисляет описательную статистику набора значений.
func computeStats(vals []float64) classify.FeatureStats {
	if len(vals) == 0 {
		return classify.FeatureStats{}
	}
	sorted := make([]float64, len(vals))
	copy(sorted, vals)
	sort.Float64s(sorted)

	n := float64(len(vals))
	mean := 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= n

	variance := 0.0
	for _, v := range vals {
		d := v - mean
		variance += d * d
	}
	if n > 1 {
		variance /= n - 1
	}

	return classify.FeatureStats{
		Mean:   mean,
		Stddev: math.Sqrt(variance),
		Median: percentile(sorted, 0.50),
		P10:    percentile(sorted, 0.10),
		P90:    percentile(sorted, 0.90),
	}
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	frac := idx - float64(lo)
	return sorted[lo]*(1-frac) + sorted[hi]*frac
}

func printAcousticProfiles(profiles map[string]classify.AcousticProfile) {
	fmt.Println("\nАкустические профили (медиана [P10..P90]):")
	featOrder := []string{"duration", "centroid", "zcr", "lowRatio", "subBass", "flatness", "decay"}
	for _, cat := range sortedCats() {
		p, ok := profiles[cat]
		if !ok {
			continue
		}
		fmt.Printf("  %-12s n=%-5d", cat+":", p.Count)
		for _, fname := range featOrder {
			fs := p.Features[fname]
			fmt.Printf("  %s=%.2f[%.2f..%.2f]", fname[:3], fs.Median, fs.P10, fs.P90)
		}
		fmt.Println()
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Валидация: confusion matrix
// ─────────────────────────────────────────────────────────────────────────────

type confusionMatrix struct {
	cats   []string
	matrix map[string]map[string]int // true → pred → count
}

func evaluate(m *classify.ClassifyModel, testFiles []analyzedFile) (accuracy float64, cm confusionMatrix) {
	mc := classify.NewModelClassifier(m)
	cats := sortedCats()
	cm.cats = cats
	cm.matrix = make(map[string]map[string]int)
	for _, c := range cats {
		cm.matrix[c] = make(map[string]int)
	}

	correct := 0
	for _, af := range testFiles {
		r := mc.ClassifyFull(af.lf.name, af.lf.relPath, af.features)
		trueC := af.lf.category
		predC := r.Category
		cm.matrix[trueC][predC]++
		if trueC == predC {
			correct++
		}
	}
	if len(testFiles) > 0 {
		accuracy = float64(correct) / float64(len(testFiles))
	}
	return accuracy, cm
}

func printConfusionMatrix(cm confusionMatrix) {
	cats := cm.cats
	// Заголовок
	fmt.Println("\nConfusion Matrix (строки = истинная, столбцы = предсказанная):")
	header := fmt.Sprintf("%-14s", "")
	for _, c := range cats {
		abbr := catAbbr(c)
		header += fmt.Sprintf(" %5s", abbr)
	}
	fmt.Println(header)
	fmt.Println(strings.Repeat("-", len(header)))

	for _, trueC := range cats {
		row := fmt.Sprintf("%-14s", catAbbr(trueC)+":")
		total := 0
		for _, cnt := range cm.matrix[trueC] {
			total += cnt
		}
		for _, predC := range cats {
			n := cm.matrix[trueC][predC]
			if n == 0 {
				row += fmt.Sprintf(" %5s", "·")
			} else if trueC == predC {
				row += fmt.Sprintf(" \033[32m%5d\033[0m", n) // зелёный для диагонали
			} else {
				row += fmt.Sprintf(" \033[31m%5d\033[0m", n) // красный для ошибок
			}
		}
		if total > 0 {
			correct := cm.matrix[trueC][trueC]
			row += fmt.Sprintf("  acc=%.0f%%", float64(correct)/float64(total)*100)
		}
		fmt.Println(row)
	}

	// Главные путаницы
	fmt.Println("\nГлавные ошибки:")
	type errEntry struct {
		true, pred string
		count      int
	}
	var errs []errEntry
	for trueC, preds := range cm.matrix {
		for predC, cnt := range preds {
			if trueC != predC && cnt > 0 {
				errs = append(errs, errEntry{trueC, predC, cnt})
			}
		}
	}
	sort.Slice(errs, func(i, j int) bool { return errs[i].count > errs[j].count })
	for i, e := range errs {
		if i >= 10 {
			break
		}
		fmt.Printf("  %-12s → %-12s : %d случаев\n", e.true, e.pred, e.count)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Вспомогательные функции
// ─────────────────────────────────────────────────────────────────────────────

func sortedCats() []string {
	cats := []string{
		string(domain.Cat808),
		string(domain.CatKick),
		string(domain.CatSnare),
		string(domain.CatClap),
		string(domain.CatHiHat),
		string(domain.CatOpenHat),
		string(domain.CatPerc),
		string(domain.CatVox),
		string(domain.CatFX),
		string(domain.CatLoop),
		string(domain.CatDrumLoop),
	}
	return cats
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var catAbbrMap = map[string]string{
	"808":      "808",
	"Kick":     "Kck",
	"Snare":    "Snr",
	"Clap":     "Clp",
	"Hi-Hat":   "HHt",
	"Open Hat": "OHt",
	"Perc":     "Prc",
	"Vox":      "Vox",
	"FX":       "FX",
	"Loop":     "Lop",
	"Drum Loop": "DLp",
}

func catAbbr(cat string) string {
	if a, ok := catAbbrMap[cat]; ok {
		return a
	}
	if len(cat) > 5 {
		return cat[:5]
	}
	return cat
}
