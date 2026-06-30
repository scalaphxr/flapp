package usecase_test

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"

	"github.com/flapp/core/internal/adapter/storage"
	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/archive"
	"github.com/flapp/core/internal/infrastructure/audio"
	"github.com/flapp/core/internal/infrastructure/classify"
	"github.com/flapp/core/internal/infrastructure/dedup"
	"github.com/flapp/core/internal/infrastructure/flp"
	"github.com/flapp/core/internal/infrastructure/tagging"
	"github.com/flapp/core/internal/usecase"

	_ "modernc.org/sqlite"
)

// openTestDB opens an in-memory SQLite database wired with the full schema.
func openTestDB(t *testing.T) *storage.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := storage.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

var wavCounter int

// makeWAV creates a minimal WAV file with a unique trailing byte so each file
// has distinct content (different SHA-256), preventing false duplicate detection.
func makeWAV(name string, dir string) string {
	wavCounter++
	path := filepath.Join(dir, name)
	hdr := []byte{
		'R', 'I', 'F', 'F', 0x0C, 0x00, 0x00, 0x00,
		'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 0x00, 0x00,
		byte(wavCounter), // unique content byte
	}
	if err := os.WriteFile(path, hdr, 0o644); err != nil {
		panic(err)
	}
	return path
}

// makeFLP creates a minimal .flp referencing samplePath.
func makeFLP(name, dir, samplePath string) string {
	path := filepath.Join(dir, name)

	encUTF16 := func(s string) []byte {
		u := utf16.Encode([]rune(s))
		b := make([]byte, 0, len(u)*2+2)
		for _, c := range u {
			b = append(b, byte(c), byte(c>>8))
		}
		return append(b, 0, 0)
	}
	writeVarLen := func(buf *bytes.Buffer, n int) {
		for {
			c := byte(n & 0x7F)
			n >>= 7
			if n != 0 {
				c |= 0x80
			}
			buf.WriteByte(c)
			if n == 0 {
				break
			}
		}
	}

	var events bytes.Buffer
	// Fine tempo: 120.000 BPM
	events.WriteByte(156)
	_ = binary.Write(&events, binary.LittleEndian, uint32(120000))

	// Sample path
	events.WriteByte(196) // evTextSamplePath
	sp := encUTF16(samplePath)
	writeVarLen(&events, len(sp))
	events.Write(sp)

	var out bytes.Buffer
	out.WriteString("FLhd")
	_ = binary.Write(&out, binary.LittleEndian, uint32(6))
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(2))
	_ = binary.Write(&out, binary.LittleEndian, uint16(96))
	out.WriteString("FLdt")
	_ = binary.Write(&out, binary.LittleEndian, uint32(events.Len()))
	out.Write(events.Bytes())

	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		panic(err)
	}
	return path
}

// makeZip creates a ZIP archive containing a WAV file.
func makeZipWith(dir, zipName string, entries map[string][]byte) string {
	path := filepath.Join(dir, zipName)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, _ := zw.Create(name)
		w.Write(content)
	}
	zw.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		panic(err)
	}
	return path
}

// nopReporter implements domain.ProgressReporter with no output.
type nopReporter struct{}

func (nopReporter) Set(float64, string, string) {}
func (nopReporter) Stage(string)                {}
func (nopReporter) Detail(string)               {}

// buildHarvest wires up a complete HarvestService against a test store.
func buildHarvest(t *testing.T, store *storage.Store, storeDir, tempDir string) *usecase.HarvestService {
	t.Helper()
	return usecase.NewHarvestService(usecase.HarvestDeps{
		Samples:       store.Samples,
		Projects:      store.Projects,
		Extractor:     archive.New([]string{".wav", ".mp3", ".flac", ".ogg", ".aiff", ".aif", ".m4a", ".flp"}),
		FLP:           flp.New(),
		Analyzer:      audio.NewAnalyzer(),
		Classifier:    classify.New(),
		Tagger:        tagging.New(),
		Hasher:        dedup.NewHasher(),
		AnalysisCache: store.AnalysisCache,
		StoreDir:      storeDir,
		TempDir:       tempDir,
		IOWorkers:     1,
		CPUWorkers:    1,
	})
}

// ---- Tests ----

func TestHarvestFolderOfWAVs(t *testing.T) {
	store := openTestDB(t)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	tempDir := filepath.Join(dir, "tmp")
	os.MkdirAll(storeDir, 0o755)
	os.MkdirAll(tempDir, 0o755)

	// Create 3 WAV files.
	srcDir := filepath.Join(dir, "samples")
	os.MkdirAll(srcDir, 0o755)
	makeWAV("kick.wav", srcDir)
	makeWAV("snare.wav", srcDir)
	makeWAV("hihat.wav", srcDir)

	svc := buildHarvest(t, store, storeDir, tempDir)
	req := domain.HarvestRequest{
		Inputs:      []string{srcDir},
		GenerateTags: true,
	}
	result, err := svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("harvest error: %v", err)
	}
	stats := result["stats"].(domain.DedupStats)
	if stats.UniqueFiles != 3 {
		t.Errorf("expected 3 unique files, got %d", stats.UniqueFiles)
	}
	if stats.Duplicates != 0 {
		t.Errorf("expected 0 duplicates, got %d", stats.Duplicates)
	}

	count, err := store.Samples.Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 samples in DB, got %d", count)
	}
}

func TestHarvestDedup_SameContent(t *testing.T) {
	store := openTestDB(t)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	tempDir := filepath.Join(dir, "tmp")
	os.MkdirAll(storeDir, 0o755)
	os.MkdirAll(tempDir, 0o755)

	srcDir := filepath.Join(dir, "samples")
	os.MkdirAll(srcDir, 0o755)
	wav := makeWAV("kick.wav", srcDir)

	// Create exact duplicate with different name.
	dup := filepath.Join(srcDir, "kick_copy.wav")
	content, _ := os.ReadFile(wav)
	os.WriteFile(dup, content, 0o644)

	svc := buildHarvest(t, store, storeDir, tempDir)
	req := domain.HarvestRequest{Inputs: []string{srcDir}}
	result, err := svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("harvest error: %v", err)
	}
	stats := result["stats"].(domain.DedupStats)
	if stats.UniqueFiles != 1 {
		t.Errorf("expected 1 unique file, got %d", stats.UniqueFiles)
	}
	if stats.Duplicates != 1 {
		t.Errorf("expected 1 duplicate, got %d", stats.Duplicates)
	}
}

func TestHarvestFromZip(t *testing.T) {
	store := openTestDB(t)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	tempDir := filepath.Join(dir, "tmp")
	os.MkdirAll(storeDir, 0o755)
	os.MkdirAll(tempDir, 0o755)

	wavContent := []byte{
		'R', 'I', 'F', 'F', 0x0A, 0x00, 0x00, 0x00,
		'W', 'A', 'V', 'E', 'f', 'm', 't', ' ', 0x00, 0x00,
	}
	zipPath := makeZipWith(dir, "pack.zip", map[string][]byte{
		"drums/kick.wav":  wavContent,
		"drums/snare.wav": append(wavContent, 0x01), // slightly different
	})

	svc := buildHarvest(t, store, storeDir, tempDir)
	req := domain.HarvestRequest{Inputs: []string{zipPath}}
	result, err := svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("harvest error: %v", err)
	}
	stats := result["stats"].(domain.DedupStats)
	if stats.UniqueFiles < 2 {
		t.Errorf("expected >= 2 unique files from ZIP, got %d", stats.UniqueFiles)
	}
	if stats.ArchivesOpened != 1 {
		t.Errorf("expected 1 archive, got %d", stats.ArchivesOpened)
	}
}

func TestHarvestWithFLP(t *testing.T) {
	store := openTestDB(t)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	tempDir := filepath.Join(dir, "tmp")
	os.MkdirAll(storeDir, 0o755)
	os.MkdirAll(tempDir, 0o755)

	srcDir := filepath.Join(dir, "project")
	os.MkdirAll(srcDir, 0o755)
	kickPath := makeWAV("kick.wav", srcDir)
	makeFLP("beat.flp", srcDir, kickPath)

	svc := buildHarvest(t, store, storeDir, tempDir)
	req := domain.HarvestRequest{
		Inputs:      []string{srcDir},
		GenerateTags: true,
	}
	result, err := svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("harvest error: %v", err)
	}
	stats := result["stats"].(domain.DedupStats)
	if stats.ProjectsParsed != 1 {
		t.Errorf("expected 1 project, got %d", stats.ProjectsParsed)
	}

	count, _ := store.Projects.Count(context.Background())
	if count != 1 {
		t.Errorf("expected 1 project in DB, got %d", count)
	}
}

func TestHarvestContentCache(t *testing.T) {
	store := openTestDB(t)
	dir := t.TempDir()
	storeDir := filepath.Join(dir, "store")
	tempDir := filepath.Join(dir, "tmp")
	os.MkdirAll(storeDir, 0o755)
	os.MkdirAll(tempDir, 0o755)

	srcDir := filepath.Join(dir, "samples")
	os.MkdirAll(srcDir, 0o755)
	makeWAV("kick.wav", srcDir)

	svc := buildHarvest(t, store, storeDir, tempDir)
	req := domain.HarvestRequest{Inputs: []string{srcDir}}

	// First harvest: populates cache.
	_, err := svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("first harvest: %v", err)
	}

	// Second harvest: should use cache (same bytes).
	_, err = svc.Run(context.Background(), req, nopReporter{})
	if err != nil {
		t.Fatalf("second harvest: %v", err)
	}

	// DB should still have 1 unique sample (second run is all-duplicate).
	count, _ := store.Samples.Count(context.Background())
	if count != 1 {
		t.Errorf("expected 1 sample after 2 runs, got %d", count)
	}
}

func TestCheckpoint_MarkAndQuery(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	cp := store.Checkpoint
	jobID := "job-test-1"
	archPath := "/path/to/archive.zip"

	if err := cp.MarkDone(ctx, jobID, archPath, "kick.wav", 12345, 4096); err != nil {
		t.Fatalf("MarkDone: %v", err)
	}
	if !cp.IsDone(ctx, jobID, archPath, "kick.wav") {
		t.Error("expected IsDone=true after MarkDone")
	}
	if cp.IsDone(ctx, jobID, archPath, "snare.wav") {
		t.Error("expected IsDone=false for unprocessed entry")
	}

	if err := cp.ClearJob(ctx, jobID); err != nil {
		t.Fatalf("ClearJob: %v", err)
	}
	if cp.IsDone(ctx, jobID, archPath, "kick.wav") {
		t.Error("expected IsDone=false after ClearJob")
	}
}

func TestUnresolvedAssets(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()

	// Insert a project to satisfy the FK.
	db := store.DB()
	var projID int64
	err := db.QueryRowContext(ctx,
		`INSERT INTO projects(name,path,added_at) VALUES('test','/tmp/test.flp',0) RETURNING id`).Scan(&projID)
	if err != nil {
		// Fallback: try insert without RETURNING.
		res, err2 := db.ExecContext(ctx, `INSERT INTO projects(name,path,added_at) VALUES('test','/tmp/test.flp',0)`)
		if err2 != nil {
			t.Skipf("cannot insert project: %v / %v", err, err2)
		}
		projID, _ = res.LastInsertId()
	}

	ur := store.Unresolved
	if err := ur.Record(ctx, projID, `C:\Samples\missing.wav`, "/Samples/missing.wav"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := ur.Record(ctx, projID, `C:\Samples\also_missing.wav`, "/Samples/also_missing.wav"); err != nil {
		t.Fatalf("Record: %v", err)
	}

	paths, err := ur.ListForProject(ctx, projID)
	if err != nil {
		t.Fatalf("ListForProject: %v", err)
	}
	if len(paths) != 2 {
		t.Errorf("expected 2 unresolved paths, got %d", len(paths))
	}
}

func TestAnalysisCache(t *testing.T) {
	store := openTestDB(t)
	ctx := context.Background()
	ac := store.AnalysisCache

	hash := "deadbeef1234567890abcdef"
	feat := domain.AudioFeatures{
		SampleRate:      44100,
		Channels:        2,
		DurationSeconds: 2.5,
		RMS:             0.3,
		Analyzed:        true,
	}
	fp := "aabbccdd"

	if err := ac.SetCached(ctx, hash, feat, fp); err != nil {
		t.Fatalf("SetCached: %v", err)
	}

	gotFeat, gotFP, ok := ac.GetCached(ctx, hash)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if gotFP != fp {
		t.Errorf("fingerprint: got %q want %q", gotFP, fp)
	}
	if gotFeat.SampleRate != feat.SampleRate {
		t.Errorf("sample rate: got %d want %d", gotFeat.SampleRate, feat.SampleRate)
	}
	if gotFeat.DurationSeconds != feat.DurationSeconds {
		t.Errorf("duration: got %v want %v", gotFeat.DurationSeconds, feat.DurationSeconds)
	}

	// Cache miss.
	_, _, ok2 := ac.GetCached(ctx, "nonexistent")
	if ok2 {
		t.Error("expected cache miss for unknown hash")
	}
}

// Satisfy the import of database/sql to avoid unused import errors.
var _ *sql.DB
