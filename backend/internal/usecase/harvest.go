package usecase

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/audio"
	"github.com/flapp/core/internal/infrastructure/dedup"
)

// AnalysisCachePort is the subset of storage.AnalysisCacheRepo used here.
type AnalysisCachePort interface {
	GetCached(ctx context.Context, contentHash string) (domain.AudioFeatures, string, bool)
	SetCached(ctx context.Context, contentHash string, feat domain.AudioFeatures, fp string) error
}

// HarvestService runs the end-to-end import pipeline. Heavy per-file work
// (hashing, decoding, fingerprinting) is fanned out across separated I/O and
// CPU worker pools; the duplicate decision and database write are funnelled
// through a single goroutine so the dedup index and SQLite writer stay consistent.
//
// Worker separation rationale:
//   - ioWorkers read files (hash + PCM decode): I/O bound, keep low (≈2).
//   - cpuWorkers compute features (FFT, perceptual hash): CPU bound (≈cores-1).
type HarvestService struct {
	samples    domain.SampleRepository
	projects   domain.ProjectRepository
	extractor  domain.ArchiveExtractor
	flp        domain.FLPParser
	analyzer   domain.AudioAnalyzer
	classifier domain.Classifier
	tagger     domain.TagGenerator
	hasher     domain.Hasher
	cache      AnalysisCachePort // optional; nil = no cache

	storeDir   string
	tempDir    string
	workers    int
	ioWorkers  int
	cpuWorkers int
}

// HarvestDeps bundles the dependencies for a HarvestService.
type HarvestDeps struct {
	Samples       domain.SampleRepository
	Projects      domain.ProjectRepository
	Extractor     domain.ArchiveExtractor
	FLP           domain.FLPParser
	Analyzer      domain.AudioAnalyzer
	Classifier    domain.Classifier
	Tagger        domain.TagGenerator
	Hasher        domain.Hasher
	AnalysisCache AnalysisCachePort // optional content-addressed analysis cache
	StoreDir      string            // permanent home for unique sample copies
	TempDir       string            // scratch space for archive extraction
	Workers       int               // legacy: sets both io and cpu workers when > 0
	IOWorkers     int               // I/O pool size (0 → 2)
	CPUWorkers    int               // CPU pool size (0 → Workers or GOMAXPROCS-1)
}

// NewHarvestService wires a harvest service.
func NewHarvestService(d HarvestDeps) *HarvestService {
	io := d.IOWorkers
	if io < 1 {
		io = 2
	}
	cpu := d.CPUWorkers
	if cpu < 1 {
		cpu = d.Workers
	}
	if cpu < 1 {
		cpu = runtime.GOMAXPROCS(0) - 1
	}
	if cpu < 1 {
		cpu = 1
	}
	total := io + cpu
	return &HarvestService{
		samples:    d.Samples,
		projects:   d.Projects,
		extractor:  d.Extractor,
		flp:        d.FLP,
		analyzer:   d.Analyzer,
		classifier: d.Classifier,
		tagger:     d.Tagger,
		hasher:     d.Hasher,
		cache:      d.AnalysisCache,
		storeDir:   d.StoreDir,
		tempDir:    d.TempDir,
		workers:    total,
		ioWorkers:  io,
		cpuWorkers: cpu,
	}
}

// analyzed is a candidate after the parallel analysis stage.
type analyzed struct {
	cand        candidate
	md5, sha256 string
	fingerprint string
	features    domain.AudioFeatures
}

// candidate is a single audio file queued for the analysis stage.
type candidate struct {
	name     string
	path     string // current on-disk location (temp for extracted files)
	relPath  string // archive/folder-relative path for classification hints
	ext      string
	size     int64
	origin   domain.Origin
	srcLabel string
	srcPath  string
	temp     bool // path is a temp file to delete after processing
}

// arcRunStat хранит тайминги одного архива для отчёта.
type arcRunStat struct {
	name       string
	format     string        // "zip", "rar", "7z"
	audioKept  int           // аудио-файлов извлечено
	flpKept    int           // .flp-файлов извлечено
	otherKept  int           // извлечено, но не нужно харвесту (удалено сразу)
	extractDur time.Duration // суммарное время Extract() для архива
}

// harvestTimer агрегирует тайминги каждой стадии конвейера для профилирования.
type harvestTimer struct {
	start time.Time

	// Wallclock последовательных стадий
	scanWall    time.Duration
	extractWall time.Duration // суммарно все архивы (wallclock loop)
	flpWall     time.Duration // вся стадия FLP: парсинг + запись в БД
	processWall time.Duration // весь process() (IO + CPU + consumer суммарно)

	// CPU-время параллельных стадий — сумма по всем воркерам (atomic)
	hashNs    atomic.Int64 // IO-воркеры: время в hasher.Hashes()
	analyzeNs atomic.Int64 // CPU-воркеры: время в analyzer.AnalyzeAll()
	cacheHits atomic.Int64 // кол-во кэш-хитов (пропущенный анализ)

	// Wallclock подстадий сериального consumer (один поток — atomics не нужны)
	dedupWall    time.Duration
	classifyWall time.Duration
	tagWall      time.Duration
	storeWall    time.Duration
	copyWall     time.Duration

	totalFiles int

	// Детализация архивной стадии
	arcStats    []arcRunStat
	flpCount    int           // кол-во распарсенных FLP
	flpParseDur time.Duration // чистое время flp.Parse() без Upsert

	// Под-стадийные тайминги аудио-анализа (заполняются после process()).
	audioSub audio.SubStatsSnap
}

// printHarvestReport выводит в stderr таблицу с временем каждой стадии конвейера.
// CPU-сумма для параллельных стадий может превышать wallclock — это нормально при N воркерах.
func printHarvestReport(tmr *harvestTimer, ioWorkers, cpuWorkers int) {
	total := time.Since(tmr.start)
	files := tmr.totalFiles

	hashCPU := time.Duration(tmr.hashNs.Load())
	analyzeCPU := time.Duration(tmr.analyzeNs.Load())
	cacheHits := int(tmr.cacheHits.Load())

	pct := func(d time.Duration) float64 {
		if total == 0 {
			return 0
		}
		return 100 * float64(d) / float64(total)
	}
	hitPct := 0.0
	if files > 0 {
		hitPct = 100 * float64(cacheHits) / float64(files)
	}

	var b strings.Builder
	row := func(name string, d time.Duration) {
		fmt.Fprintf(&b, " %-34s %10s  %5.1f%%\n", name, d.Round(time.Millisecond), pct(d))
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "══════════════════════════════════════════════════════════\n")
	fmt.Fprintf(&b, " FLAPP HARVEST — тайминги стадий конвейера\n")
	fmt.Fprintf(&b, " Файлов: %d  │  Wallclock: %s\n", files, total.Round(time.Millisecond))
	fmt.Fprintf(&b, "══════════════════════════════════════════════════════════\n")
	fmt.Fprintf(&b, " %-34s %10s  %s\n", "Стадия", "Время", "% total")
	fmt.Fprintf(&b, " ────────────────────────────────────────────────────────\n")
	row("Сканирование", tmr.scanWall)
	row("Распаковка архивов", tmr.extractWall)
	row("Парс FLP", tmr.flpWall)
	fmt.Fprintf(&b, " ── process() [wallclock: %s] ────────────────────────\n", tmr.processWall.Round(time.Millisecond))
	fmt.Fprintf(&b, " %-34s %10s  (CPU-сумма ×%d воркеров)\n", "  Хэширование IO", hashCPU.Round(time.Millisecond), ioWorkers)
	fmt.Fprintf(&b, " %-34s %10s  (CPU-сумма ×%d воркеров)\n", "  Анализ аудио", analyzeCPU.Round(time.Millisecond), cpuWorkers)
	fmt.Fprintf(&b, "   └─ кэш-хиты: %d/%d (%.1f%%)\n", cacheHits, files, hitPct)
	fmt.Fprintf(&b, " ── consumer (серийный) ──────────────────────────────\n")
	row("  Dedup-проверка", tmr.dedupWall)
	row("  Классификация", tmr.classifyWall)
	row("  Автотеги", tmr.tagWall)
	row("  Запись в БД", tmr.storeWall)
	row("  Копирование файлов", tmr.copyWall)
	fmt.Fprintf(&b, "══════════════════════════════════════════════════════════\n")
	fmt.Fprintf(&b, " Wallclock ИТОГО: %s\n", total.Round(time.Millisecond))

	// Детализация аудио-анализа по под-стадиям.
	sub := tmr.audioSub
	nWAV := sub.CountWAV
	nMP3 := sub.CountMP3
	nDecoded := nWAV + nMP3
	if nDecoded > 0 {
		decWAV := time.Duration(sub.DecodeWAVNs)
		decMP3 := time.Duration(sub.DecodeMP3Ns)
		tDom := time.Duration(sub.TimeDomainNs)
		lwWin := time.Duration(sub.LoudestWinNs)
		specFFT := time.Duration(sub.SpectralFFTNs)
		fp := time.Duration(sub.FingerprintNs)
		sumKnown := decWAV + decMP3 + tDom + lwWin + specFFT + fp

		avgMs := func(d time.Duration, n int64) string {
			if n == 0 {
				return "—"
			}
			return (d / time.Duration(n)).Round(time.Microsecond*100).String()
		}
		pctSub := func(d time.Duration) float64 {
			if sumKnown == 0 {
				return 0
			}
			return 100 * float64(d) / float64(sumKnown)
		}

		avgSamples := sub.SampleCount / nDecoded
		avgDurMs := int64(float64(avgSamples) / 44100 * 1000) // грубо 44.1kHz

		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "── Аудио-анализ: разбивка под-стадий ──────────────────────────────────────\n")
		fmt.Fprintf(&b, " Декодировано: %d файлов (WAV/AIFF: %d, MP3: %d)\n", nDecoded, nWAV, nMP3)
		fmt.Fprintf(&b, " Среднее сэмплов/файл: %d (~%d ms)\n", avgSamples, avgDurMs)
		fmt.Fprintf(&b, " CPU-сумма всех под-стадий: %s  (×%d воркеров)\n", sumKnown.Round(time.Millisecond), cpuWorkers)
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, " %-28s  %10s  %8s  %5s\n", "Под-стадия", "CPU-сумма", "avg/файл", "%")
		fmt.Fprintf(&b, " ────────────────────────────────────────────────────────────────────────\n")
		subRow := func(name string, d time.Duration, n int64) {
			fmt.Fprintf(&b, " %-28s  %10s  %8s  %5.1f%%\n",
				name, d.Round(time.Millisecond), avgMs(d, n), pctSub(d))
		}
		subRow("Декод WAV/AIFF", decWAV, nWAV)
		subRow("Декод MP3", decMP3, nMP3)
		subRow("Time-domain (RMS/ZCR/peak)", tDom, nDecoded)
		subRow("LoudestWindow scan", lwWin, nDecoded)
		subRow("Спектр (FFT+centroid)", specFFT, nDecoded)
		subRow("Отпечаток (16×FFT)", fp, nWAV) // MP3 отпечаток не считается
		fmt.Fprintf(&b, " ────────────────────────────────────────────────────────────────────────\n")
		fmt.Fprintf(&b, " Σ учтённых: %s\n", sumKnown.Round(time.Millisecond))
	}

	// Детализация по архивам — полезна для гипотез: zip vs rar vs 7z, FLP vs аудио.
	if len(tmr.arcStats) > 0 {
		fmt.Fprintf(&b, "\n")
		fmt.Fprintf(&b, "── Детализация архивов ───────────────────────────────────\n")
		fmt.Fprintf(&b, " %-28s %-4s  %5s %4s %6s  %10s\n", "Архив", "Тип", "Аудио", "FLP", "Прочее", "Время")
		fmt.Fprintf(&b, " ──────────────────────────────────────────────────────\n")
		var arcTotal time.Duration
		for _, a := range tmr.arcStats {
			arcTotal += a.extractDur
			name := a.name
			if len(name) > 27 {
				name = "…" + name[len(name)-26:]
			}
			fmt.Fprintf(&b, " %-28s %-4s  %5d %4d %6d  %10s\n",
				name, a.format, a.audioKept, a.flpKept, a.otherKept, a.extractDur.Round(time.Millisecond))
		}
		fmt.Fprintf(&b, " ──────────────────────────────────────────────────────\n")
		fmt.Fprintf(&b, " %-28s       ИТОГО wallclock: %s  (loop: %s)\n", "", arcTotal.Round(time.Millisecond), tmr.extractWall.Round(time.Millisecond))

		if tmr.flpCount > 0 {
			avg := tmr.flpParseDur / time.Duration(tmr.flpCount)
			fmt.Fprintf(&b, "\n")
			fmt.Fprintf(&b, "── FLP-парсинг ───────────────────────────────────────────\n")
			fmt.Fprintf(&b, " Файлов: %d  │  чистый Parse: %s  │  avg: %s/файл\n",
				tmr.flpCount, tmr.flpParseDur.Round(time.Millisecond), avg.Round(time.Millisecond))
			fmt.Fprintf(&b, " Вся стадия FLP (вкл. Upsert): %s\n", tmr.flpWall.Round(time.Millisecond))
		}
	}

	fmt.Fprint(os.Stderr, b.String())
}

// Run executes a harvest described by req, publishing progress through report,
// and returns a result map carrying the DedupStats for the UI.
func (s *HarvestService) Run(ctx context.Context, req domain.HarvestRequest, report domain.ProgressReporter) (map[string]interface{}, error) {
	if err := os.MkdirAll(s.storeDir, 0o755); err != nil {
		return nil, err
	}
	runTemp := filepath.Join(s.tempDir, fmt.Sprintf("harvest-%d", time.Now().UnixNano()))
	if err := os.MkdirAll(runTemp, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(runTemp)

	acc := dedup.NewAccumulator()
	tmr := &harvestTimer{start: time.Now()}
	var t0 time.Time

	// 1. Scan inputs (0 -> 0.05).
	report.Set(0.0, "Сканирование", "Поиск файлов и папок")
	t0 = time.Now()
	scanned, err := scanInputs(req.Inputs, req.ExtraFormats)
	tmr.scanWall = time.Since(t0)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// 2. Extract archives (0.05 -> 0.20).
	candidates := make([]candidate, 0, len(scanned.audio))
	for _, d := range scanned.audio {
		candidates = append(candidates, candidate{
			name: d.name, path: d.path, relPath: d.relPath, ext: d.ext, size: d.size,
			origin: domain.OriginFolder, srcLabel: "папка", srcPath: filepath.Dir(d.path),
		})
	}
	extractedProjects := make([]discovered, 0)
	if len(scanned.archives) > 0 {
		report.Set(0.05, "Распаковка архивов", "")
		t0 = time.Now()
		s.extractArchivesParallel(ctx, scanned.archives, runTemp, req.ExtraFormats,
			&candidates, &extractedProjects, tmr, acc, report)
		tmr.extractWall = time.Since(t0)
	}

	// 3. Parse FL Studio projects (0.20 -> 0.30).
	allProjects := append(append([]discovered{}, scanned.projects...), extractedProjects...)
	referenced := map[string]bool{} // basenames referenced by any project
	if len(allProjects) > 0 {
		report.Set(0.20, "Анализ проектов", "")
		t0 = time.Now()
		for i, pf := range allProjects {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			report.Detail(pf.name)
			tParse := time.Now()
			var proj *domain.Project
			var perr error
			if len(pf.rawBytes) > 0 {
				// FLP уже в памяти (из архива): парсим без чтения с диска.
				proj, perr = s.flp.ParseBytes(ctx, pf.rawBytes, pf.name, pf.path)
			} else {
				proj, perr = s.flp.Parse(ctx, pf.path)
			}
			tmr.flpParseDur += time.Since(tParse)
			tmr.flpCount++
			if perr == nil && proj != nil {
				proj.Size = pf.size
				if _, err := s.projects.Upsert(ctx, proj); err == nil {
					acc.NoteProject()
				}
				for _, sp := range proj.SamplePaths {
					referenced[strings.ToLower(filepath.Base(sp))] = true
				}
			}
			report.Set(0.20+0.10*float64(i+1)/float64(len(allProjects)), "Анализ проектов", pf.name)
		}
		tmr.flpWall = time.Since(t0)
	}

	// If a drumkits directory is set, walk it and add files whose basename
	// matches a sample referenced by any parsed project.
	if req.DrumkitsDir != "" && len(referenced) > 0 {
		drumkitCands := s.searchDrumkitsForReferenced(req.DrumkitsDir, referenced, req.ExtraFormats)
		candidates = append(candidates, drumkitCands...)
	}

	// Optionally restrict to samples referenced by a project, and tag the
	// origin of those that are referenced.
	candidates = s.applyProjectLinkage(candidates, referenced, req.OnlyFromFLP)

	// 4. Analyze + dedup + store (0.30 -> 1.0).
	report.Set(0.30, "Анализ звуков", "")
	t0 = time.Now()
	stats, err := s.process(ctx, candidates, req, acc, report, tmr)
	tmr.processWall = time.Since(t0)
	if err != nil {
		return nil, err
	}

	// Извлекаем под-стадийные тайминги аудио-анализатора (type assertion к конкретному типу).
	if a, ok := s.analyzer.(*audio.Analyzer); ok {
		tmr.audioSub = a.SubStats()
	}

	report.Set(1.0, "Готово", fmt.Sprintf("%d уникальных • %d дублей", stats.UniqueFiles, stats.Duplicates))
	printHarvestReport(tmr, s.ioWorkers, s.cpuWorkers)
	return map[string]interface{}{
		"stats": stats,
	}, nil
}

// extractArchivesParallel обрабатывает список архивов параллельно.
// Число одновременных горутин ограничено s.cpuWorkers (или 2 при нехватке).
// Результаты собираются в исходном порядке: кандидаты и проекты добавляются
// в те же слайсы, что и при последовательной обработке.
func (s *HarvestService) extractArchivesParallel(
	ctx context.Context,
	archives []discovered,
	runTemp string,
	extra bool,
	candidates *[]candidate,
	projects *[]discovered,
	tmr *harvestTimer,
	acc *dedup.Accumulator,
	report domain.ProgressReporter,
) {
	n := len(archives)

	type arcResult struct {
		cands []candidate
		projs []discovered
		stat  arcRunStat
	}

	results := make([]arcResult, n)

	// Число параллельных воркеров: используем CPU-пул, но не более числа архивов.
	workers := s.cpuWorkers
	if workers < 2 {
		workers = 2
	}
	if workers > n {
		workers = n
	}

	work := make(chan int, n)
	for i := range archives {
		work <- i
	}
	close(work)

	var mu sync.Mutex
	completed := 0
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				if ctx.Err() != nil {
					return
				}
				arc := archives[i]
				got, projs, stat := s.extractArchive(ctx, arc, runTemp, extra)
				results[i] = arcResult{cands: got, projs: projs, stat: stat}

				mu.Lock()
				completed++
				pct := 0.05 + 0.15*float64(completed)/float64(n)
				mu.Unlock()
				report.Set(pct, "Распаковка архивов", arc.name)
			}
		}()
	}
	wg.Wait()

	// Собираем результаты в исходном порядке (детерминизм + acc без гонок).
	for _, r := range results {
		*candidates = append(*candidates, r.cands...)
		*projects = append(*projects, r.projs...)
		tmr.arcStats = append(tmr.arcStats, r.stat)
		acc.NoteArchive()
	}
}

// extractArchive unpacks one archive into runTemp, returning audio candidates,
// .flp project data, and per-archive timing statistics.
//
// Оптимизация FLP: вместо того чтобы писать .flp на диск и потом читать его обратно
// в стадии 3, читаем содержимое сразу в память и удаляем temp-файл. Стадия 3 получает
// данные через discovered.rawBytes и вызывает ParseBytes — disk round-trip устранён.
func (s *HarvestService) extractArchive(ctx context.Context, arc discovered, runTemp string, extra bool) ([]candidate, []discovered, arcRunStat) {
	dest := filepath.Join(runTemp, sanitizeName(arc.name))
	audioExts := audioExtSet(extra)

	arcExt := strings.ToLower(filepath.Ext(arc.name))
	stat := arcRunStat{
		name:   arc.name,
		format: strings.TrimPrefix(arcExt, "."),
	}

	var cands []candidate
	var projs []discovered

	t := time.Now()
	_ = s.extractor.Extract(ctx, arc.path, dest, func(e domain.ExtractedFile) error {
		ext := strings.ToLower(filepath.Ext(e.Name))
		switch {
		case ext == ".flp":
			// Читаем FLP в память и сразу удаляем temp-файл: стадия 3 парсит из буфера.
			data, err := os.ReadFile(e.TempPath)
			os.Remove(e.TempPath)
			if err != nil {
				return nil
			}
			stat.flpKept++
			projs = append(projs, discovered{
				rawBytes: data,
				path:     arc.path, // архив как логический источник
				relPath:  e.RelPath,
				name:     e.Name,
				ext:      ext,
				size:     e.Size,
			})
		case audioExts[ext]:
			stat.audioKept++
			cands = append(cands, candidate{
				name: e.Name, path: e.TempPath, relPath: e.RelPath, ext: ext, size: e.Size,
				origin: domain.OriginArchive, srcLabel: "архив", srcPath: arc.path, temp: true,
			})
		default:
			stat.otherKept++
			os.Remove(e.TempPath) // unwanted payload, reclaim the temp space
		}
		return nil
	})
	stat.extractDur = time.Since(t)

	return cands, projs, stat
}

// searchDrumkitsForReferenced walks dir recursively and returns candidates for
// every audio file whose lowercase basename appears in the referenced map.
// Files not in referenced are silently skipped.
func (s *HarvestService) searchDrumkitsForReferenced(dir string, referenced map[string]bool, extra bool) []candidate {
	audioExts := audioExtSet(extra)
	var found []candidate
	_ = filepath.WalkDir(dir, func(p string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		if !audioExts[ext] {
			return nil
		}
		name := filepath.Base(p)
		if !referenced[strings.ToLower(name)] {
			return nil // sound not used by any project — skip
		}
		info, err := de.Info()
		if err != nil {
			return nil
		}
		found = append(found, candidate{
			name:     name,
			path:     p,
			relPath:  relativeTo(dir, p),
			ext:      ext,
			size:     info.Size(),
			origin:   domain.OriginBoth,
			srcLabel: "драмкит",
			srcPath:  dir,
		})
		return nil
	})
	return found
}

// applyProjectLinkage marks archive/folder samples that a project references as
// OriginBoth, and—when onlyFromFLP is set—drops everything not referenced.
func (s *HarvestService) applyProjectLinkage(cands []candidate, referenced map[string]bool, onlyFromFLP bool) []candidate {
	if len(referenced) == 0 {
		if onlyFromFLP {
			return nil
		}
		return cands
	}
	out := cands[:0]
	for _, c := range cands {
		isRef := referenced[strings.ToLower(c.name)]
		if onlyFromFLP && !isRef {
			if c.temp {
				os.Remove(c.path)
			}
			continue
		}
		if isRef {
			c.origin = domain.OriginBoth
		}
		out = append(out, c)
	}
	return out
}

// process runs the two-pool analysis + serial dedup/store stage.
//
// Stage I/O (ioWorkers goroutines): compute full content hash (MD5 + SHA-256).
// Stage CPU (cpuWorkers goroutines): decode PCM, compute audio features and
//   perceptual fingerprint.
//
// If an analysis cache is available, the CPU stage is skipped for files whose
// SHA-256 is already cached. This is the core of the content-addressed cache:
// re-harvesting the same bytes (regardless of path or name) costs only a hash.
func (s *HarvestService) process(ctx context.Context, cands []candidate, req domain.HarvestRequest, acc *dedup.Accumulator, report domain.ProgressReporter, tmr *harvestTimer) (domain.DedupStats, error) {
	total := len(cands)
	tmr.totalFiles = total
	if total == 0 {
		return acc.Stats(), nil
	}

	index := dedup.NewIndex(req.DeepDedup, req.AcousticThreshold)
	if err := s.seedIndex(ctx, index); err != nil {
		report.Detail("warn: dedup seed failed: " + err.Error())
	}

	// --- I/O stage: hash files ---

	type hashed struct {
		cand        candidate
		md5, sha256 string
	}

	work := make(chan candidate, s.ioWorkers*4)
	hashes := make(chan hashed, s.ioWorkers*4)
	results := make(chan analyzed, s.cpuWorkers*4)

	go func() {
		defer close(work)
		for _, c := range cands {
			select {
			case <-ctx.Done():
				return
			case work <- c:
			}
		}
	}()

	var ioWg sync.WaitGroup
	for i := 0; i < s.ioWorkers; i++ {
		ioWg.Add(1)
		go func() {
			defer ioWg.Done()
			for c := range work {
				if ctx.Err() != nil {
					return
				}
				th := time.Now()
				m, sh, _ := s.hasher.Hashes(c.path)
				tmr.hashNs.Add(time.Since(th).Nanoseconds())
				hashes <- hashed{cand: c, md5: m, sha256: sh}
			}
		}()
	}
	go func() { ioWg.Wait(); close(hashes) }()

	// --- CPU stage: analyze (with content cache shortcut) ---

	var cpuWg sync.WaitGroup
	for i := 0; i < s.cpuWorkers; i++ {
		cpuWg.Add(1)
		go func() {
			defer cpuWg.Done()
			for h := range hashes {
				if ctx.Err() != nil {
					return
				}
				a := analyzed{cand: h.cand, md5: h.md5, sha256: h.sha256}

				// Content cache hit: skip re-analysis entirely.
				if s.cache != nil && h.sha256 != "" {
					if feat, fp, ok := s.cache.GetCached(ctx, h.sha256); ok {
						a.features = feat
						a.fingerprint = fp
						tmr.cacheHits.Add(1)
						results <- a
						continue
					}
				}

				ta := time.Now()
				a.features, a.fingerprint, _ = s.analyzer.AnalyzeAll(ctx, h.cand.path)
				tmr.analyzeNs.Add(time.Since(ta).Nanoseconds())

				// Populate cache for future runs.
				if s.cache != nil && h.sha256 != "" {
					_ = s.cache.SetCached(ctx, h.sha256, a.features, a.fingerprint)
				}
				results <- a
			}
		}()
	}
	go func() { cpuWg.Wait(); close(results) }()

	// Serial consumer: dedup -> classify -> tag -> store.
	processed := 0
	lastReport := time.Now()
	for a := range results {
		if err := ctx.Err(); err != nil {
			return acc.Stats(), err
		}
		processed++
		s.consume(ctx, a, req, index, acc, tmr)
		if time.Since(lastReport) > 150*time.Millisecond {
			report.Set(0.30+0.70*float64(processed)/float64(total), "Анализ звуков", a.cand.name)
			lastReport = time.Now()
		}
	}
	return acc.Stats(), nil
}

// seedIndex loads existing samples (id + hashes + fingerprints) into the index.
func (s *HarvestService) seedIndex(ctx context.Context, index *dedup.Index) error {
	const page = 500
	offset := 0
	for {
		batch, _, err := s.samples.Search(ctx, domain.SearchQuery{Limit: page, Offset: offset, Sort: "added", Order: "asc"})
		if err != nil {
			return err
		}
		index.Seed(batch)
		if len(batch) < page {
			return nil
		}
		offset += page
	}
}

// analyzeCandidate is kept for callers that invoke it directly (e.g. tests).
// In the main pipeline, process() uses the separated I/O+CPU pools instead.
func (s *HarvestService) analyzeCandidate(ctx context.Context, c candidate, _ bool) analyzed {
	a := analyzed{cand: c}
	a.md5, a.sha256, _ = s.hasher.Hashes(c.path)

	// Check content cache before running expensive decode.
	if s.cache != nil && a.sha256 != "" {
		if feat, fp, ok := s.cache.GetCached(ctx, a.sha256); ok {
			a.features = feat
			a.fingerprint = fp
			return a
		}
	}

	a.features, a.fingerprint, _ = s.analyzer.AnalyzeAll(ctx, c.path)

	if s.cache != nil && a.sha256 != "" {
		_ = s.cache.SetCached(ctx, a.sha256, a.features, a.fingerprint)
	}
	return a
}

// consume applies the duplicate decision and stores unique samples.
func (s *HarvestService) consume(ctx context.Context, a analyzed, req domain.HarvestRequest, index *dedup.Index, acc *dedup.Accumulator, tmr *harvestTimer) {
	defer func() {
		if a.cand.temp {
			os.Remove(a.cand.path)
		}
	}()

	// dedup
	tc := time.Now()
	existingID, kind, existingName := index.Check(a.md5, a.sha256, a.fingerprint)
	tmr.dedupWall += time.Since(tc)

	// Дублем считается только файл с совпадающим именем (case-insensitive).
	// Один и тот же байтовый контент под разными именами — разные звуки, храним оба.
	isDuplicate := kind != dedup.Unique && strings.EqualFold(existingName, strings.ToLower(a.cand.name))

	if isDuplicate {
		acc.AddDuplicate(a.cand.size)
		// If a project references this duplicate, bump its usage counter so the
		// analytics "most used" reflects real project references.
		if a.cand.origin == domain.OriginBoth && existingID > 0 {
			_ = s.samples.IncrementUsed(ctx, existingID, 1)
		}
		return
	}

	// копирование файла
	tc = time.Now()
	storedPath, err := s.copyToStore(a)
	tmr.copyWall += time.Since(tc)
	if err != nil {
		// Could not persist the file; do not record a phantom library entry.
		acc.AddDuplicate(a.cand.size)
		return
	}

	// классификация
	tc = time.Now()
	cat, fromAudio := s.classifier.Classify(a.cand.name, a.cand.relPath, a.features)
	tmr.classifyWall += time.Since(tc)

	sample := &domain.Sample{
		Name:        a.cand.name,
		Path:        storedPath,
		Ext:         strings.TrimPrefix(a.cand.ext, "."),
		Size:        a.cand.size,
		Category:    cat,
		Auto:        fromAudio,
		Origin:      a.cand.origin,
		SourceLabel: a.cand.srcLabel,
		SourcePath:  a.cand.srcPath,
		MD5:         a.md5,
		SHA256:      a.sha256,
		Fingerprint: a.fingerprint,
		Features:    a.features,
		AddedAt:     time.Now(),
	}
	if a.cand.origin == domain.OriginBoth {
		sample.UsedCount = 1
	}

	// автотеги
	if req.GenerateTags {
		tc = time.Now()
		sample.Tags = s.tagger.Generate(sample)
		tmr.tagWall += time.Since(tc)
	}

	// запись в БД
	tc = time.Now()
	id, err := s.samples.Upsert(ctx, sample)
	tmr.storeWall += time.Since(tc)
	if err != nil {
		acc.AddDuplicate(a.cand.size)
		return
	}
	index.Add(id, a.md5, a.sha256, a.fingerprint, a.cand.name)
	acc.AddUnique(a.cand.size)
}

// copyToStore copies a unique file into the managed library directory using a
// collision-resistant name derived from its content hash.
func (s *HarvestService) copyToStore(a analyzed) (string, error) {
	stem := strings.TrimSuffix(a.cand.name, filepath.Ext(a.cand.name))
	tag := a.sha256
	if tag == "" {
		tag = a.md5
	}
	if len(tag) > 12 {
		tag = tag[:12]
	}
	if tag == "" {
		tag = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	dstName := fmt.Sprintf("%s__%s%s", sanitizeName(stem), tag, a.cand.ext)
	dst := filepath.Join(s.storeDir, dstName)

	if _, err := os.Stat(dst); err == nil {
		return dst, nil // already present (same content) — reuse it
	}
	return dst, copyFile(a.cand.path, dst)
}

// copyFile copies src to dst, creating parent directories as needed.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}

// sanitizeName reduces an arbitrary file name to a safe path segment.
func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", "\x00", "",
	)
	name = replacer.Replace(name)
	if name == "" {
		return "file"
	}
	return name
}
