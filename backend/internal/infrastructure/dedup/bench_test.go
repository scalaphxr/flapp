package dedup_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/flapp/core/internal/infrastructure/dedup"
)

func BenchmarkQuickHash_SmallFile(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "small.wav")
	os.WriteFile(path, make([]byte, 10*1024), 0o644)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dedup.QuickHash(path)
	}
}

func BenchmarkQuickHash_LargeFile(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "large.wav")
	os.WriteFile(path, make([]byte, 10*1024*1024), 0o644) // 10 MB
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dedup.QuickHash(path)
	}
}

func BenchmarkIndex_Check(b *testing.B) {
	idx := dedup.NewIndex(true, 80)
	for i := 0; i < 10000; i++ {
		sha := fmt.Sprintf("%064x", i)
		fp := fmt.Sprintf("%064x", i*7)
		idx.Add(int64(i), "", sha, fp, "sample.wav")
	}
	sha := fmt.Sprintf("%064x", 9999)
	fp := fmt.Sprintf("%064x", 9999*7)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _ = idx.Check("", sha, fp)
	}
}
