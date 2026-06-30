package usecase

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/flapp/core/internal/domain"
)

// BeatManagerService performs mass file operations over the library, chiefly
// the batch-rename engine. Renaming is a two-phase feature: Preview computes the
// resulting names purely (no side effects) so the UI can show a before/after
// table, and Apply performs the physical renames and database updates, driven
// as a background job for large selections.
type BeatManagerService struct {
	samples  domain.SampleRepository
	storeDir string
}

// NewBeatManagerService wires a beat-manager service.
func NewBeatManagerService(samples domain.SampleRepository, storeDir string) *BeatManagerService {
	return &BeatManagerService{samples: samples, storeDir: storeDir}
}

// Rename operation type identifiers (stable wire values used by the frontend).
const (
	OpUpper            = "upper"
	OpLower            = "lower"
	OpTitle            = "title"
	OpStripLeadingNum  = "strip_leading_digits"
	OpStripTrailingNum = "strip_trailing_digits"
	OpRemoveSpecial    = "remove_special"
	OpTrim             = "trim"
	OpPrefix           = "prefix"
	OpSuffix           = "suffix"
	OpReplace          = "replace"
	OpRegexReplace     = "regex_replace"
	OpInsertBeforeBPM  = "before_bpm"
	OpInsertAfterBPM   = "after_bpm"
	OpSmartMarketing   = "smart"
)

// RenameOp is a single transform in a rename pipeline.
type RenameOp struct {
	Type  string `json:"type"`
	From  string `json:"from"`  // replace / regex_replace pattern
	To    string `json:"to"`    // replace / regex_replace replacement
	Text  string `json:"text"`  // prefix / suffix / before-after BPM insert
	Regex bool   `json:"regex"` // treat From as a regular expression
}

// RenameSpec is an ordered list of transforms applied to each selected sample.
type RenameSpec struct {
	Ops []RenameOp `json:"ops"`
}

// RenamePreview is one before/after pair.
type RenamePreview struct {
	ID      int64  `json:"id"`
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
	Changed bool   `json:"changed"`
}

// Preview computes the new names for the given samples without touching disk.
func (s *BeatManagerService) Preview(ctx context.Context, ids []int64, spec RenameSpec) ([]RenamePreview, error) {
	out := make([]RenamePreview, 0, len(ids))
	for _, id := range ids {
		smp, err := s.samples.GetByID(ctx, id)
		if err != nil {
			continue
		}
		newName := applyRename(smp, spec)
		out = append(out, RenamePreview{
			ID:      id,
			OldName: smp.Name,
			NewName: newName,
			Changed: newName != smp.Name,
		})
	}
	return out, nil
}

// Apply performs the renames: it rewrites each managed file's name on disk
// (de-colliding within the store directory) and updates the database row.
// Progress is reported per file. Returns a result map with the changed count.
func (s *BeatManagerService) Apply(ctx context.Context, ids []int64, spec RenameSpec, report domain.ProgressReporter) (map[string]interface{}, error) {
	report.Set(0, "Переименование", "")
	used := map[string]bool{}
	changed := 0
	total := len(ids)

	for i, id := range ids {
		if err := ctx.Err(); err != nil {
			return map[string]interface{}{"renamed": changed}, err
		}
		smp, err := s.samples.GetByID(ctx, id)
		if err != nil {
			continue
		}
		newName := applyRename(smp, spec)
		if newName == "" || newName == smp.Name {
			report.Set(progressFrac(i+1, total), "Переименование", smp.Name)
			continue
		}

		newPath, derr := s.renameOnDisk(smp, newName, used)
		if derr != nil {
			// Skip files that cannot be renamed (locked, missing) but keep going.
			report.Set(progressFrac(i+1, total), "Переименование", smp.Name)
			continue
		}
		if err := s.samples.Rename(ctx, id, newName, newPath); err != nil {
			continue
		}
		changed++
		report.Set(progressFrac(i+1, total), "Переименование", newName)
	}
	report.Set(1, "Готово", fmt.Sprintf("Переименовано: %d", changed))
	return map[string]interface{}{"renamed": changed, "total": total}, nil
}

// renameOnDisk renames the managed copy to reflect the new display name while
// keeping it unique within the store directory and preserving its extension.
func (s *BeatManagerService) renameOnDisk(smp *domain.Sample, newName string, used map[string]bool) (string, error) {
	dir := filepath.Dir(smp.Path)
	ext := filepath.Ext(smp.Path)
	base := sanitizeName(strings.TrimSuffix(newName, filepath.Ext(newName)))

	candidate := filepath.Join(dir, base+ext)
	n := 1
	for {
		_, statErr := os.Stat(candidate)
		taken := used[strings.ToLower(candidate)] || (statErr == nil && candidate != smp.Path)
		if !taken {
			break
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
		n++
	}
	if candidate == smp.Path {
		used[strings.ToLower(candidate)] = true
		return candidate, nil
	}
	if err := os.Rename(smp.Path, candidate); err != nil {
		return "", err
	}
	used[strings.ToLower(candidate)] = true
	return candidate, nil
}

// applyRename runs the full pipeline on a sample's name and returns the result.
// The extension is preserved; transforms operate on the stem only.
func applyRename(smp *domain.Sample, spec RenameSpec) string {
	ext := filepath.Ext(smp.Name)
	stem := strings.TrimSuffix(smp.Name, ext)

	for _, op := range spec.Ops {
		stem = applyOp(stem, op, smp)
	}
	stem = strings.TrimSpace(stem)
	if stem == "" {
		stem = strings.TrimSuffix(smp.Name, ext)
	}
	return stem + ext
}

var (
	leadingDigitsRe  = regexp.MustCompile(`^[\s0-9._-]+`)
	trailingDigitsRe = regexp.MustCompile(`[\s0-9._-]+$`)
	specialRe        = regexp.MustCompile(`[^\p{L}\p{N}\s_-]+`)
	multiSpaceRe     = regexp.MustCompile(`\s{2,}`)
)

// applyOp applies one transform to a name stem.
func applyOp(stem string, op RenameOp, smp *domain.Sample) string {
	switch op.Type {
	case OpUpper:
		return strings.ToUpper(stem)
	case OpLower:
		return strings.ToLower(stem)
	case OpTitle:
		return titleCase(stem)
	case OpStripLeadingNum:
		return leadingDigitsRe.ReplaceAllString(stem, "")
	case OpStripTrailingNum:
		return trailingDigitsRe.ReplaceAllString(stem, "")
	case OpRemoveSpecial:
		return strings.TrimSpace(multiSpaceRe.ReplaceAllString(specialRe.ReplaceAllString(stem, " "), " "))
	case OpTrim:
		return strings.TrimSpace(multiSpaceRe.ReplaceAllString(stem, " "))
	case OpPrefix:
		return op.Text + stem
	case OpSuffix:
		return stem + op.Text
	case OpReplace:
		if op.From == "" {
			return stem
		}
		if op.Regex {
			if re, err := regexp.Compile(op.From); err == nil {
				return re.ReplaceAllString(stem, op.To)
			}
			return stem
		}
		return strings.ReplaceAll(stem, op.From, op.To)
	case OpRegexReplace:
		if op.From == "" {
			return stem
		}
		if re, err := regexp.Compile(op.From); err == nil {
			return re.ReplaceAllString(stem, op.To)
		}
		return stem
	case OpInsertBeforeBPM:
		return insertAroundBPM(stem, op.Text, smp, true)
	case OpInsertAfterBPM:
		return insertAroundBPM(stem, op.Text, smp, false)
	case OpSmartMarketing:
		return smartMarketingName(smp)
	default:
		return stem
	}
}

// titleCase upper-cases the first letter of each word, treating whitespace and
// the common filename separators "_" and "-" as word boundaries.
func titleCase(s string) string {
	atBoundary := true
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '_' || r == '-' {
			atBoundary = true
			return r
		}
		if atBoundary {
			atBoundary = false
			return unicode.ToUpper(r)
		}
		return unicode.ToLower(r)
	}, s)
}

// insertAroundBPM inserts text immediately before or after the first BPM-like
// number in the name; if no BPM token is present it appends the text.
func insertAroundBPM(stem, text string, smp *domain.Sample, before bool) string {
	if text == "" {
		return stem
	}
	loc := bpmTokenRe.FindStringIndex(stem)
	if loc == nil {
		// No literal BPM in the name: fall back to the project BPM if known.
		if smp != nil && smp.BPM > 0 {
			if before {
				return fmt.Sprintf("%s %s %dBPM", strings.TrimSpace(stem), text, smp.BPM)
			}
			return fmt.Sprintf("%s %dBPM %s", strings.TrimSpace(stem), smp.BPM, text)
		}
		return strings.TrimSpace(stem) + " " + text
	}
	if before {
		return stem[:loc[0]] + text + " " + stem[loc[0]:]
	}
	return stem[:loc[1]] + " " + text + stem[loc[1]:]
}

var bpmTokenRe = regexp.MustCompile(`(?i)\b\d{2,3}\s?bpm\b`)

// smartMarketingName builds a clean, store-ready name from a sample's metadata,
// e.g. "Dark 808 - 140BPM - Am". Missing fields are simply omitted.
func smartMarketingName(smp *domain.Sample) string {
	parts := []string{}
	if len(smp.Tags) > 0 {
		parts = append(parts, titleCase(smp.Tags[0]))
	}
	if smp.Category != "" {
		parts = append(parts, string(smp.Category))
	}
	head := strings.Join(parts, " ")
	if head == "" {
		head = strings.TrimSuffix(smp.Name, filepath.Ext(smp.Name))
	}

	segs := []string{head}
	if smp.BPM > 0 {
		segs = append(segs, fmt.Sprintf("%dBPM", smp.BPM))
	}
	if smp.KeyName != "" {
		segs = append(segs, smp.KeyName)
	}
	return strings.Join(segs, " - ")
}

func progressFrac(done, total int) float64 {
	if total <= 0 {
		return 1
	}
	return float64(done) / float64(total)
}

// FileRenamePreview is a before/after pair for an arbitrary on-disk file.
type FileRenamePreview struct {
	Path    string `json:"path"`
	OldName string `json:"oldName"`
	NewName string `json:"newName"`
	Changed bool   `json:"changed"`
}

// PreviewFiles computes rename previews for arbitrary on-disk paths without
// touching the database or the disk. The BPM/key/tags fields are empty for
// plain files, so ops that depend on them (smart, before_bpm) degrade
// gracefully to name-only behaviour.
func (s *BeatManagerService) PreviewFiles(_ context.Context, paths []string, spec RenameSpec) ([]FileRenamePreview, error) {
	out := make([]FileRenamePreview, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		smp := &domain.Sample{Name: name}
		newName := applyRename(smp, spec)
		out = append(out, FileRenamePreview{
			Path:    p,
			OldName: name,
			NewName: newName,
			Changed: newName != name,
		})
	}
	return out, nil
}

// ApplyFiles renames files on disk at the given paths according to spec.
// Files that produce no name change or that cannot be renamed are skipped.
func (s *BeatManagerService) ApplyFiles(ctx context.Context, paths []string, spec RenameSpec, report domain.ProgressReporter) (map[string]interface{}, error) {
	report.Set(0, "Переименование файлов", "")
	renamed, skipped := 0, 0
	total := len(paths)

	for i, p := range paths {
		if ctx.Err() != nil {
			break
		}
		name := filepath.Base(p)
		smp := &domain.Sample{Name: name}
		newName := applyRename(smp, spec)
		if newName == "" || newName == name {
			report.Set(progressFrac(i+1, total), "Пропущен", name)
			continue
		}
		dir := filepath.Dir(p)
		dest := filepath.Join(dir, newName)
		// De-collide if destination already exists (and is not the source itself).
		if _, err := os.Stat(dest); err == nil && dest != p {
			ext := filepath.Ext(newName)
			stem := strings.TrimSuffix(newName, ext)
			for n := 1; ; n++ {
				candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
				if _, err2 := os.Stat(candidate); os.IsNotExist(err2) {
					dest = candidate
					break
				}
			}
		}
		if err := os.Rename(p, dest); err != nil {
			skipped++
			report.Set(progressFrac(i+1, total), "Пропущен", name)
			continue
		}
		renamed++
		report.Set(progressFrac(i+1, total), "Переименование", newName)
	}
	report.Set(1, "Готово", fmt.Sprintf("Переименовано: %d", renamed))
	return map[string]interface{}{"renamed": renamed, "skipped": skipped}, nil
}
