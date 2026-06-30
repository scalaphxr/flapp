// Package usecase — unresolved asset resolution.
//
// When the FLP parser finds a sample path that cannot be located on disk,
// it is recorded as an "unresolved asset". The UI can display these so the
// user knows what samples are missing.
//
// Resolution strategy (in priority order):
//  1. Exact path match (file exists as-is).
//  2. Resolve relative to the project directory.
//  3. Resolve relative to a configured "drumkits" root.
//  4. Case-insensitive basename match in any known library path.
//  5. Record as unresolved if none of the above succeed.
package usecase

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// ResolveFlpSamplePath attempts to find the actual on-disk location for a
// path extracted from an .flp file.
//
// Parameters:
//   rawPath    – the original path string from the FLP event stream.
//   projectDir – directory containing the .flp file.
//   roots      – additional search roots (drumkits dir, library store, etc.)
//
// Returns the resolved absolute path, or ("", false) if the file cannot be found.
func ResolveFlpSamplePath(rawPath, projectDir string, roots []string) (string, bool) {
	if rawPath == "" {
		return "", false
	}

	// 1. Exact path (as-is, for local-machine paths that are already correct).
	if fileExists(rawPath) {
		return rawPath, true
	}

	// Normalise separators only — keep drive letter intact so Windows-to-Windows
	// resolution still works. Drive-letter stripping only happens in
	// NormalizeFLPSamplePath for storage purposes.
	norm := filepath.FromSlash(strings.ReplaceAll(rawPath, "\\", "/"))
	if norm != rawPath && fileExists(norm) {
		return norm, true
	}

	base := filepath.Base(norm)
	baseLower := strings.ToLower(base)

	// 2. Relative to project directory.
	if projectDir != "" {
		if candidate := filepath.Join(projectDir, base); fileExists(candidate) {
			return candidate, true
		}
	}

	// 3. Search additional roots by basename (case-insensitive, one level deep).
	for _, root := range roots {
		if candidate := filepath.Join(root, base); fileExists(candidate) {
			return candidate, true
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() && strings.ToLower(e.Name()) == baseLower {
				return filepath.Join(root, e.Name()), true
			}
		}
	}
	return "", false
}

// NormalizeFLPSamplePath converts a raw FLP sample path to a canonical form
// suitable for cross-platform storage and comparison.
func NormalizeFLPSamplePath(rawPath string) string {
	return normalizePathSeparators(rawPath)
}

func normalizePathSeparators(p string) string {
	// Unify separators.
	p = strings.ReplaceAll(p, "\\", "/")
	// Strip Windows drive letter (C:/ → /).
	if len(p) >= 3 && p[1] == ':' && p[2] == '/' {
		p = p[2:]
	}
	return filepath.FromSlash(p)
}

// recordUnresolvedForProject records all unresolved paths for projectID.
// It skips paths that can be resolved against the given roots.
func recordUnresolvedForProject(
	ctx context.Context,
	projectID int64,
	rawPaths []string,
	projectDir string,
	roots []string,
	record func(ctx context.Context, projectID int64, rawPath, normalized string) error,
) {
	for _, raw := range rawPaths {
		if _, ok := ResolveFlpSamplePath(raw, projectDir, roots); ok {
			continue // found on disk → not unresolved
		}
		norm := NormalizeFLPSamplePath(raw)
		_ = record(ctx, projectID, raw, norm)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
