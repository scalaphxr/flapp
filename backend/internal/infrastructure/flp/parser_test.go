package flp

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// flpBuilder assembles a synthetic but format-correct .flp byte stream.
type flpBuilder struct {
	events bytes.Buffer
}

func (b *flpBuilder) byteEvent(id byte, v byte) {
	b.events.WriteByte(id)
	b.events.WriteByte(v)
}

func (b *flpBuilder) wordEvent(id byte, v uint16) {
	b.events.WriteByte(id)
	_ = binary.Write(&b.events, binary.LittleEndian, v)
}

func (b *flpBuilder) dwordEvent(id byte, v uint32) {
	b.events.WriteByte(id)
	_ = binary.Write(&b.events, binary.LittleEndian, v)
}

func (b *flpBuilder) writeVarLen(n int) {
	for {
		c := byte(n & 0x7F)
		n >>= 7
		if n != 0 {
			c |= 0x80
		}
		b.events.WriteByte(c)
		if n == 0 {
			break
		}
	}
}

func (b *flpBuilder) textEventASCII(id byte, s string) {
	b.events.WriteByte(id)
	payload := append([]byte(s), 0x00)
	b.writeVarLen(len(payload))
	b.events.Write(payload)
}

func (b *flpBuilder) textEventUTF16(id byte, s string) {
	b.events.WriteByte(id)
	u := utf16.Encode([]rune(s))
	payload := make([]byte, 0, len(u)*2+2)
	for _, c := range u {
		payload = append(payload, byte(c), byte(c>>8))
	}
	payload = append(payload, 0x00, 0x00) // null terminator
	b.writeVarLen(len(payload))
	b.events.Write(payload)
}

func (b *flpBuilder) bytes() []byte {
	var out bytes.Buffer
	out.WriteString("FLhd")
	_ = binary.Write(&out, binary.LittleEndian, uint32(6))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))  // format
	_ = binary.Write(&out, binary.LittleEndian, uint16(4))  // nChannels
	_ = binary.Write(&out, binary.LittleEndian, uint16(96)) // ppq
	out.WriteString("FLdt")
	_ = binary.Write(&out, binary.LittleEndian, uint32(b.events.Len()))
	out.Write(b.events.Bytes())
	return out.Bytes()
}

func TestParseSyntheticProject(t *testing.T) {
	b := &flpBuilder{}
	b.textEventASCII(evVersion, "20.8.3.2304")
	b.dwordEvent(evFineTempo, 140000) // 140.000 BPM
	b.textEventUTF16(evTextTitle, "Midnight Drive")
	b.textEventUTF16(evTextAuthor, "Producer X")
	b.textEventUTF16(evTextGenre, "Trap")

	// Channel 0: a sampler referencing an 808.
	b.wordEvent(evNewChan, 0)
	b.byteEvent(evChanType, 0) // sampler
	b.textEventUTF16(evTextChanName, "808 Sub")
	b.textEventUTF16(evTextSamplePath, `C:\Samples\808\deep_808.wav`)

	// Channel 1: a plugin instrument.
	b.wordEvent(evNewChan, 1)
	b.byteEvent(evChanType, 2) // plugin
	b.textEventUTF16(evTextChanName, "Lead")
	b.textEventUTF16(evTextPlugin, "Serum")

	dir := t.TempDir()
	path := filepath.Join(dir, "test.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatalf("write flp: %v", err)
	}

	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if proj.FLPVersion != "20.8.3.2304" {
		t.Errorf("version = %q, want 20.8.3.2304", proj.FLPVersion)
	}
	if proj.BPM != 140.0 {
		t.Errorf("bpm = %v, want 140", proj.BPM)
	}
	if proj.Title != "Midnight Drive" {
		t.Errorf("title = %q, want Midnight Drive", proj.Title)
	}
	if proj.Artist != "Producer X" {
		t.Errorf("artist = %q, want Producer X", proj.Artist)
	}
	if len(proj.SamplePaths) != 1 || proj.SamplePaths[0] != `C:\Samples\808\deep_808.wav` {
		t.Errorf("samplePaths = %v", proj.SamplePaths)
	}
	if len(proj.Plugins) != 1 || proj.Plugins[0] != "Serum" {
		t.Errorf("plugins = %v", proj.Plugins)
	}
	if len(proj.Channels) != 2 {
		t.Fatalf("channels = %d, want 2", len(proj.Channels))
	}
	if proj.Channels[0].Name != "808 Sub" || proj.Channels[0].SamplePath == "" {
		t.Errorf("channel0 = %+v", proj.Channels[0])
	}
	if proj.Channels[0].Kind != "sampler" {
		t.Errorf("channel0 kind = %q, want sampler", proj.Channels[0].Kind)
	}
	if proj.Channels[1].Plugin != "Serum" || proj.Channels[1].Kind != "plugin" {
		t.Errorf("channel1 = %+v", proj.Channels[1])
	}
	found := false
	for _, tag := range proj.Tags {
		if tag == "trap" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected genre tag 'trap' in %v", proj.Tags)
	}
}

func TestParseLegacyTempoAndASCII(t *testing.T) {
	b := &flpBuilder{}
	b.textEventASCII(evVersion, "9.0.0")
	b.wordEvent(evTempoLegacy, 174) // legacy whole-number BPM
	b.textEventASCII(evTextTitle, "DnB Roller")
	b.wordEvent(evNewChan, 0)
	b.textEventASCII(evTextSamplePath, "/home/user/amen.wav")

	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if proj.BPM != 174 {
		t.Errorf("bpm = %v, want 174", proj.BPM)
	}
	if proj.Title != "DnB Roller" {
		t.Errorf("title = %q", proj.Title)
	}
	if len(proj.SamplePaths) != 1 {
		t.Errorf("samplePaths = %v", proj.SamplePaths)
	}
}

func TestParseRejectsNonFLP(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.flp")
	if err := os.WriteFile(path, []byte("this is not an flp"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Parse(context.Background(), path); err == nil {
		t.Fatal("expected error for non-FLP input")
	}
}

// TestParsePatternNotes проверяет извлечение нот пианоролла из синтетического FLP.
func TestParsePatternNotes(t *testing.T) {
	b := &flpBuilder{}
	b.textEventASCII(evVersion, "20.8.0")
	b.dwordEvent(evFineTempo, 120000) // 120 BPM

	// Channel 0: 808 Sub.
	b.wordEvent(evNewChan, 0)
	b.textEventUTF16(evTextChanName, "808 Sub")

	// Channel 1: Lead.
	b.wordEvent(evNewChan, 1)
	b.textEventUTF16(evTextChanName, "Lead")

	// Pattern 0: две ноты на канале 0 и одна на канале 1.
	b.wordEvent(evNewPattern, 0)
	b.textEventUTF16(evPatternName, "Main Pattern")

	// Формируем буфер нот вручную (24 байта на запись, LE).
	var noteBuf bytes.Buffer
	writeNote := func(pos, length uint32, rack uint16, key uint8, vel uint8) {
		_ = binary.Write(&noteBuf, binary.LittleEndian, pos)           // 0-3
		_ = binary.Write(&noteBuf, binary.LittleEndian, uint16(0))     // 4-5 flags
		_ = binary.Write(&noteBuf, binary.LittleEndian, rack)          // 6-7
		_ = binary.Write(&noteBuf, binary.LittleEndian, length)        // 8-11
		_ = binary.Write(&noteBuf, binary.LittleEndian, uint32(key))   // 12-15
		_ = binary.Write(&noteBuf, binary.LittleEndian, uint16(120))   // 16-17 fine pitch
		_ = binary.Write(&noteBuf, binary.LittleEndian, uint16(0))     // 18-19 reserved
		noteBuf.WriteByte(vel)                                          // 20 velocity
		noteBuf.WriteByte(64)                                           // 21 pan
		noteBuf.WriteByte(0)                                            // 22 mod_x
		noteBuf.WriteByte(0)                                            // 23 mod_y
	}
	writeNote(0, 96, 0, 36, 100)  // C2 на 808
	writeNote(96, 96, 0, 40, 90)  // E2 на 808
	writeNote(0, 192, 1, 60, 110) // C4 на Lead

	noteBufBytes := noteBuf.Bytes()
	b.events.WriteByte(evPatternNotes)
	b.writeVarLen(len(noteBufBytes))
	b.events.Write(noteBufBytes)

	dir := t.TempDir()
	path := filepath.Join(dir, "notes_test.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatalf("write flp: %v", err)
	}

	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if proj.PPQ != 96 {
		t.Errorf("ppq = %d, want 96", proj.PPQ)
	}

	if len(proj.Notes) != 3 {
		t.Fatalf("notes count = %d, want 3", len(proj.Notes))
	}

	// Проверяем содержимое нот.
	note0 := proj.Notes[0]
	if note0.RackChan != 0 || note0.Key != 36 || note0.Position != 0 || note0.Length != 96 {
		t.Errorf("note[0] = %+v, want rack=0 key=36 pos=0 len=96", note0)
	}
	if note0.Velocity != 100 {
		t.Errorf("note[0].Velocity = %d, want 100", note0.Velocity)
	}

	note2 := proj.Notes[2]
	if note2.RackChan != 1 || note2.Key != 60 {
		t.Errorf("note[2] = %+v, want rack=1 key=60", note2)
	}

	// Все ноты должны иметь имя паттерна.
	for i, n := range proj.Notes {
		if n.PatternName != "Main Pattern" {
			t.Errorf("note[%d].PatternName = %q, want 'Main Pattern'", i, n.PatternName)
		}
	}
}

// TestParseProjectNameNormalization проверяет, что proj.Name не содержит
// числовой префикс "000001_", добавляемый archive при извлечении во временную папку.
func TestParseProjectNameNormalization(t *testing.T) {
	// Случай 1: FLP содержит заголовок — используем его, игнорируем имя файла.
	t.Run("title_preferred", func(t *testing.T) {
		b := &flpBuilder{}
		b.textEventASCII(evTextTitle, "My Cool Beat")
		dir := t.TempDir()
		path := filepath.Join(dir, "000001_my_cool_beat.flp")
		if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		proj, err := New().Parse(context.Background(), path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if proj.Name != "My Cool Beat" {
			t.Errorf("Name = %q, want 'My Cool Beat'", proj.Name)
		}
	})

	// Случай 2: FLP без заголовка, файл с числовым префиксом — префикс должен быть убран.
	t.Run("strip_archive_prefix", func(t *testing.T) {
		b := &flpBuilder{}
		b.dwordEvent(evFineTempo, 120000)
		dir := t.TempDir()
		path := filepath.Join(dir, "000001_qertqertqe.flp")
		if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		proj, err := New().Parse(context.Background(), path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if proj.Name != "qertqertqe" {
			t.Errorf("Name = %q, want 'qertqertqe' (prefix stripped)", proj.Name)
		}
	})

	// Случай 3: прямой импорт .flp — имя файла чистое, ничего не меняется.
	t.Run("direct_import_untouched", func(t *testing.T) {
		b := &flpBuilder{}
		b.dwordEvent(evFineTempo, 120000)
		dir := t.TempDir()
		path := filepath.Join(dir, "my_project.flp")
		if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		proj, err := New().Parse(context.Background(), path)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if proj.Name != "my_project" {
			t.Errorf("Name = %q, want 'my_project'", proj.Name)
		}
	})
}

// TestIsEmptySampler проверяет правило IsEmptySampler:
//   - сэмплер с SamplePath → NOT empty;
//   - сэмплер без SamplePath → IS empty;
//   - плагин без SamplePath → NOT empty (звук даёт плагин).
func TestIsEmptySampler(t *testing.T) {
	b := &flpBuilder{}
	b.textEventASCII(evVersion, "20.8.0")
	b.dwordEvent(evFineTempo, 120000)

	// Канал 0: сэмплер со снэйром → не пустой.
	b.wordEvent(evNewChan, 0)
	b.byteEvent(evChanType, 0)
	b.textEventUTF16(evTextChanName, "Snare")
	b.textEventUTF16(evTextSamplePath, `C:\Samples\snare 01.wav`)

	// Канал 1: пустой сэмплер (нет события SampleFileName) → IS empty.
	b.wordEvent(evNewChan, 1)
	b.byteEvent(evChanType, 0)
	b.textEventUTF16(evTextChanName, "Empty Slot")

	// Канал 2: плагин Kontakt без SamplePath → не пустой (плагин сам генерит звук).
	b.wordEvent(evNewChan, 2)
	b.byteEvent(evChanType, 2)
	b.textEventUTF16(evTextChanName, "Kontakt Strings")
	b.textEventUTF16(evTextPlugin, "Kontakt 7")

	dir := t.TempDir()
	path := filepath.Join(dir, "empty_sampler_test.flp")
	if err := os.WriteFile(path, b.bytes(), 0o644); err != nil {
		t.Fatalf("write flp: %v", err)
	}
	proj, err := New().Parse(context.Background(), path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(proj.Channels) != 3 {
		t.Fatalf("channels = %d, want 3", len(proj.Channels))
	}

	// Канал 0: сэмплер со звуком — НЕ пустой.
	if proj.Channels[0].IsEmptySampler {
		t.Errorf("channel 0 (snare with SamplePath) must NOT be IsEmptySampler")
	}
	// Канал 1: сэмплер без звука — пустой.
	if !proj.Channels[1].IsEmptySampler {
		t.Errorf("channel 1 (no SamplePath) must be IsEmptySampler")
	}
	// Канал 2: плагин — НЕ пустой.
	if proj.Channels[2].IsEmptySampler {
		t.Errorf("channel 2 (plugin Kontakt) must NOT be IsEmptySampler")
	}
}

func TestVarLenRoundTrip(t *testing.T) {
	for _, n := range []int{0, 1, 127, 128, 255, 300, 16384, 200000} {
		b := &flpBuilder{}
		b.writeVarLen(n)
		r := bytes.NewReader(b.events.Bytes())
		got, err := readVarLen(r)
		if err != nil {
			t.Fatalf("readVarLen(%d): %v", n, err)
		}
		if got != n {
			t.Errorf("varlen round trip: got %d want %d", got, n)
		}
	}
}
