package review

import (
	"fmt"
	"strings"
)

func CompressDiff(files []FileDiff, maxBytes int) (string, bool, int, []FileDiff) {
	if maxBytes <= 0 {
		maxBytes = 60000
	}

	var b strings.Builder
	truncated := false
	omitted := 0
	outFiles := make([]FileDiff, 0, len(files))

	for _, file := range files {
		header := fmt.Sprintf("\n--- %s (%s, +%d -%d) ---\n", file.Path, file.Status, file.Additions, file.Deletions)
		remaining := maxBytes - b.Len()
		if remaining <= len(header) {
			truncated = true
			omitted += len(header) + len(file.Patch)
			file.Truncated = true
			outFiles = append(outFiles, file)
			continue
		}

		b.WriteString(header)
		remaining = maxBytes - b.Len()
		patch := file.Patch
		if len(patch) > remaining {
			b.WriteString(patch[:remaining])
			truncated = true
			omitted += len(patch) - remaining
			file.Patch = patch[:remaining]
			file.Truncated = true
		} else {
			b.WriteString(patch)
		}
		outFiles = append(outFiles, file)
	}

	return strings.TrimSpace(b.String()), truncated, omitted, outFiles
}
