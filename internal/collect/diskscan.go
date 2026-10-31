package collect

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

// ScanResult is the outcome of walking one root directory.
type ScanResult struct {
	Scan    model.DiskScan
	Entries []model.DiskEntry
}

// ScanDir walks root without following symlinks and aggregates the size
// of every directory (like `du`). To keep the database small, only
// entries of at least minBytes are kept — except every directory up to
// depth 4 under the root, so the drill-down tree always has its top
// levels even when a folder is small.
func ScanDir(root string, minBytes int64) ScanResult {
	start := time.Now()
	root = filepath.Clean(root)
	dirSizes := map[string]int64{root: 0}
	var files []model.DiskEntry
	var fileCount, errCount int64

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			errCount++
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil // never follow symlinks: avoids cycles and double counts
		}
		if d.IsDir() && isReparsePoint(path, d) {
			return fs.SkipDir // NTFS junction: same reasoning as symlinks
		}
		if d.IsDir() {
			if _, ok := dirSizes[path]; !ok {
				dirSizes[path] = 0
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			errCount++
			return nil
		}
		size := info.Size()
		fileCount++
		// Add this file's size to every ancestor up to the root.
		for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
			dirSizes[dir] += size
			if dir == root || dir == filepath.Dir(dir) {
				break
			}
		}
		if size >= minBytes {
			files = append(files, model.DiskEntry{
				Path: path, Parent: filepath.Dir(path), Name: d.Name(), Size: size,
			})
		}
		return nil
	})

	entries := make([]model.DiskEntry, 0, len(dirSizes)+len(files))
	for path, size := range dirSizes {
		depth := depthUnder(root, path)
		if path != root && size < minBytes && depth > 4 {
			continue
		}
		entries = append(entries, model.DiskEntry{
			Path: path, Parent: filepath.Dir(path),
			Name: filepath.Base(path), IsDir: true, Size: size,
		})
	}
	entries = append(entries, files...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Size > entries[j].Size })

	return ScanResult{
		Scan: model.DiskScan{
			Time: start, Root: root, DurationMs: time.Since(start).Milliseconds(),
			TotalBytes: dirSizes[root], FileCount: fileCount, ErrCount: errCount,
		},
		Entries: entries,
	}
}

func depthUnder(root, path string) int {
	if path == root {
		return 0
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return 99
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}
