// Command flapp-core is the backend sidecar for the Flapp desktop app.
// It wires the Clean-Architecture layers together, starts a local HTTP+SSE API
// bound to 127.0.0.1, and prints the chosen port on stdout so the Tauri shell
// can discover and proxy to it. The process shuts down gracefully on SIGINT or
// SIGTERM (sent by Tauri when the window closes).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	nethttp "net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	httpadapter "github.com/flapp/core/internal/adapter/http"
	"github.com/flapp/core/internal/adapter/storage"
	"github.com/flapp/core/internal/infrastructure/archive"
	"github.com/flapp/core/internal/infrastructure/audio"
	"github.com/flapp/core/internal/infrastructure/classify"
	"github.com/flapp/core/internal/infrastructure/dedup"
	"github.com/flapp/core/internal/infrastructure/flp"
	"github.com/flapp/core/internal/infrastructure/jobs"
	"github.com/flapp/core/internal/infrastructure/settings"
	"github.com/flapp/core/internal/infrastructure/tagging"
	"github.com/flapp/core/internal/usecase"
)


func main() {
	port := flag.Int("port", 0, "TCP port to listen on (0 = pick a free port)")
	dataDir := flag.String("data-dir", "", "data directory (defaults to the OS config dir)")
	flag.Parse()

	dir, err := resolveDataDir(*dataDir)
	if err != nil {
		log.Fatalf("resolve data dir: %v", err)
	}
	if err := run(dir, *port); err != nil {
		log.Fatal(err)
	}
}

// run constructs the application graph and serves until interrupted.
func run(dir string, port int) error {
	// Directory layout under the data dir.
	dbPath := filepath.Join(dir, "library.db")
	storeDir := filepath.Join(dir, "library")
	tempDir := filepath.Join(dir, "tmp")
	settingsPath := filepath.Join(dir, "settings.json")
	for _, d := range []string{storeDir, tempDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", d, err)
		}
	}

	cfg, err := settings.Open(settingsPath)
	if err != nil {
		return fmt.Errorf("open settings: %w", err)
	}
	current := cfg.Get()

	exportDir := current.ExportDir
	if exportDir == "" {
		exportDir = filepath.Join(dir, "exports")
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		return fmt.Errorf("create export dir: %w", err)
	}

	midiDir := filepath.Join(exportDir, "MIDI")
	if err := os.MkdirAll(midiDir, 0o755); err != nil {
		return fmt.Errorf("create midi dir: %w", err)
	}

	store, err := storage.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer store.Close()

	if err := store.Samples.DeleteAll(context.Background()); err != nil {
		return fmt.Errorf("clear library on startup: %w", err)
	}

	// Infrastructure adapters.
	analyzer := audio.NewAnalyzer()
	classifier := classify.New()
	hasher := dedup.NewHasher()
	parser := flp.New()
	tagger := tagging.New()
	extractor := archive.New([]string{".wav", ".mp3", ".flac", ".ogg", ".aiff", ".aif", ".m4a", ".flp"})
	// Отдельный экстрактор только для .flp — для MIDI-сервиса, чтобы не извлекать аудио.
	flpExtractor := archive.New([]string{".flp"})
	packer := archive.NewPacker()
	queue := jobs.New(current.Workers)
	defer queue.Shutdown()

	// Use-case services.
	harvest := usecase.NewHarvestService(usecase.HarvestDeps{
		Samples:       store.Samples,
		Projects:      store.Projects,
		Extractor:     extractor,
		FLP:           parser,
		Analyzer:      analyzer,
		Classifier:    classifier,
		Tagger:        tagger,
		Hasher:        hasher,
		AnalysisCache: store.AnalysisCache,
		StoreDir:      storeDir,
		TempDir:       tempDir,
		Workers:       current.Workers,
	})
	library := usecase.NewLibraryService(store.Samples, store.Tags)
	beatmgr := usecase.NewBeatManagerService(store.Samples, storeDir)
	midiExtract := usecase.NewMidiExtractService(flpExtractor, parser, midiDir)
	packbuild := usecase.NewPackBuilderService(store.Samples, packer, exportDir, midiExtract)
	analytics := usecase.NewAnalyticsService(store.Analytics)
	smart := usecase.NewSmartSearchService(store.Samples)

	server := httpadapter.New(httpadapter.Services{
		Library:     library,
		Harvest:     harvest,
		BeatMgr:     beatmgr,
		PackBuild:   packbuild,
		Analytics:   analytics,
		Smart:       smart,
		MidiExtract: midiExtract,
		Projects:    store.Projects,
		Collections: store.Collections,
		Jobs:        queue,
		Settings:    cfg,
	})

	// Bind first so we can report the actual port (when port == 0).
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	actual := ln.Addr().(*net.TCPAddr).Port

	httpServer := &nethttp.Server{
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The Tauri shell parses this line to learn where the backend is.
	fmt.Printf("PORT=%d\n", actual)
	os.Stdout.Sync()

	// Serve in the background; block on a shutdown signal.
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- httpServer.Serve(ln)
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		if err != nil && err != nethttp.ErrServerClosed {
			return err
		}
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
	}
	return nil
}

// resolveDataDir returns the explicit dir if given, else <os-config>/flapp.
func resolveDataDir(explicit string) (string, error) {
	if explicit != "" {
		return explicit, filepathMkdir(explicit)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		base, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	dir := filepath.Join(base, "flapp")
	return dir, filepathMkdir(dir)
}

func filepathMkdir(dir string) error {
	return os.MkdirAll(dir, 0o755)
}
