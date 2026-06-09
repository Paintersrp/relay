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

func BuildPreviewDiff(original string, transformed string, maxLines int) PreviewDiff {
	if maxLines <= 0 {
		maxLines = 300
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

	// Truncate if needed
	truncated := false
	if len(result) > maxLines {
		result = result[:maxLines]
		truncated = true
	}

	return PreviewDiff{
		Lines:     result,
		Truncated: truncated,
	}
}

func splitPreviewLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
