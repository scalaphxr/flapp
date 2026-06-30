package dedup

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
)

const quickBlock = 65536 // 64 KB front + 64 KB back

// QuickHash computes a probabilistic content fingerprint from the first and
// last 64 KB of a file plus its total size. It reads at most 128 KB regardless
// of file size, making it ~100× faster than a full SHA-256 on large files.
//
// The result is prefixed "q:" to distinguish it from a full content hash.
// Returns "" on any I/O error.
func QuickHash(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return ""
	}
	size := fi.Size()

	h := sha256.New()

	// Encode size so same-prefix files of different lengths hash differently.
	var sb [8]byte
	binary.LittleEndian.PutUint64(sb[:], uint64(size))
	h.Write(sb[:])

	// Front block.
	front := make([]byte, quickBlock)
	n, _ := io.ReadFull(f, front)
	if n > 0 {
		h.Write(front[:n])
	}

	// Back block (only if file is large enough to have a separate tail).
	if size > int64(quickBlock) {
		if _, err := f.Seek(-quickBlock, io.SeekEnd); err == nil {
			back := make([]byte, quickBlock)
			if m, _ := io.ReadFull(f, back); m > 0 {
				h.Write(back[:m])
			}
		}
	}

	return "q:" + hex.EncodeToString(h.Sum(nil))
}

// QuickHashChanged reports whether the quick-hash for path differs from the
// stored hash. Returns true (changed) if either cannot be computed.
func QuickHashChanged(path, stored string) bool {
	current := QuickHash(path)
	if current == "" || stored == "" {
		return true
	}
	return current != stored
}
