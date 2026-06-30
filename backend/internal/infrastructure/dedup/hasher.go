package dedup

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

// Hasher implements domain.Hasher, computing MD5 and SHA-256 in one read.
type Hasher struct{}

func NewHasher() *Hasher { return &Hasher{} }

// Hashes streams the file once through both hash functions.
func (h *Hasher) Hashes(path string) (string, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	m := md5.New()
	s := sha256.New()
	if _, err := io.Copy(io.MultiWriter(m, s), f); err != nil {
		return "", "", err
	}
	return hex.EncodeToString(m.Sum(nil)), hex.EncodeToString(s.Sum(nil)), nil
}
