// Package flp implements a real, dependency-free parser for FL Studio project
// files (.flp). The .flp container is a RIFF-like stream:
//
//	"FLhd" <uint32 len=6> <uint16 format> <uint16 nChannels> <uint16 ppq>
//	"FLdt" <uint32 len>   <event stream>
//
// The event stream is a flat list of TLV records. The event id (one byte)
// determines the payload size class:
//
//	id <  64  -> 1  byte   payload
//	id < 128  -> 2  bytes  payload (uint16 little-endian)
//	id < 192  -> 4  bytes  payload (uint32 little-endian)
//	id >= 192 -> variable  payload, length is a 7-bit varint prefix
//
// Because every record is self-describing, unknown events are skipped safely;
// we only attach semantics to the well-documented ids we need (tempo, title,
// author, version, channel names, sample paths, plugin names, channel type,
// pattern names and piano-roll notes).
package flp

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/flapp/core/internal/domain"
)

// Well-known FLP event ids consumed by this parser.
const (
	// Byte-range events (id < 64).
	evChanType = 21 // byte: channel kind (0 sampler, 2 plugin, 3 layer, 4 auto)

	// Word-range events (64 ≤ id < 128).
	evNewChan     = 64 // word: begins a new channel, payload = channel index
	evNewPattern  = 65 // word: begins a new pattern, payload = pattern index
	evTempoLegacy = 66 // word: whole-number BPM (pre-FL ~7)

	// Dword-range events (128 ≤ id < 192).
	evFineTempo     = 156 // dword: BPM * 1000 (некоторые версии FL)
	evFineTempoAlt  = 157 // dword: BPM * 1000 (FL Studio 20+)

	// Variable-length events (id >= 192).
	evTextChanName   = 195 // text: channel/instrument name (0xC3 в реальных FLP)
	evPatternName    = 193 // text: pattern name
	evTextTitle      = 194 // text: project title
	evTextSamplePath = 196 // text: sampler sample file path
	evVersion        = 199 // text: "20.8.3.2304" (always ASCII)
	evTextDefPlugin  = 201 // text: internal default plugin name
	evTextPlugin     = 203 // text: user-facing plugin name
	evTextGenre      = 206 // text: project genre
	evTextAuthor     = 207 // text: project author / artist
	evPatternNotes   = 224 // data: piano-roll note records (24 bytes each, modern FLP)
)

const debugFLP = false

// Parser implements domain.FLPParser.
type Parser struct{}

// New returns a ready FLP parser.
func New() *Parser { return &Parser{} }

var errBadHeader = errors.New("flp: not a valid FL Studio project (missing FLhd)")

// flpChunks holds the decoded FLhd PPQ and the FLdt event payload.
type flpChunks struct {
	ppq  uint16
	data []byte
}

// Parse reads and decodes a .flp file into a domain.Project.
func (p *Parser) Parse(ctx context.Context, path string) (*domain.Project, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fi, _ := os.Stat(path)
	var size int64
	var mtime time.Time
	if fi != nil {
		size = fi.Size()
		mtime = fi.ModTime()
	}
	return p.parseRaw(ctx, raw, path, filepath.Base(path), size, mtime)
}

// ParseBytes decodes a .flp already loaded into memory. displayName is used as
// the project's display name when no internal title is present; srcPath is stored
// as the project's Path for library bookkeeping.
// Используется при извлечении .flp из архива: позволяет парсить прямо из буфера
// без лишней записи и повторного чтения с диска.
func (p *Parser) ParseBytes(ctx context.Context, raw []byte, displayName, srcPath string) (*domain.Project, error) {
	return p.parseRaw(ctx, raw, srcPath, displayName, int64(len(raw)), time.Time{})
}

// parseRaw — общая реализация Parse и ParseBytes.
func (p *Parser) parseRaw(ctx context.Context, raw []byte, srcPath, baseName string, size int64, mtime time.Time) (*domain.Project, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	proj := &domain.Project{
		Name:    strings.TrimSuffix(baseName, filepath.Ext(baseName)),
		Path:    srcPath,
		AddedAt: time.Now(),
		Size:    size,
	}
	if !mtime.IsZero() {
		proj.CreatedAt = mtime
	}

	chunks, err := readFLPChunks(raw)
	if err != nil {
		return nil, err
	}
	proj.PPQ = int(chunks.ppq)

	if err := decodeEvents(ctx, chunks.data, proj); err != nil {
		return nil, err
	}

	// Нормализуем имя проекта.
	// Приоритет: внутренний заголовок FLP (evTextTitle) — наиболее надёжный источник.
	// Иначе убираем числовой префикс "000001_" который infrastructure/archive добавляет
	// в TempPath для уникальности на диске, чтобы он не просачивался в отображаемое имя.
	if proj.Title != "" {
		proj.Name = proj.Title
	} else {
		proj.Name = stripArchivePrefix(proj.Name)
	}

	dedupeStrings(&proj.SamplePaths)
	dedupeStrings(&proj.Plugins)
	return proj, nil
}

// stripArchivePrefix убирает ведущий числовой префикс вида "000001_" из имени файла.
// Архивный распаковщик (infrastructure/archive) добавляет такой префикс к TempPath
// для предотвращения коллизий на диске; в DisplayName проекта он не нужен.
func stripArchivePrefix(name string) string {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i > 0 && i < len(name) && name[i] == '_' {
		return name[i+1:]
	}
	return name
}

// readFLPChunks validates the FLhd header, reads PPQ, and returns the FLdt payload.
func readFLPChunks(raw []byte) (flpChunks, error) {
	if len(raw) < 8 || !bytes.Equal(raw[0:4], []byte("FLhd")) {
		return flpChunks{}, errBadHeader
	}
	hdrLen := binary.LittleEndian.Uint32(raw[4:8])
	pos := 8 + int(hdrLen)
	if pos+8 > len(raw) {
		return flpChunks{}, errBadHeader
	}

	// PPQ находится в FLhd: format(2) + nChannels(2) + ppq(2), смещение 12 от начала файла.
	var ppq uint16
	if hdrLen >= 6 && len(raw) >= 14 {
		ppq = binary.LittleEndian.Uint16(raw[12:14])
	}

	if !bytes.Equal(raw[pos:pos+4], []byte("FLdt")) {
		return flpChunks{}, errBadHeader
	}
	dataLen := binary.LittleEndian.Uint32(raw[pos+4 : pos+8])
	start := pos + 8
	end := start + int(dataLen)
	if end > len(raw) || end < start {
		end = len(raw) // tolerate a truncated/over-reported length
	}
	return flpChunks{ppq: ppq, data: raw[start:end]}, nil
}

// decodeEvents walks the TLV event stream and fills the project, включая ноты пианоролла.
func decodeEvents(ctx context.Context, data []byte, proj *domain.Project) error {
	r := bytes.NewReader(data)

	var (
		cur               *domain.FLPChannel // channel currently being described
		fineTempo         uint32
		legacyTempo       uint16
		eventCount        int
		currentPatternIdx int
		patterns          = make(map[int]string) // patternIdx → имя паттерна
		pendingNotes      []domain.FLPNote      // ноты до разрешения имён паттернов
	)

	flushChannel := func() {
		if cur != nil {
			// Пустой сэмплер: нативный FL-сэмплер без загруженного звука.
			// Каналы с Plugin != "" или SamplePath != "" — НЕ пустые.
			// Каналы layer/automation — тоже не пустые (другая семантика).
			cur.IsEmptySampler = cur.Plugin == "" &&
				cur.SamplePath == "" &&
				(cur.Kind == "sampler" || cur.Kind == "channel")
			proj.Channels = append(proj.Channels, *cur)
			cur = nil
		}
	}

	for r.Len() > 0 {
		// Cooperative cancellation on big files.
		eventCount++
		if eventCount%2048 == 0 {
			if err := ctx.Err(); err != nil {
				return err
			}
		}

		id, err := r.ReadByte()
		if err != nil {
			break
		}

		switch {
		case id < 64: // byte payload
			b, err := r.ReadByte()
			if err != nil {
				return nil
			}
			if id == evChanType && cur != nil {
				cur.Kind = channelKind(b)
			}

		case id < 128: // word payload
			var w uint16
			if err := binary.Read(r, binary.LittleEndian, &w); err != nil {
				return nil
			}
			switch id {
			case evNewChan:
				flushChannel()
				cur = &domain.FLPChannel{Index: int(w), Kind: "channel"}
			case evNewPattern:
				// Начало нового паттерна; обновляем контекст.
				currentPatternIdx = int(w)
			case evTempoLegacy:
				legacyTempo = w
			}

		case id < 192: // dword payload
			var d uint32
			if err := binary.Read(r, binary.LittleEndian, &d); err != nil {
				return nil
			}
			// Valid BPM range 10–600 means fineTempo ∈ [10000, 600000].
			// Values like 0xFFFFFFFF (≈4.3 billion) are FL Studio's "not set" sentinel.
			if (id == evFineTempo || id == evFineTempoAlt) && fineTempo == 0 && d >= 10_000 && d <= 600_000 {
				fineTempo = d
			}

		default: // variable-length payload (id >= 192)
			n, err := readVarLen(r)
			if err != nil {
				return nil
			}
			buf := make([]byte, n)
			if _, err := readFull(r, buf); err != nil {
				return nil
			}
			switch id {
			case evPatternName:
				// Имя текущего паттерна.
				patterns[currentPatternIdx] = decodeText(buf)
			case evPatternNotes:
				// Записи нот пианоролла для текущего паттерна.
				notes := parsePatternNotes(buf, currentPatternIdx)
				pendingNotes = append(pendingNotes, notes...)
			default:
				applyText(id, buf, proj, &cur)
			}
		}
	}
	flushChannel()

	// Подставляем имена паттернов — они могут идти в потоке до или после нот.
	for i := range pendingNotes {
		if name, ok := patterns[pendingNotes[i].PatternIndex]; ok && name != "" {
			pendingNotes[i].PatternName = name
		}
		if pendingNotes[i].PatternName == "" {
			pendingNotes[i].PatternName = fmt.Sprintf("Pattern %d", pendingNotes[i].PatternIndex+1)
		}
	}
	proj.Notes = pendingNotes

	switch {
	case fineTempo > 0:
		proj.BPM = float64(fineTempo) / 1000.0
	case legacyTempo > 0:
		proj.BPM = float64(legacyTempo)
	}
	if debugFLP {
		fmt.Printf("[flp debug] fineTempo=%d legacyTempo=%d → bpm=%.1f\n", fineTempo, legacyTempo, proj.BPM)
	}
	return nil
}

// parsePatternNotes разбирает буфер события FLP_PatternNotes (0xE0) в список нот.
//
// Современный FLP: запись = 24 байта, little-endian.
// Поддерживается также 20-байтовый формат (старые версии FL Studio).
// При неизвестном размере записи буфер пропускается без паники.
func parsePatternNotes(buf []byte, patternIdx int) []domain.FLPNote {
	if len(buf) == 0 {
		return nil
	}

	const sizeModern = 24
	const sizeLegacy = 20

	recSize := sizeModern
	switch {
	case len(buf)%sizeModern == 0:
		recSize = sizeModern
	case len(buf)%sizeLegacy == 0:
		recSize = sizeLegacy
	default:
		// Нестандартный размер — пропускаем, не роняем сайдкар.
		return nil
	}

	n := len(buf) / recSize
	notes := make([]domain.FLPNote, 0, n)

	for i := 0; i < n; i++ {
		rec := buf[i*recSize : i*recSize+recSize]

		pos := binary.LittleEndian.Uint32(rec[0:4])
		// flags := binary.LittleEndian.Uint16(rec[4:6]) — не используем
		rackChan := binary.LittleEndian.Uint16(rec[6:8])
		length := binary.LittleEndian.Uint32(rec[8:12])
		// Ключ: старший байт — тонкая подстройка, младший — MIDI note 0-127.
		keyWord := binary.LittleEndian.Uint32(rec[12:16])
		key := uint8(keyWord & 0x7F)

		// Velocity лежит на смещении 20 (для обоих форматов).
		var velocity uint8 = 100 // умолчание для вариантов без поля velocity
		if recSize >= 21 {
			velocity = rec[20]
			if velocity == 0 {
				velocity = 100
			}
		}

		notes = append(notes, domain.FLPNote{
			Position:     pos,
			Length:       length,
			RackChan:     rackChan,
			Key:          key,
			Velocity:     velocity,
			PatternIndex: patternIdx,
		})
	}
	return notes
}

// applyText handles the text/structured event class.
func applyText(id byte, buf []byte, proj *domain.Project, cur **domain.FLPChannel) {
	switch id {
	case evVersion:
		proj.FLPVersion = strings.TrimRight(string(buf), "\x00")
	case evTextTitle:
		proj.Title = decodeText(buf)
	case evTextAuthor:
		proj.Artist = decodeText(buf)
	case evTextGenre:
		if g := decodeText(buf); g != "" {
			addUnique(&proj.Tags, strings.ToLower(g))
		}
	case evTextChanName:
		if *cur != nil {
			(*cur).Name = decodeText(buf)
		}
	case evTextSamplePath:
		path := decodeText(buf)
		if path == "" {
			return
		}
		proj.SamplePaths = append(proj.SamplePaths, path)
		if *cur != nil {
			(*cur).SamplePath = path
			if (*cur).Kind == "channel" {
				(*cur).Kind = "sampler"
			}
		}
	case evTextPlugin, evTextDefPlugin:
		name := decodeText(buf)
		if name == "" {
			return
		}
		proj.Plugins = append(proj.Plugins, name)
		if *cur != nil && (*cur).Plugin == "" {
			(*cur).Plugin = name
			if (*cur).Kind == "channel" {
				(*cur).Kind = "plugin"
			}
		}
	}
}

// channelKind maps the FLP_ChanType byte to a readable kind.
func channelKind(b byte) string {
	switch b {
	case 0:
		return "sampler"
	case 2:
		return "plugin"
	case 3:
		return "layer"
	case 4:
		return "automation"
	default:
		return "channel"
	}
}

// readVarLen decodes FL's little-endian 7-bit variable-length integer.
func readVarLen(r *bytes.Reader) (int, error) {
	var value, shift int
	for {
		b, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		value |= int(b&0x7F) << shift
		if b&0x80 == 0 {
			return value, nil
		}
		shift += 7
		if shift > 28 { // guard against malformed input
			return value, nil
		}
	}
}

// readFull fills buf from r or returns the underlying error.
func readFull(r *bytes.Reader, buf []byte) (int, error) {
	got := 0
	for got < len(buf) {
		b, err := r.ReadByte()
		if err != nil {
			return got, err
		}
		buf[got] = b
		got++
	}
	return got, nil
}

// decodeText converts an FLP text payload to a Go string. Modern projects use
// UTF-16LE, older ones use ANSI/UTF-8; we auto-detect per string so the parser
// is robust regardless of the FL version that wrote the file.
func decodeText(b []byte) string {
	if looksUTF16LE(b) {
		return utf16LEToString(b)
	}
	return strings.TrimRight(string(b), "\x00")
}

// looksUTF16LE reports whether the payload is little-endian UTF-16 holding
// mostly ASCII-range code points (the common case for names and file paths):
// every second byte is then zero.
func looksUTF16LE(b []byte) bool {
	if len(b) < 2 || len(b)%2 != 0 {
		return false
	}
	pairs, zeros := 0, 0
	for i := 0; i+1 < len(b); i += 2 {
		pairs++
		if b[i+1] == 0x00 {
			zeros++
		}
	}
	return pairs > 0 && zeros*2 >= pairs
}

// utf16LEToString decodes UTF-16LE bytes, trimming a trailing NUL terminator.
func utf16LEToString(b []byte) string {
	n := len(b) / 2
	u := make([]uint16, 0, n)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	s := string(utf16.Decode(u))
	return strings.TrimRight(s, "\x00")
}

// dedupeStrings removes duplicates from a slice in place, preserving order.
func dedupeStrings(slice *[]string) {
	if slice == nil || len(*slice) < 2 {
		return
	}
	seen := make(map[string]struct{}, len(*slice))
	out := (*slice)[:0]
	for _, v := range *slice {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	*slice = out
}

// addUnique appends v only if not already present.
func addUnique(slice *[]string, v string) {
	for _, e := range *slice {
		if e == v {
			return
		}
	}
	*slice = append(*slice, v)
}

// ParseBPMString is a tiny helper used by tests/tools to read a BPM-like token.
func ParseBPMString(s string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
