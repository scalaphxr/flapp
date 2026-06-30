// Package dedup implements the multi-level duplicate-detection engine. It
// recognises two classes of duplicate:
//
//   - Exact: identical bytes, detected by SHA-256 (with MD5 as a secondary
//     key). Two files with different names but identical content collapse to
//     one library entry.
//   - Acoustic: perceptually identical audio that differs at the byte level
//     (re-encode, different header, trimmed silence). Detected by Hamming
//     distance between perceptual fingerprints, gated behind the "deep" flag.
//
// The Index is an in-memory accelerator for a single harvest run; cross-run
// deduplication is handled by the repository (FindByHash / FindByFingerprint).
package dedup

import (
	"strings"

	"github.com/flapp/core/internal/domain"
	"github.com/flapp/core/internal/infrastructure/audio"
)

// DefaultAcousticThreshold is the maximum Hamming distance (in bits, out of the
// 496-bit fingerprint) at which two sounds are treated as acoustically
// identical. Calibrated against the fingerprint engine: a re-encode or moderate
// gain/trim of the same sound stays under ~64 bits, a near-but-distinct pitch
// sits around ~110, and clearly different sounds are 200+. 80 (~16%) catches the
// former without collapsing the latter. The value is user-tunable in Settings
// because the precision/recall tradeoff is catalog-dependent.
const DefaultAcousticThreshold = 80

// Kind classifies the outcome of a duplicate check.
type Kind string

const (
	Unique   Kind = "unique"
	Exact    Kind = "exact"
	Acoustic Kind = "acoustic"
)

type nameID struct {
	id   int64
	name string // lowercase filename, used for name-based dedup gate
}

type fingerprintRef struct {
	fp   string
	id   int64
	name string
}

// Index tracks the hashes and fingerprints seen so far in a harvest run.
// It is not safe for concurrent use; callers serialise dedup decisions (the
// store stage is single-threaded by design so duplicates resolve deterministically).
type Index struct {
	bySHA     map[string]nameID
	byMD5     map[string]nameID
	prints    []fingerprintRef
	threshold int
	deep      bool
}

// NewIndex creates an empty dedup index. When deep is false, only exact
// (hash) duplicates are detected. threshold <= 0 falls back to the default.
func NewIndex(deep bool, threshold int) *Index {
	if threshold <= 0 {
		threshold = DefaultAcousticThreshold
	}
	return &Index{
		bySHA:     make(map[string]nameID),
		byMD5:     make(map[string]nameID),
		threshold: threshold,
		deep:      deep,
	}
}

// Check reports whether the given fingerprints match something already indexed.
// Returns the matched sample id, kind of match, and the existing entry's name
// (lowercase). Callers should only treat the result as a true duplicate when
// the existing name equals the candidate name (case-insensitive).
func (ix *Index) Check(md5, sha256, fingerprint string) (id int64, kind Kind, existingName string) {
	if sha256 != "" {
		if e, ok := ix.bySHA[sha256]; ok {
			return e.id, Exact, e.name
		}
	}
	if md5 != "" {
		if e, ok := ix.byMD5[md5]; ok {
			return e.id, Exact, e.name
		}
	}
	if ix.deep && fingerprint != "" {
		if eid, ename, ok := ix.nearestFingerprint(fingerprint); ok {
			return eid, Acoustic, ename
		}
	}
	return 0, Unique, ""
}

// nearestFingerprint returns the closest indexed fingerprint within threshold.
func (ix *Index) nearestFingerprint(fp string) (int64, string, bool) {
	bestID := int64(0)
	bestName := ""
	bestDist := ix.threshold + 1
	for _, ref := range ix.prints {
		d := audio.HammingHex(fp, ref.fp)
		if d < 0 {
			continue // length mismatch -> incomparable
		}
		if d < bestDist {
			bestDist = d
			bestID = ref.id
			bestName = ref.name
		}
	}
	if bestDist <= ix.threshold {
		return bestID, bestName, true
	}
	return 0, "", false
}

// Add records a freshly stored unique sample so later candidates can match it.
// name should be the lowercase filename (used for the name-equality gate).
func (ix *Index) Add(id int64, md5, sha256, fingerprint, name string) {
	low := strings.ToLower(name)
	if sha256 != "" {
		ix.bySHA[sha256] = nameID{id: id, name: low}
	}
	if md5 != "" {
		ix.byMD5[md5] = nameID{id: id, name: low}
	}
	if ix.deep && fingerprint != "" {
		ix.prints = append(ix.prints, fingerprintRef{fp: fingerprint, id: id, name: low})
	}
}

// Seed preloads the index from samples already in the library, so a new
// harvest deduplicates against the existing collection, not just within itself.
func (ix *Index) Seed(existing []*domain.Sample) {
	for _, s := range existing {
		ix.Add(s.ID, s.MD5, s.SHA256, s.Fingerprint, s.Name)
	}
}

// Accumulator tallies the running dedup statistics shown to the user.
type Accumulator struct {
	stats domain.DedupStats
}

// NewAccumulator returns a zeroed statistics accumulator.
func NewAccumulator() *Accumulator { return &Accumulator{} }

// AddUnique records a kept, unique file of the given size.
func (a *Accumulator) AddUnique(size int64) {
	a.stats.FilesFound++
	a.stats.UniqueFiles++
	a.stats.BytesTotal += size
}

// AddDuplicate records a discarded duplicate of the given size.
func (a *Accumulator) AddDuplicate(size int64) {
	a.stats.FilesFound++
	a.stats.Duplicates++
	a.stats.BytesTotal += size
	a.stats.BytesSaved += size
}

// NoteProject increments the parsed-project counter.
func (a *Accumulator) NoteProject() { a.stats.ProjectsParsed++ }

// NoteArchive increments the opened-archive counter.
func (a *Accumulator) NoteArchive() { a.stats.ArchivesOpened++ }

// Stats returns the accumulated statistics snapshot.
func (a *Accumulator) Stats() domain.DedupStats { return a.stats }
