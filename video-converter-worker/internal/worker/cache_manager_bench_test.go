package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func BenchmarkCacheManager_Cleanup(b *testing.B) {
	tempDir := b.TempDir()

	// Create 1000 dummy files
	for i := 0; i < 1000; i++ {
		filePath := filepath.Join(tempDir, fmt.Sprintf("test_%d.tmp", i))
		os.WriteFile(filePath, make([]byte, 1024), 0644)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		cm := NewCacheManager(tempDir, 200*1024, 24*time.Hour) // This triggers both maxAge and maxSize

		// Restore files for next run
		for j := 0; j < 1000; j++ {
			filePath := filepath.Join(tempDir, fmt.Sprintf("test_%d.tmp", j))
			os.WriteFile(filePath, make([]byte, 1024), 0644)
			if j < 500 {
				oldTime := time.Now().Add(-48 * time.Hour)
				os.Chtimes(filePath, oldTime, oldTime)
			}
		}

		b.StartTimer()
		cm.Cleanup()
	}
}
