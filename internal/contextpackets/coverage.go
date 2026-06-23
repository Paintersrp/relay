package contextpackets

func coverageCounts(entries []ContextCoverageEntry) (covered, blocked, missing int) {
	for _, entry := range entries {
		switch entry.Status {
		case CoverageStatusCovered:
			covered++
		case CoverageStatusBlocked:
			blocked++
		case CoverageStatusMissing:
			missing++
		}
	}
	return covered, blocked, missing
}

func statusFromCoverage(entries []ContextCoverageEntry, truncated bool) string {
	if hasRequiredStatus(entries, CoverageStatusBlocked) || hasRequiredStatus(entries, CoverageStatusMissing) {
		return ContextPacketStatusBlocked
	}
	if truncated || hasAnyStatus(entries, CoverageStatusBlocked) || hasAnyStatus(entries, CoverageStatusMissing) || hasAnyStatus(entries, CoverageStatusPartial) {
		return ContextPacketStatusPartial
	}
	return ContextPacketStatusCreated
}

func hasRequiredStatus(entries []ContextCoverageEntry, status string) bool {
	for _, entry := range entries {
		if entry.Required && entry.Status == status {
			return true
		}
	}
	return false
}

func hasAnyStatus(entries []ContextCoverageEntry, status string) bool {
	for _, entry := range entries {
		if entry.Status == status {
			return true
		}
	}
	return false
}
