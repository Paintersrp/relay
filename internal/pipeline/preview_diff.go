package pipeline

import "strings"

type PreviewDiffLine struct {
	Kind string
	Text string
}

type PreviewDiff struct {
	Lines     []PreviewDiffLine
	Truncated bool
}

const DefaultPreviewDiffMaxLines = 300
const DefaultPreviewDiffContextLines = 3

func BuildPreviewDiff(original string, transformed string, maxLines int) PreviewDiff {
	if maxLines <= 0 {
		maxLines = DefaultPreviewDiffMaxLines
	}

	a := splitPreviewLines(original)
	b := splitPreviewLines(transformed)

	m := len(a)
	n := len(b)

	// LCS DP table
	dp := make([][]int, m+1)
	for i := range dp {
		dp[i] = make([]int, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] {
					dp[i][j] = dp[i-1][j]
				} else {
					dp[i][j] = dp[i][j-1]
				}
			}
		}
	}

	// Walk back LCS table to produce diff
	var result []PreviewDiffLine
	i, j := m, n
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && a[i-1] == b[j-1] {
			result = append(result, PreviewDiffLine{Kind: "same", Text: a[i-1]})
			i--
			j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			result = append(result, PreviewDiffLine{Kind: "add", Text: b[j-1]})
			j--
		} else {
			result = append(result, PreviewDiffLine{Kind: "remove", Text: a[i-1]})
			i--
		}
	}

	// Reverse result to original order
	for left, right := 0, len(result)-1; left < right; left, right = left+1, right-1 {
		result[left], result[right] = result[right], result[left]
	}

	return compactDiffHunks(result, DefaultPreviewDiffContextLines, maxLines)
}

type lineRange struct {
	start, end int
}

func compactDiffHunks(lines []PreviewDiffLine, contextLines int, maxLines int) PreviewDiff {
	if maxLines <= 0 {
		maxLines = DefaultPreviewDiffMaxLines
	}
	if contextLines < 0 {
		contextLines = 0
	}

	var ranges []lineRange
	for i, line := range lines {
		if line.Kind == "add" || line.Kind == "remove" {
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			end := i + contextLines
			if end >= len(lines) {
				end = len(lines) - 1
			}
			ranges = appendMerged(ranges, start, end)
		}
	}

	if len(ranges) == 0 {
		return PreviewDiff{}
	}

	var compact []PreviewDiffLine
	for i, r := range ranges {
		if i > 0 && r.start > ranges[i-1].end+1 {
			compact = append(compact, PreviewDiffLine{Kind: "skip", Text: "... unchanged lines omitted ..."})
		}
		compact = append(compact, lines[r.start:r.end+1]...)
	}

	truncated := false
	if len(compact) > maxLines {
		compact = compact[:maxLines]
		truncated = true
	}

	return PreviewDiff{Lines: compact, Truncated: truncated}
}

func appendMerged(ranges []lineRange, start, end int) []lineRange {
	if len(ranges) == 0 {
		return append(ranges, lineRange{start, end})
	}
	last := &ranges[len(ranges)-1]
	if start <= last.end+1 {
		if end > last.end {
			last.end = end
		}
		return ranges
	}
	return append(ranges, lineRange{start, end})
}

func splitPreviewLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
