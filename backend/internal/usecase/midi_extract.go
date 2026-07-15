package usecase

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/midi"
)

// bpmKeywordRe: "140bpm", "bpm140", "140 bpm"
var bpmKeywordRe = regexp.MustCompile(`(?i)\b(\d{2,3})\s*bpm\b|\bbpm\s*(\d{2,3})\b`)

// bpmStandaloneRe: любое отдельное 2-3 значное число
var bpmStandaloneRe = regexp.MustCompile(`\b(\d{2,3})\b`)

// extractBPMFromName ищет BPM в имени проекта.
// 1) ключевое слово "bpm" ("140bpm", "bpm140")
// 2) первое отдельное число 60-260 (для имён "BEAT NAME 143 ARTIST")
func extractBPMFromName(name string) float64 {
	if m := bpmKeywordRe.FindStringSubmatch(name); m != nil {
		for _, g := range m[1:] {
			if g == "" {
				continue
			}
			if v, err := strconv.ParseFloat(g, 64); err == nil && v >= 40 && v <= 300 {
				return v
			}
		}
	}
	for _, m := range bpmStandaloneRe.FindAllStringSubmatch(name, -1) {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil && v >= 60 && v <= 260 {
			return v
		}
	}
	return 0
}

// MidiExtractService — сервис извлечения MIDI-клипов из FL Studio проектов.
// Клипы хранятся в памяти (без БД); очищаются вручную.
type MidiExtractService struct {
	extractor  domain.ArchiveExtractor
	flpParser  domain.FLPParser
	midiDir    string // папка для сохранения .mid файлов
	exportDir  string // папка для ZIP-паков (по умолчанию <dataDir>/exports)

	mu    sync.RWMutex
	clips []*domain.MidiClip
}

// DefaultExportDir возвращает папку для MIDI-паков (фолбэк для handleMidiPack).
func (s *MidiExtractService) DefaultExportDir() string { return s.exportDir }

// NewMidiExtractService создаёт сервис.
// midiDir — папка в data-директории для .mid файлов; exportDir — для ZIP-паков.
func NewMidiExtractService(extractor domain.ArchiveExtractor, flpParser domain.FLPParser, midiDir string) *MidiExtractService {
	// exportDir = parent of midiDir = <ExportDir>
	exportDir := filepath.Dir(midiDir)
	return &MidiExtractService{
		extractor: extractor,
		flpParser: flpParser,
		midiDir:   midiDir,
		exportDir: exportDir,
	}
}

// flpEntry описывает один FLP-файл и его источник.
type flpEntry struct {
	path       string // путь к .flp (временный для архивов)
	sourceType string // "flp" | "zip"
	sourceName string // отображаемое имя источника (без расширения и числового префикса)
}

// stripNumPrefix убирает ведущий числовой префикс вида "000001_" из имени.
func stripNumPrefix(name string) string {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i > 0 && i < len(name) && name[i] == '_' {
		return name[i+1:]
	}
	return name
}

// Extract — главный конвейер: вход → FLP → ноты → группировка → .mid файлы.
func (s *MidiExtractService) Extract(ctx context.Context, req domain.MidiExtractRequest, report domain.ProgressReporter) (map[string]interface{}, error) {
	log.Printf("[midi] Extract start: inputs=%d %v", len(req.Inputs), req.Inputs)
	report.Stage("Сканирование файлов")

	entries, tempDirs, err := s.collectFLPs(ctx, req.Inputs)
	defer func() {
		for _, d := range tempDirs {
			_ = os.RemoveAll(d)
		}
	}()
	if err != nil {
		log.Printf("[midi] collectFLPs error: %v", err)
		return nil, err
	}
	log.Printf("[midi] found %d FLP files", len(entries))
	if len(entries) == 0 {
		return map[string]interface{}{"clips": 0}, nil
	}

	// Определяем базовую папку: из запроса или внутренняя.
	baseDir := s.midiDir
	if req.OutputDir != "" {
		baseDir = req.OutputDir
	}
	runDir := filepath.Join(baseDir, time.Now().Format("20060102_150405"))
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		log.Printf("[midi] cannot create runDir %q: %v", runDir, err)
		return nil, fmt.Errorf("create midi dir: %w", err)
	}
	log.Printf("[midi] writing .mid files to %q", runDir)

	var newClips []*domain.MidiClip
	total := len(entries)

	for i, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		report.Set(float64(i)/float64(total), "Парсинг FLP", filepath.Base(entry.path))

		proj, err := s.flpParser.Parse(ctx, entry.path)
		if err != nil {
			log.Printf("[midi] parse error %q: %v", entry.path, err)
			continue
		}
		log.Printf("[midi] parsed %q: ppq=%d bpm=%.1f channels=%d notes=%d",
			filepath.Base(entry.path), proj.PPQ, proj.BPM, len(proj.Channels), len(proj.Notes))

		clips := s.processProject(proj, runDir, entry.sourceType, entry.sourceName, req.IgnoreEmptySamplers)
		log.Printf("[midi] %q → %d clips", filepath.Base(entry.path), len(clips))
		newClips = append(newClips, clips...)
	}

	s.mu.Lock()
	s.clips = append(s.clips, newClips...)
	s.mu.Unlock()

	log.Printf("[midi] Extract done: total new clips=%d", len(newClips))
	return map[string]interface{}{"clips": len(newClips)}, nil
}

// processProject группирует ноты проекта по (pattern, rackChan) и генерирует .mid.
// srcType и srcName — тип и отображаемое имя источника (zip-архив или flp-файл).
// ignoreEmpty=true — каналы без загруженного звука и без плагина (IsEmptySampler)
// пропускаются (так шлёт UI по умолчанию); false — включаются все группы нот.
func (s *MidiExtractService) processProject(proj *domain.Project, outDir, srcType, srcName string, ignoreEmpty bool) []*domain.MidiClip {
	if len(proj.Notes) == 0 {
		return nil
	}

	ppq := proj.PPQ
	if ppq <= 0 {
		ppq = 96
	}
	bpm := proj.BPM
	if bpm <= 0 {
		bpm = extractBPMFromName(proj.Name)
	}
	// Фолбэк на имя источника (ZIP-архива): внутри архива FLP часто называется
	// "untitled.flp", а BPM написан в имени самого архива ("Beat 140bpm.zip").
	if bpm <= 0 {
		bpm = extractBPMFromName(srcName)
	}
	bpmForSMF := bpm
	if bpmForSMF <= 0 {
		bpmForSMF = 120 // WriteSMF требует валидный темп
	}

	// Быстрый индекс каналов: RackChan → FLPChannel.
	chanMap := make(map[uint16]domain.FLPChannel, len(proj.Channels))
	for _, ch := range proj.Channels {
		chanMap[uint16(ch.Index)] = ch
	}

	// Группируем ноты по (patternIndex, rackChan).
	type groupKey struct {
		pattern  int
		rackChan uint16
	}
	groups := make(map[groupKey][]domain.FLPNote)
	for _, n := range proj.Notes {
		k := groupKey{pattern: n.PatternIndex, rackChan: n.RackChan}
		groups[k] = append(groups[k], n)
	}

	var clips []*domain.MidiClip
	var skippedEmpty int

	for k, notes := range groups {
		if len(notes) == 0 {
			continue
		}

		ch := chanMap[k.rackChan]

		// Пропускаем пустые сэмплеры: канал без загруженного звука и без плагина.
		// Сэмплер со звуком (808, снэйр, хэт…) и плагины (Kontakt, Serum) — проходят.
		if ignoreEmpty && ch.IsEmptySampler {
			skippedEmpty++
			continue
		}

		// Фильтруем ноты с нулевой длиной (FL Studio ghost notes / step-off события).
		// Без фильтра создаются "пустые" MIDI-клипы с клипом без звука.
		{
			filtered := notes[:0]
			for _, n := range notes {
				if n.Length > 0 {
					filtered = append(filtered, n)
				}
			}
			notes = filtered
		}
		if len(notes) == 0 {
			continue
		}

		// Статистика нот для категоризации и отображения.
		minKey, maxKey := notes[0].Key, notes[0].Key
		var maxEndTick uint32
		midiNotes := make([]midi.NoteEvent, len(notes))
		for i, n := range notes {
			if n.Key < minKey {
				minKey = n.Key
			}
			if n.Key > maxKey {
				maxKey = n.Key
			}
			end := n.Position + n.Length
			if end > maxEndTick {
				maxEndTick = end
			}
			midiNotes[i] = midi.NoteEvent{
				PositionTicks: n.Position,
				LengthTicks:   n.Length,
				Key:           n.Key,
				Velocity:      n.Velocity,
			}
		}

		// Round the pattern end up to the nearest bar (4 beats).
		// FL Studio patterns are always a whole number of bars; the last note
		// often ends before the bar boundary, leaving intended silence.
		if ppq > 0 {
			barTicks := uint32(ppq * 4)
			if rem := maxEndTick % barTicks; rem != 0 {
				maxEndTick = maxEndTick + barTicks - rem
			}
		}

		contentHash := computeMidiHash(midiNotes)

		cat, src := midi.Categorize(ch.Name, ch.SamplePath, ch.Plugin, midiNotes, ppq)

		patternName := notes[0].PatternName
		if patternName == "" {
			patternName = fmt.Sprintf("Pattern %d", k.pattern+1)
		}

		// ChannelName — реальное имя из FL Studio (может быть ""). Фолбэк НЕ хранится
		// в домене: фронт показывает "Channel N" когда поле пустое.
		channelName := ch.Name

		// Для имени файла на диске нужен непустой токен — используем фолбэк локально.
		fileToken := channelName
		if fileToken == "" {
			fileToken = fmt.Sprintf("Channel %d", k.rackChan)
		}

		// Имя файла: "<Паттерн> - <Канал>.mid"
		rawName := patternName + " - " + fileToken + ".mid"
		fileName := sanitizeMidiFileName(rawName)
		// Чтобы избежать коллизий имён между разными проектами, префикс — имя проекта.
		filePath := filepath.Join(outDir, sanitizeMidiFileName(proj.Name)+"_"+fileName)

		smfData, writeErr := midi.WriteSMF(midiNotes, ppq, bpmForSMF)
		if writeErr == nil {
			_ = os.WriteFile(filePath, smfData, 0o644)
		}

		var durSec float64
		if bpmForSMF > 0 && ppq > 0 {
			durSec = float64(maxEndTick) / float64(ppq) * 60.0 / bpmForSMF
		}

		clips = append(clips, &domain.MidiClip{
			ID:            newMidiID(),
			ProjectPath:   proj.Path,
			ProjectName:   proj.Name,
			BPM:           bpmForSMF, // всегда ненулевой: реальный или 120
			PatternIndex:  k.pattern,
			PatternName:   patternName,
			ChannelIndex:  int(k.rackChan),
			ChannelName:   channelName, // пустая строка = нет имени в FL Studio
			SamplePath:    ch.SamplePath,
			Plugin:        ch.Plugin,
			Category:      domain.MidiCategory(cat),
			DecisionSrc:   src,
			NoteCount:     len(notes),
			DurationTicks: maxEndTick,
			DurationSec:   durSec,
			MinKey:        minKey,
			MaxKey:        maxKey,
			FilePath:      filePath,
			FileName:      fileName,
			SourceType:    srcType,
			SourceName:    srcName,
			ContentHash:   contentHash,
		})
	}
	if skippedEmpty > 0 {
		log.Printf("[midi] processProject %q: отсеяно пустых сэмплеров=%d", proj.Name, skippedEmpty)
	}
	return clips
}

// ListClips возвращает снимок хранимых клипов с опциональным фильтром категории.
func (s *MidiExtractService) ListClips(category string) []*domain.MidiClip {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if category == "" {
		out := make([]*domain.MidiClip, len(s.clips))
		copy(out, s.clips)
		return out
	}
	var out []*domain.MidiClip
	for _, c := range s.clips {
		if string(c.Category) == category {
			out = append(out, c)
		}
	}
	return out
}

// GetClipFile возвращает абсолютный путь к .mid файлу по ID клипа.
func (s *MidiExtractService) GetClipFile(id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clips {
		if c.ID == id {
			if _, err := os.Stat(c.FilePath); err != nil {
				return "", domain.ErrNotFound
			}
			return c.FilePath, nil
		}
	}
	return "", domain.ErrNotFound
}

// BuildPack упаковывает выбранные .mid файлы в ZIP с раскладкой по категориям.
// Если exportDir пустой — используем s.exportDir (папка <dataDir>/exports).
func (s *MidiExtractService) BuildPack(ctx context.Context, ids []string, packName, exportDir string) (string, error) {
	log.Printf("[midi] BuildPack: ids=%d packName=%q exportDir=%q", len(ids), packName, exportDir)

	s.mu.RLock()
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	selected := make([]*domain.MidiClip, 0, len(ids))
	for _, c := range s.clips {
		if idSet[c.ID] {
			selected = append(selected, c)
		}
	}
	s.mu.RUnlock()

	log.Printf("[midi] BuildPack: matched %d/%d clips", len(selected), len(ids))
	if len(selected) == 0 {
		return "", domain.ErrNotFound
	}

	if packName == "" {
		packName = "midi_pack_" + time.Now().Format("20060102_150405")
	}
	// Фолбэк на внутреннюю директорию когда settings.ExportDir не задан.
	if exportDir == "" {
		exportDir = s.exportDir
	}
	log.Printf("[midi] BuildPack: resolved exportDir=%q", exportDir)
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return "", fmt.Errorf("create export dir %q: %w", exportDir, err)
	}
	outPath := filepath.Join(exportDir, sanitizeMidiFileName(packName)+".zip")

	f, err := os.Create(outPath)
	if err != nil {
		return "", fmt.Errorf("create zip: %w", err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, clip := range selected {
		if ctx.Err() != nil {
			_ = zw.Close()
			return "", ctx.Err()
		}
		arcPath := filepath.ToSlash(filepath.Join(string(clip.Category), clip.FileName))
		w, err := zw.Create(arcPath)
		if err != nil {
			continue
		}
		data, err := os.ReadFile(clip.FilePath)
		if err != nil {
			continue
		}
		_, _ = w.Write(data)
	}
	if err := zw.Close(); err != nil {
		return "", err
	}
	log.Printf("[midi] BuildPack done: %q", outPath)
	return outPath, nil
}

// GetClipNotes парсит .mid файл клипа и возвращает ноты для пианоролла.
func (s *MidiExtractService) GetClipNotes(id string) (*domain.MidiNotesResult, error) {
	s.mu.RLock()
	var clip *domain.MidiClip
	for _, c := range s.clips {
		if c.ID == id {
			clip = c
			break
		}
	}
	s.mu.RUnlock()

	if clip == nil || clip.FilePath == "" {
		return nil, domain.ErrNotFound
	}
	data, err := os.ReadFile(clip.FilePath)
	if err != nil {
		return nil, domain.ErrNotFound
	}
	result, err := midi.ParseSMF(data)
	if err != nil {
		return nil, err
	}
	// Override with authoritative values stored on the clip:
	// BPM was corrected by name extraction; DurationTicks is bar-rounded.
	if clip.BPM > 0 {
		result.BPM = clip.BPM
	}
	if clip.DurationTicks > 0 {
		result.DurationTicks = int(clip.DurationTicks)
	}
	return result, nil
}

// computeMidiHash returns a 16-char hex hash of note content.
// Notes are sorted by (tick, pitch), then start-position-normalized (first tick → 0),
// so the same pattern at different positions in the arrangement hashes identically.
func computeMidiHash(notes []midi.NoteEvent) string {
	if len(notes) == 0 {
		return ""
	}
	type item struct {
		pos, dur uint32
		key, vel uint8
	}
	items := make([]item, len(notes))
	for i, n := range notes {
		items[i] = item{n.PositionTicks, n.LengthTicks, n.Key, n.Velocity}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].pos != items[j].pos {
			return items[i].pos < items[j].pos
		}
		return items[i].key < items[j].key
	})
	// Normalize: shift so first note starts at tick 0.
	if off := items[0].pos; off > 0 {
		for i := range items {
			items[i].pos -= off
		}
	}
	h := md5.New()
	var buf [10]byte
	for _, it := range items {
		binary.LittleEndian.PutUint32(buf[0:4], it.pos)
		binary.LittleEndian.PutUint32(buf[4:8], it.dur)
		buf[8], buf[9] = it.key, it.vel
		h.Write(buf[:])
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// DeduplicateClips находит клипы с одинаковым содержимым нот и удаляет дубликаты,
// оставляя по одному представителю из каждой группы.
// Приоритет сохранения: есть сэмпл > непустое имя канала > длиннее имя > раньше по ID.
func (s *MidiExtractService) DeduplicateClips() *domain.MidiDedupResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Группируем по ContentHash (пустой хеш — не дедуплицируем).
	groups := make(map[string][]*domain.MidiClip)
	for _, c := range s.clips {
		if c.ContentHash == "" {
			continue
		}
		groups[c.ContentHash] = append(groups[c.ContentHash], c)
	}

	removeIDs := make(map[string]struct{})
	var keptReps []*domain.MidiClip

	for _, group := range groups {
		if len(group) <= 1 {
			continue
		}
		// Сортируем: лучший вариант первым.
		sort.SliceStable(group, func(i, j int) bool {
			ci, cj := group[i], group[j]
			// 1. Клип с сэмплом лучше клипа без сэмпла.
			hi := ci.SamplePath != ""
			hj := cj.SamplePath != ""
			if hi != hj {
				return hi
			}
			// 2. Непустое имя канала лучше пустого.
			ni := ci.ChannelName != ""
			nj := cj.ChannelName != ""
			if ni != nj {
				return ni
			}
			// 3. Более длинное имя (больше информации).
			if len(ci.ChannelName) != len(cj.ChannelName) {
				return len(ci.ChannelName) > len(cj.ChannelName)
			}
			// 4. Тай-брейк по ID (сохраняем первый извлечённый).
			return ci.ID < cj.ID
		})
		keptReps = append(keptReps, group[0])
		for _, dup := range group[1:] {
			removeIDs[dup.ID] = struct{}{}
		}
	}

	removed := len(removeIDs)
	if removed == 0 {
		return &domain.MidiDedupResult{Removed: 0, Groups: 0, Kept: []*domain.MidiClip{}}
	}

	// Удаляем файлы дубликатов с диска.
	for _, c := range s.clips {
		if _, isDup := removeIDs[c.ID]; isDup && c.FilePath != "" {
			_ = os.Remove(c.FilePath)
		}
	}

	// Фильтруем список клипов.
	out := s.clips[:0]
	for _, c := range s.clips {
		if _, isDup := removeIDs[c.ID]; !isDup {
			out = append(out, c)
		}
	}
	s.clips = out

	log.Printf("[midi] dedup: removed=%d groups=%d", removed, len(keptReps))
	return &domain.MidiDedupResult{
		Removed: removed,
		Groups:  len(keptReps),
		Kept:    keptReps,
	}
}

// GetClipSamplePath возвращает путь к аудиофайлу сэмпла для данного клипа.
func (s *MidiExtractService) GetClipSamplePath(id string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clips {
		if c.ID == id {
			if c.SamplePath == "" {
				return "", domain.ErrNotFound
			}
			if _, err := os.Stat(c.SamplePath); err != nil {
				return "", domain.ErrNotFound
			}
			return c.SamplePath, nil
		}
	}
	return "", domain.ErrNotFound
}

// GetClipRawSamplePath возвращает сырой путь к сэмплу из FLP без проверки существования файла.
func (s *MidiExtractService) GetClipRawSamplePath(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, c := range s.clips {
		if c.ID == id {
			return c.SamplePath
		}
	}
	return ""
}

// ClearClips удаляет все клипы из памяти И с диска.
// Без удаления с диска .mid файлы переиндексировались бы при следующем запуске.
func (s *MidiExtractService) ClearClips() {
	s.mu.Lock()
	s.clips = nil
	s.mu.Unlock()

	// Удаляем подпапки с извлечёнными .mid файлами (каждый запуск создаёт свою).
	if entries, err := os.ReadDir(s.midiDir); err == nil {
		for _, e := range entries {
			os.RemoveAll(filepath.Join(s.midiDir, e.Name()))
		}
	}
	// Воссоздаём пустую директорию, чтобы следующая экстракция не упала.
	os.MkdirAll(s.midiDir, 0o755)
}

// SetClipCategory задаёт категорию клипа вручную (override авто-определения).
// BuildPack использует текущее значение Category, поэтому пак раскладывает
// по переопределённой категории без дополнительных изменений.
func (s *MidiExtractService) SetClipCategory(id string, cat domain.MidiCategory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.clips {
		if c.ID == id {
			c.Category = cat
			c.CategoryOverride = true
			return nil
		}
	}
	return domain.ErrNotFound
}

// ClearDerivedCache удаляет производный кэш:
//   - Все .mid файлы из midiDir (извлечённые из FLP).
//   - Все .zip файлы из exportDir (собранные паки).
//
// НЕ затрагивает: library.db, settings.json, library/ (уникальные аудиокопии).
func (s *MidiExtractService) ClearDerivedCache() (domain.CacheStats, error) {
	s.mu.Lock()
	s.clips = nil
	s.mu.Unlock()

	var stats domain.CacheStats

	// Удаляем подпапки и файлы из midiDir (каждый запуск создаёт подпапку с timestamp).
	if entries, err := os.ReadDir(s.midiDir); err == nil {
		for _, e := range entries {
			entPath := filepath.Join(s.midiDir, e.Name())
			size, _ := dirSize(entPath)
			stats.MidiBytes += size
			stats.MidiFiles++
			_ = os.RemoveAll(entPath)
		}
	}

	// Удаляем .zip файлы из exportDir.
	if entries, err := os.ReadDir(s.exportDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if strings.ToLower(filepath.Ext(e.Name())) != ".zip" {
				continue
			}
			entPath := filepath.Join(s.exportDir, e.Name())
			if info, err := e.Info(); err == nil {
				stats.ExportBytes += info.Size()
			}
			stats.ExportFiles++
			_ = os.Remove(entPath)
		}
	}

	stats.TotalBytes = stats.MidiBytes + stats.ExportBytes
	return stats, nil
}

// dirSize возвращает суммарный размер файлов в дереве dir.
func dirSize(dir string) (int64, error) {
	var total int64
	err := filepath.WalkDir(dir, func(_ string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// collectFLPs ищет все .flp-файлы среди входящих путей (папки, архивы, прямые .flp).
// Возвращает список flpEntry с типом источника и его отображаемым именем.
func (s *MidiExtractService) collectFLPs(ctx context.Context, inputs []string) (entries []flpEntry, tempDirs []string, err error) {
	for _, in := range inputs {
		if ctx.Err() != nil {
			return entries, tempDirs, ctx.Err()
		}

		info, statErr := os.Stat(in)
		if statErr != nil {
			continue
		}

		ext := strings.ToLower(filepath.Ext(in))

		switch {
		case info.IsDir():
			_ = filepath.WalkDir(in, func(p string, de fs.DirEntry, walkErr error) error {
				if walkErr != nil || de.IsDir() {
					return nil
				}
				if strings.ToLower(filepath.Ext(p)) != ".flp" {
					return nil
				}
				base := filepath.Base(p)
				name := stripNumPrefix(strings.TrimSuffix(base, filepath.Ext(base)))
				entries = append(entries, flpEntry{path: p, sourceType: "flp", sourceName: name})
				return nil
			})

		case ext == ".flp":
			base := filepath.Base(in)
			name := stripNumPrefix(strings.TrimSuffix(base, ext))
			log.Printf("[midi] direct FLP: %q → sourceName=%q", in, name)
			entries = append(entries, flpEntry{path: in, sourceType: "flp", sourceName: name})

		case ext == ".zip" || ext == ".rar" || ext == ".7z":
			// Имя источника = имя архива (без числового префикса, без расширения).
			arcBase := filepath.Base(in)
			arcName := stripNumPrefix(strings.TrimSuffix(arcBase, filepath.Ext(arcBase)))
			tmpDir, tmpErr := os.MkdirTemp("", "midi_flp_*")
			if tmpErr != nil {
				continue
			}
			tempDirs = append(tempDirs, tmpDir)
			_ = s.extractor.Extract(ctx, in, tmpDir, func(entry domain.ExtractedFile) error {
				if strings.ToLower(filepath.Ext(entry.Name)) == ".flp" {
					entries = append(entries, flpEntry{
						path:       entry.TempPath,
						sourceType: "zip",
						sourceName: arcName, // все FLP из архива показывают имя архива
					})
				}
				return nil
			})
		}
	}
	return entries, tempDirs, nil
}

// sanitizeMidiFileName убирает из имени файла символы, недопустимые на Windows/macOS.
func sanitizeMidiFileName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteRune('_')
		default:
			if !unicode.IsPrint(r) {
				b.WriteRune('_')
			} else {
				b.WriteRune(r)
			}
		}
	}
	result := strings.TrimSpace(b.String())
	if result == "" {
		return "midi"
	}
	if len(result) > 200 {
		result = result[:200]
	}
	return result
}

// счётчик ID для MIDI-клипов; атомарный, чтобы быть безопасным без мьютекса.
var midiIDCounter uint64

func newMidiID() string {
	id := atomic.AddUint64(&midiIDCounter, 1)
	return fmt.Sprintf("m%d_%d", id, time.Now().UnixMicro()%1_000_000)
}
