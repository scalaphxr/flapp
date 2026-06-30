// Package usecase contains the application services that orchestrate the
// domain ports into the product's features: harvesting, library search, batch
// renaming, pack building, analytics and smart search. Services depend only on
// domain interfaces (dependency inversion), so the concrete infrastructure
// (SQLite, archive readers, the audio engine) is injected and swappable.
package usecase

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// audio extension sets. The base set is always scanned; the extended set is
// gated behind the harvest request's ExtraFormats flag.
var (
	baseAudioExts  = map[string]bool{".wav": true, ".mp3": true}
	extraAudioExts = map[string]bool{".flac": true, ".ogg": true, ".aiff": true, ".aif": true, ".m4a": true}
	archiveExts    = map[string]bool{".zip": true, ".rar": true, ".7z": true}
)

// fileClass categorises a path during scanning.
type fileClass int

const (
	classOther fileClass = iota
	classAudio
	classProject
	classArchive
)

// scanResult holds everything discovered under the harvest inputs.
type scanResult struct {
	archives []discovered
	projects []discovered
	audio    []discovered
}

// discovered is one located file plus the breadcrumb of where it came from.
type discovered struct {
	path     string
	relPath  string // path relative to the dropped root (folder hints for the classifier)
	name     string
	ext      string
	size     int64
	rawBytes []byte // если не nil: содержимое уже в памяти (FLP из архива, temp-файл удалён)
}

// audioExtSet returns the active audio extension set for a request.
func audioExtSet(includeExtra bool) map[string]bool {
	set := make(map[string]bool, len(baseAudioExts)+len(extraAudioExts))
	for e := range baseAudioExts {
		set[e] = true
	}
	if includeExtra {
		for e := range extraAudioExts {
			set[e] = true
		}
	}
	return set
}

// classify decides how a single path should be treated.
func classify(path string, audioExts map[string]bool) fileClass {
	ext := strings.ToLower(filepath.Ext(path))
	switch {
	case ext == ".flp":
		return classProject
	case archiveExts[ext]:
		return classArchive
	case audioExts[ext]:
		return classAudio
	default:
		return classOther
	}
}

// scanInputs walks every dropped input and buckets discovered files. Folders
// are traversed recursively; individual files are classified directly. relPath
// is computed against the dropped root so the classifier keeps folder context
// (e.g. ".../Drums/Kicks/kick.wav").
func scanInputs(inputs []string, includeExtra bool) (scanResult, error) {
	audioExts := audioExtSet(includeExtra)
	var res scanResult

	add := func(root, path string, info fs.FileInfo) {
		rel := relativeTo(root, path)
		d := discovered{
			path:    path,
			relPath: rel,
			name:    filepath.Base(path),
			ext:     strings.ToLower(filepath.Ext(path)),
			size:    info.Size(),
		}
		switch classify(path, audioExts) {
		case classArchive:
			res.archives = append(res.archives, d)
		case classProject:
			res.projects = append(res.projects, d)
		case classAudio:
			res.audio = append(res.audio, d)
		}
	}

	for _, in := range inputs {
		info, err := os.Stat(in)
		if err != nil {
			continue // skip vanished inputs rather than failing the whole run
		}
		if !info.IsDir() {
			add(filepath.Dir(in), in, info)
			continue
		}
		root := in
		_ = filepath.WalkDir(in, func(p string, de fs.DirEntry, err error) error {
			if err != nil {
				return nil // tolerate unreadable subtrees
			}
			if de.IsDir() {
				return nil
			}
			fi, err := de.Info()
			if err != nil {
				return nil
			}
			add(root, p, fi)
			return nil
		})
	}
	return res, nil
}

// relativeTo returns path relative to root, falling back to the base name.
func relativeTo(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}
