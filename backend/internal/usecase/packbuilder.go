package usecase

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/flapp/core/internal/domain"
)

// PackBuilderService assembles a chosen set of samples into a single exported
// archive (a drum kit or sample pack). Files are optionally foldered by
// category so the resulting kit is browsable in any DAW.
type PackBuilderService struct {
	samples domain.SampleRepository
	packer  domain.Packer
	outDir  string
	midiSvc *MidiExtractService
}

// NewPackBuilderService wires a pack-builder service.
func NewPackBuilderService(samples domain.SampleRepository, packer domain.Packer, outDir string, midiSvc *MidiExtractService) *PackBuilderService {
	return &PackBuilderService{samples: samples, packer: packer, outDir: outDir, midiSvc: midiSvc}
}

// PackRequest describes a pack to build.
type PackRequest struct {
	Name            string  `json:"name"`
	SampleIDs       []int64 `json:"sampleIds"`
	GroupByCategory bool    `json:"groupByCategory"`
	Format          string  `json:"format"`          // currently "zip"
	IncludeMidi     bool    `json:"includeMidi"`     // включить .mid файлы в пак
	MidiGroupMode   string  `json:"midiGroupMode"`   // "flat" | "by_project"
}

// Build gathers the requested samples, lays out their archive paths, and writes
// the pack. It is intended to be run inside a job; progress is forwarded from
// the packer. Returns a result map with the output path and file count.
func (s *PackBuilderService) Build(ctx context.Context, req PackRequest, report domain.ProgressReporter) (map[string]interface{}, error) {
	report.Set(0, "Подготовка пака", "")

	entries := make([]domain.PackEntry, 0, len(req.SampleIDs))
	usedArc := map[string]int{}
	for _, id := range req.SampleIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		smp, err := s.samples.GetByID(ctx, id)
		if err != nil {
			continue
		}
		arc := s.arcPath(smp, req.GroupByCategory, usedArc)
		entries = append(entries, domain.PackEntry{SourcePath: smp.Path, ArcPath: arc})
	}
	// Добавляем .mid файлы если запрошено.
	if req.IncludeMidi && s.midiSvc != nil {
		clips := s.midiSvc.ListClips("")
		for _, clip := range clips {
			if clip.FilePath == "" {
				continue
			}
			var arcPath string
			if req.MidiGroupMode == "by_project" {
				arcPath = path.Join("midi", clip.SourceName, clip.FileName)
			} else {
				arcPath = path.Join("midi", clip.FileName)
			}
			entries = append(entries, domain.PackEntry{SourcePath: clip.FilePath, ArcPath: arcPath})
		}
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: no readable samples selected", domain.ErrInvalidInput)
	}

	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "zip"
	}
	dest := filepath.Join(s.outDir, sanitizeName(packBaseName(req.Name))+"."+format)

	report.Set(0.05, "Сборка архива", filepath.Base(dest))
	err := s.packer.Pack(ctx, dest, format, entries, func(done, total int) {
		if total > 0 {
			report.Set(0.05+0.95*float64(done)/float64(total), "Сборка архива", "")
		}
	})
	if err != nil {
		return nil, err
	}
	report.Set(1, "Готово", fmt.Sprintf("%d звуков", len(entries)))
	return map[string]interface{}{
		"path":  dest,
		"count": len(entries),
	}, nil
}

// arcPath builds a unique in-archive path for a sample, optionally nested under
// its category folder, using the human display name.
func (s *PackBuilderService) arcPath(smp *domain.Sample, group bool, used map[string]int) string {
	name := sanitizeName(smp.Name)
	if filepath.Ext(name) == "" && smp.Ext != "" {
		name = name + "." + strings.TrimPrefix(smp.Ext, ".")
	}
	var arc string
	if group {
		arc = path.Join(string(smp.Category), name)
	} else {
		arc = name
	}
	// De-collide identical destinations.
	if n, seen := used[arc]; seen {
		n++
		used[arc] = n
		ext := path.Ext(arc)
		stem := strings.TrimSuffix(arc, ext)
		return fmt.Sprintf("%s (%d)%s", stem, n, ext)
	}
	used[arc] = 0
	return arc
}

// ExportToFolder copies samples into destDir organised into category sub-folders.
//   destDir/808/kick_heavy.wav
//   destDir/Snare/sn_crispy.wav
//   …
func (s *PackBuilderService) ExportToFolder(ctx context.Context, sampleIDs []int64, destDir string, report domain.ProgressReporter) (map[string]interface{}, error) {
	report.Set(0, "Экспорт", "")
	total := len(sampleIDs)
	copied := 0
	skipped := 0
	usedNames := map[string]int{} // dedup within each category folder

	for i, id := range sampleIDs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		smp, err := s.samples.GetByID(ctx, id)
		if err != nil {
			skipped++
			continue
		}
		catDir := filepath.Join(destDir, sanitizeName(string(smp.Category)))
		if err := os.MkdirAll(catDir, 0o755); err != nil {
			skipped++
			continue
		}
		name := sanitizeName(smp.Name)
		if filepath.Ext(name) == "" && smp.Ext != "" {
			name = name + "." + strings.TrimPrefix(smp.Ext, ".")
		}
		key := string(smp.Category) + "/" + name
		if n, seen := usedNames[key]; seen {
			n++
			usedNames[key] = n
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		} else {
			usedNames[key] = 0
		}
		dst := filepath.Join(catDir, name)
		if err := copyFile(smp.Path, dst); err != nil {
			skipped++
		} else {
			copied++
		}
		report.Set(float64(i+1)/float64(total), "Экспорт", smp.Name)
	}
	// Copy MIDI clips into MIDI/ subfolder alongside category folders.
	if s.midiSvc != nil {
		clips := s.midiSvc.ListClips("")
		if len(clips) > 0 {
			midiDestDir := filepath.Join(destDir, "MIDI")
			if err := os.MkdirAll(midiDestDir, 0o755); err == nil {
				usedMidi := map[string]int{}
				for _, clip := range clips {
					if clip.FilePath == "" {
						continue
					}
					if _, err := os.Stat(clip.FilePath); err != nil {
						continue
					}
					name := clip.FileName
					key := name
					if n, seen := usedMidi[key]; seen {
						n++
						usedMidi[key] = n
						ext := filepath.Ext(name)
						stem := strings.TrimSuffix(name, ext)
						name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
					} else {
						usedMidi[key] = 0
					}
					_ = copyFile(clip.FilePath, filepath.Join(midiDestDir, name))
				}
			}
		}
	}

	report.Set(1, "Готово", fmt.Sprintf("скопировано %d", copied))
	return map[string]interface{}{
		"copied":  copied,
		"skipped": skipped,
		"destDir": destDir,
	}, nil
}

func packBaseName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Sample Pack"
	}
	return name
}
