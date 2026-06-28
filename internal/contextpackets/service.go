package contextpackets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"relay/internal/sources"
	"relay/internal/store"
)

const (
	defaultMaxSources    = 50
	hardMaxSources       = 200
	defaultMaxTotalBytes = 256 * 1024
	hardMaxTotalBytes    = 1024 * 1024

	LimitHitMaxSources        = "max_sources"
	LimitHitMaxTotalBytes     = "max_total_bytes"
	LimitHitCoverageTruncated = "coverage_truncated"
	LimitHitNone              = "none"
	LimitHitUnknown           = "unknown"
)

var slugTokenPattern = regexp.MustCompile(`[^a-z0-9]+`)

type Service struct {
	store   *store.Store
	sources *sources.Service
}

func NewService(st *store.Store) *Service {
	return &Service{store: st, sources: sources.NewService(st)}
}

func NewServiceWithSources(st *store.Store, sourceService *sources.Service) *Service {
	if sourceService == nil {
		sourceService = sources.NewService(st)
	}
	return &Service{store: st, sources: sourceService}
}

func (s *Service) CreateContextPacket(ctx context.Context, input ContextPacketInput) (*ContextPacketResult, error) {
	if s.store == nil {
		return nil, fmt.Errorf("store is required")
	}
	if s.sources == nil {
		return nil, fmt.Errorf("source service is required")
	}
	projectID := strings.TrimSpace(input.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("project_id is required")
	}

	// Validate that at least one source request (seed files, seed searches, or inventory) is present
	if !input.IncludeInventory && len(input.SeedFiles) == 0 && len(input.SeedSearches) == 0 {
		return nil, fmt.Errorf("at least one seed file, seed search, or inventory request is required")
	}

	// Lookup project row ID to satisfy foreign key constraints
	project, err := s.store.GetProjectByProjectID(projectID)
	if err != nil {
		return nil, fmt.Errorf("lookup project by ID %s: %w", projectID, err)
	}
	projectRowID := project.ID
	projectRepos, err := s.store.ListProjectRepositories(projectRowID)
	if err != nil {
		return nil, fmt.Errorf("list project repositories for %s: %w", projectID, err)
	}
	normalizedSeedFiles, normalizedSeedSearches, err := normalizeContextPacketSeeds(input.SeedFiles, input.SeedSearches, projectRepos)
	if err != nil {
		return nil, err
	}

	taskSlug := normalizeTaskSlug(input.TaskSlug)
	maxSources := boundedPositive(input.MaxSources, defaultMaxSources, hardMaxSources)
	maxTotalBytes := boundedPositive(input.MaxTotalBytes, defaultMaxTotalBytes, hardMaxTotalBytes)
	generatedAt := nowSQLUTC()

	// Align context packet ID generation to format: ctxpkt-YYYY-MM-DD-<short-random>
	datePart := generatedAt
	if len(datePart) >= len("2006-01-02") {
		datePart = datePart[:len("2006-01-02")]
	} else {
		datePart = time.Now().UTC().Format("2006-01-02")
	}
	randomPart := uuid.NewString()
	if len(randomPart) > 8 {
		randomPart = randomPart[:8]
	}
	contextPacketID := fmt.Sprintf("ctxpkt-%s-%s", datePart, randomPart)

	builder := packetBuilder{
		projectID:        projectID,
		sourceSnapshotID: strings.TrimSpace(input.SourceSnapshotID),
		maxSources:       maxSources,
		maxTotalBytes:    maxTotalBytes,
	}
	var coverage []ContextCoverageEntry
	var blockers []sources.SourceBlocker

	processInventory := func() error {
		entry := ContextCoverageEntry{SeedID: "inventory", SeedType: "inventory", Status: CoverageStatusCovered}
		inventory, err := s.sources.ListProjectFiles(ctx, sources.FileInventoryInput{
			ProjectID:        projectID,
			SourceSnapshotID: input.SourceSnapshotID,
			MaxResults:       maxSources,
		})
		if err != nil {
			return err
		}
		if inventory.SourceSnapshotID != "" {
			builder.sourceSnapshotID = inventory.SourceSnapshotID
		}
		if len(inventory.Blockers) > 0 {
			entry.Status = CoverageStatusPartial
			entry.Blockers = inventory.Blockers
			blockers = append(blockers, inventory.Blockers...)
		}
		entry.Truncated = inventory.Truncated
		for _, file := range inventory.Files {
			source := ContextSource{
				SourceID:         sourceID(SourceTypeInventory, file.RepoID, file.Path, 0, 0, file.ContentHash),
				SourceType:       SourceTypeInventory,
				ProjectID:        file.ProjectID,
				RepoID:           file.RepoID,
				SourceSnapshotID: file.SourceSnapshotID,
				Path:             file.Path,
				ContentHash:      file.ContentHash,
				RedactionStatus:  file.RedactionStatus,
				GeneratedAt:      generatedAt,
			}
			if !builder.addSource(source) {
				entry.Truncated = true
				break
			}
			entry.SourceIDs = append(entry.SourceIDs, source.SourceID)
		}
		if len(entry.SourceIDs) == 0 && len(entry.Blockers) == 0 {
			entry.Status = CoverageStatusMissing
			entry.MissingCause = "inventory returned no files"
		}
		coverage = append(coverage, entry)
		return nil
	}

	processSeedFile := func(i int, seed ContextSeedFile) error {
		seedID := fmt.Sprintf("file:%d", i+1)
		entry := ContextCoverageEntry{
			SeedID:   seedID,
			SeedType: "file",
			Required: seed.Required,
			RepoID:   strings.TrimSpace(seed.RepoID),
			Path:     strings.TrimSpace(seed.Path),
			Reason:   strings.TrimSpace(seed.Reason),
		}
		read, err := s.sources.ReadProjectFile(ctx, sources.BoundedFileReadInput{
			ProjectID:        projectID,
			SourceSnapshotID: input.SourceSnapshotID,
			RepoID:           seed.RepoID,
			Path:             seed.Path,
			LineStart:        seed.LineStart,
			LineEnd:          seed.LineEnd,
			MaxBytes:         seed.MaxBytes,
		})
		if err != nil {
			return err
		}
		if read.SourceSnapshotID != "" {
			builder.sourceSnapshotID = read.SourceSnapshotID
		}
		if len(read.Blockers) > 0 {
			entry.Blockers = read.Blockers
			blockers = append(blockers, read.Blockers...)
			if seed.Required {
				entry.Status = CoverageStatusBlocked
			} else {
				entry.Status = CoverageStatusPartial
			}
			coverage = append(coverage, entry)
			return nil
		}
		if read.Content == "" {
			entry.MissingCause = "file read returned no content"
			if seed.Required {
				entry.Status = CoverageStatusMissing
			} else {
				entry.Status = CoverageStatusPartial
			}
			coverage = append(coverage, entry)
			return nil
		}
		source := ContextSource{
			SourceID:         sourceID(SourceTypeFileRead, read.RepoID, read.Path, read.LineStart, read.LineEnd, read.SnippetHash),
			SourceType:       SourceTypeFileRead,
			ProjectID:        read.ProjectID,
			RepoID:           read.RepoID,
			SourceSnapshotID: read.SourceSnapshotID,
			Path:             read.Path,
			LineStart:        read.LineStart,
			LineEnd:          read.LineEnd,
			Content:          read.Content,
			ContentHash:      read.ContentHash,
			SnippetHash:      read.SnippetHash,
			RedactionStatus:  read.RedactionStatus,
			Truncated:        read.Truncated,
			GeneratedAt:      read.GeneratedAt,
			Reason:           seed.Reason,
		}
		if !builder.addSource(source) {
			entry.Status = CoverageStatusPartial
			entry.Truncated = true
			coverage = append(coverage, entry)
			return nil
		}
		entry.Status = CoverageStatusCovered
		entry.SourceIDs = []string{source.SourceID}
		entry.Truncated = read.Truncated
		if read.Truncated {
			entry.Status = CoverageStatusPartial
		}
		coverage = append(coverage, entry)
		return nil
	}

	processSeedSearch := func(i int, seed ContextSeedSearch) error {
		seedID := fmt.Sprintf("search:%d", i+1)
		entry := ContextCoverageEntry{
			SeedID:   seedID,
			SeedType: "search",
			Required: seed.Required,
			Pattern:  seed.Pattern,
			Reason:   strings.TrimSpace(seed.Reason),
		}
		search, err := s.sources.SearchProjectFiles(ctx, sources.SourceSearchInput{
			ProjectID:        projectID,
			SourceSnapshotID: input.SourceSnapshotID,
			RepoIDs:          seed.RepoIDs,
			Pattern:          seed.Pattern,
			Literal:          seed.Literal,
			CaseSensitive:    seed.CaseSensitive,
			ContextLines:     seed.ContextLines,
			MaxResults:       seed.MaxResults,
		})
		if err != nil {
			return err
		}
		if search.SourceSnapshotID != "" {
			builder.sourceSnapshotID = search.SourceSnapshotID
		}
		if len(search.Blockers) > 0 {
			entry.Blockers = search.Blockers
			blockers = append(blockers, search.Blockers...)
		}
		for _, match := range search.Matches {
			source := ContextSource{
				SourceID:         sourceID(SourceTypeSearchMatch, match.RepoID, match.Path, match.LineStart, match.LineEnd, match.SnippetHash),
				SourceType:       SourceTypeSearchMatch,
				ProjectID:        match.ProjectID,
				RepoID:           match.RepoID,
				SourceSnapshotID: match.SourceSnapshotID,
				Path:             match.Path,
				LineStart:        match.LineStart,
				LineEnd:          match.LineEnd,
				Snippet:          match.Snippet,
				ContentHash:      match.ContentHash,
				SnippetHash:      match.SnippetHash,
				RedactionStatus:  match.RedactionStatus,
				Truncated:        match.Truncated,
				GeneratedAt:      match.GeneratedAt,
				Reason:           seed.Reason,
			}
			if !builder.addSource(source) {
				entry.Truncated = true
				break
			}
			entry.SourceIDs = append(entry.SourceIDs, source.SourceID)
		}
		entry.Truncated = entry.Truncated || search.Truncated
		switch {
		case len(entry.SourceIDs) > 0 && (entry.Truncated || len(entry.Blockers) > 0):
			entry.Status = CoverageStatusPartial
		case len(entry.SourceIDs) > 0:
			entry.Status = CoverageStatusCovered
		case len(entry.Blockers) > 0 && seed.Required:
			entry.Status = CoverageStatusPartial
		case len(entry.Blockers) > 0:
			entry.Status = CoverageStatusPartial
		case seed.Required && !entry.Truncated:
			// A required search that completes with zero matches, no blockers,
			// and no truncation is completed evidence that the search was
			// performed. Treat it as covered so it does not make an otherwise
			// usable context packet partial or fail handoff readiness.
			entry.Status = CoverageStatusCovered
			entry.MissingCause = "search completed with no matches"
		case seed.Required:
			// Truncated required search with no captured matches remains partial.
			entry.Status = CoverageStatusPartial
			entry.MissingCause = "search truncated before matches were captured"
		default:
			entry.Status = CoverageStatusPartial
			entry.MissingCause = "optional search returned no matches"
		}
		coverage = append(coverage, entry)
		return nil
	}

	for i, seed := range normalizedSeedFiles {
		if seed.Required {
			if err := processSeedFile(i, seed); err != nil {
				return nil, err
			}
		}
	}
	for i, seed := range normalizedSeedSearches {
		if seed.Required {
			if err := processSeedSearch(i, seed); err != nil {
				return nil, err
			}
		}
	}
	for i, seed := range normalizedSeedFiles {
		if !seed.Required {
			if err := processSeedFile(i, seed); err != nil {
				return nil, err
			}
		}
	}
	for i, seed := range normalizedSeedSearches {
		if !seed.Required {
			if err := processSeedSearch(i, seed); err != nil {
				return nil, err
			}
		}
	}
	if input.IncludeInventory {
		if err := processInventory(); err != nil {
			return nil, err
		}
	}

	builder.sources, blockers, coverage = runFinalRedactionScan(builder.sources, blockers, coverage)

	// Recalculate total bytes
	totalBytes := 0
	for _, src := range builder.sources {
		totalBytes += len([]byte(src.Content)) + len([]byte(src.Snippet))
	}
	builder.totalBytes = totalBytes

	optionalInventoryTruncated := hasOptionalInventoryTruncatedCoverage(coverage)
	truncated := hasBlockingTruncatedCoverage(coverage)
	covered, blocked, missing := coverageCounts(coverage)
	status := statusFromCoverage(coverage, truncated)
	summary := ContextPacketSummary{
		SourceCount:                len(builder.sources),
		CoveredSeedCount:           covered,
		BlockedSeedCount:           blocked,
		MissingSeedCount:           missing,
		Truncated:                  truncated,
		MaxSources:                 maxSources,
		MaxTotalBytes:              maxTotalBytes,
		TotalSourceBytes:           builder.totalBytes,
		InventoryIncluded:          input.IncludeInventory,
		OptionalInventoryTruncated: optionalInventoryTruncated,
	}

	// Lookup source snapshot row ID
	var snapshotRowID int64
	if builder.sourceSnapshotID != "" {
		snapshot, err := s.store.GetSourceSnapshotByID(builder.sourceSnapshotID)
		if err != nil {
			return nil, fmt.Errorf("lookup source snapshot by ID %s: %w", builder.sourceSnapshotID, err)
		}
		snapshotRowID = snapshot.ID
	}

	packet := ContextPacket{
		SchemaVersion:    ContextPacketSchemaVersion,
		ContextPacketID:  contextPacketID,
		ProjectID:        projectID,
		PlanID:           strings.TrimSpace(input.PlanID),
		PassID:           strings.TrimSpace(input.PassID),
		TaskSlug:         taskSlug,
		SourceSnapshotID: builder.sourceSnapshotID,
		Status:           status,
		GeneratedAt:      generatedAt,
		Summary:          summary,
		Sources:          builder.sources,
		Coverage:         coverage,
		Blockers:         blockers,
	}
	report := ContextCoverageReport{
		SchemaVersion:    ContextPacketSchemaVersion,
		ContextPacketID:  contextPacketID,
		ProjectID:        projectID,
		PlanID:           packet.PlanID,
		PassID:           packet.PassID,
		TaskSlug:         taskSlug,
		SourceSnapshotID: builder.sourceSnapshotID,
		Status:           status,
		GeneratedAt:      generatedAt,
		Summary:          summary,
		Entries:          coverage,
	}

	packetJSONPath, packetMarkdownPath, coverageReportPath, err := writeArtifacts(generatedAt, taskSlug, packet, report)
	if err != nil {
		return nil, err
	}
	blockersJSON, err := json.Marshal(blockers)
	if err != nil {
		return nil, err
	}
	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}

	row, err := s.store.CreateContextPacket(store.CreateContextPacketParams{
		ContextPacketID:     contextPacketID,
		ProjectRowID:        projectRowID,
		ProjectID:           projectID,
		PlanID:              packet.PlanID,
		PassID:              packet.PassID,
		TaskSlug:            taskSlug,
		SourceSnapshotRowID: snapshotRowID,
		SourceSnapshotID:    builder.sourceSnapshotID,
		Status:              status,
		PacketJSONPath:      packetJSONPath,
		PacketMarkdownPath:  packetMarkdownPath,
		CoverageReportPath:  coverageReportPath,
		SourceCount:         int64(summary.SourceCount),
		CoveredSeedCount:    int64(summary.CoveredSeedCount),
		BlockedSeedCount:    int64(summary.BlockedSeedCount),
		MissingSeedCount:    int64(summary.MissingSeedCount),
		Truncated:           boolToInt64(summary.Truncated),
		BlockersJSON:        string(blockersJSON),
		SummaryJSON:         string(summaryJSON),
		CompletedAt:         generatedAt,
	})
	if err != nil {
		return nil, err
	}
	for _, source := range builder.sources {
		if _, err := s.store.CreateContextPacketSource(store.CreateContextPacketSourceParams{
			ContextPacketRowID: row.ID,
			SourceID:           source.SourceID,
			SourceType:         source.SourceType,
			ProjectID:          source.ProjectID,
			RepoID:             source.RepoID,
			SourceSnapshotID:   source.SourceSnapshotID,
			Path:               source.Path,
			LineStart:          int64(source.LineStart),
			LineEnd:            int64(source.LineEnd),
			ContentHash:        source.ContentHash,
			SnippetHash:        source.SnippetHash,
			RedactionStatus:    source.RedactionStatus,
			Truncated:          boolToInt64(source.Truncated),
			GeneratedAt:        source.GeneratedAt,
			Reason:             source.Reason,
		}); err != nil {
			return nil, err
		}
	}

	return &ContextPacketResult{
		ContextPacketID:    contextPacketID,
		ProjectID:          projectID,
		PlanID:             packet.PlanID,
		PassID:             packet.PassID,
		TaskSlug:           taskSlug,
		SourceSnapshotID:   builder.sourceSnapshotID,
		Status:             status,
		PacketJSONPath:     packetJSONPath,
		PacketMarkdownPath: packetMarkdownPath,
		CoverageReportPath: coverageReportPath,
		SourceCount:        summary.SourceCount,
		CoveredSeedCount:   summary.CoveredSeedCount,
		BlockedSeedCount:   summary.BlockedSeedCount,
		MissingSeedCount:   summary.MissingSeedCount,
		Truncated:          summary.Truncated,
		Blockers:           blockers,
		Summary:            summary,
		Coverage:           coverage,
		LimitHit:           detectLimitHit(summary, coverage),
	}, nil
}

type packetBuilder struct {
	projectID        string
	sourceSnapshotID string
	maxSources       int
	maxTotalBytes    int
	totalBytes       int
	truncated        bool
	seen             map[string]struct{}
	sources          []ContextSource
}

func (b *packetBuilder) addSource(source ContextSource) bool {
	if b.seen == nil {
		b.seen = make(map[string]struct{})
	}
	if _, ok := b.seen[source.SourceID]; ok {
		return true
	}
	if len(b.sources) >= b.maxSources {
		b.truncated = true
		return false
	}
	bytes := len([]byte(source.Content)) + len([]byte(source.Snippet))
	if b.totalBytes+bytes > b.maxTotalBytes {
		b.truncated = true
		return false
	}
	b.seen[source.SourceID] = struct{}{}
	b.sources = append(b.sources, source)
	b.totalBytes += bytes
	if b.sourceSnapshotID == "" {
		b.sourceSnapshotID = source.SourceSnapshotID
	}
	return true
}

func normalizeTaskSlug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugTokenPattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "context-packet"
	}
	if len(value) > 120 {
		value = strings.Trim(value[:120], "-")
	}
	if value == "" {
		return "context-packet"
	}
	return value
}

func sourceID(sourceType, repoID, path string, lineStart, lineEnd int, hash string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%d|%s", sourceType, repoID, path, lineStart, lineEnd, hash)))
	return "src_" + hex.EncodeToString(sum[:])[:16]
}

func hasAnyTruncatedCoverage(entries []ContextCoverageEntry) bool {
	for _, entry := range entries {
		if entry.Truncated {
			return true
		}
	}
	return false
}

func hasBlockingTruncatedCoverage(entries []ContextCoverageEntry) bool {
	hasRequired := false
	for _, entry := range entries {
		if entry.Required {
			hasRequired = true
			if entry.Truncated {
				return true
			}
			continue
		}
		if entry.Truncated && entry.SeedType != "inventory" {
			return true
		}
	}
	if hasRequired {
		return false
	}
	return hasAnyTruncatedCoverage(entries)
}

func hasOptionalInventoryTruncatedCoverage(entries []ContextCoverageEntry) bool {
	for _, entry := range entries {
		if entry.SeedType == "inventory" && !entry.Required && entry.Truncated {
			return true
		}
	}
	return false
}

func detectLimitHit(summary ContextPacketSummary, coverage []ContextCoverageEntry) string {
	if !summary.Truncated {
		return LimitHitNone
	}
	if summary.MaxSources > 0 && summary.SourceCount >= summary.MaxSources {
		return LimitHitMaxSources
	}
	if summary.MaxTotalBytes > 0 && summary.TotalSourceBytes >= summary.MaxTotalBytes {
		return LimitHitMaxTotalBytes
	}
	for _, entry := range coverage {
		if entry.Truncated {
			return LimitHitCoverageTruncated
		}
	}
	return LimitHitUnknown
}

func boundedPositive(value, defaultValue, hardCap int) int {
	if value <= 0 {
		value = defaultValue
	}
	if value > hardCap {
		value = hardCap
	}
	return value
}

func normalizeContextPacketSeeds(files []ContextSeedFile, searches []ContextSeedSearch, repos []store.ProjectRepository) ([]ContextSeedFile, []ContextSeedSearch, error) {
	repoMap := make(map[string]store.ProjectRepository, len(repos))
	for _, repo := range repos {
		repoMap[repo.RepoID] = repo
	}

	normalizedFiles := make([]ContextSeedFile, 0, len(files))
	for _, seed := range files {
		seed.RepoID = strings.TrimSpace(seed.RepoID)
		repoID, err := normalizeContextPacketRepoID(seed.RepoID, repoMap)
		if err != nil {
			return nil, nil, err
		}
		seed.RepoID = repoID
		normalizedFiles = append(normalizedFiles, seed)
	}

	normalizedSearches := make([]ContextSeedSearch, 0, len(searches))
	for _, seed := range searches {
		repoIDs, err := normalizeContextPacketRepoIDs(seed.RepoIDs, repoMap)
		if err != nil {
			return nil, nil, err
		}
		seed.RepoIDs = repoIDs
		normalizedSearches = append(normalizedSearches, seed)
	}

	return normalizedFiles, normalizedSearches, nil
}

func normalizeContextPacketRepoIDs(repoIDs []string, repos map[string]store.ProjectRepository) ([]string, error) {
	if len(repoIDs) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(repoIDs))
	seen := map[string]struct{}{}
	for _, raw := range repoIDs {
		repoID, err := normalizeContextPacketRepoID(raw, repos)
		if err != nil {
			return nil, err
		}
		if repoID == "" {
			continue
		}
		if _, ok := seen[repoID]; ok {
			continue
		}
		out = append(out, repoID)
		seen[repoID] = struct{}{}
	}
	return out, nil
}

func normalizeContextPacketRepoID(raw string, repos map[string]store.ProjectRepository) (string, error) {
	repoID := strings.TrimSpace(raw)
	if repoID == "" {
		return "", nil
	}
	if _, ok := repos[repoID]; ok {
		return repoID, nil
	}
	var match string
	for registered := range repos {
		if repoIDAliasMatches(repoID, registered) {
			if match != "" && match != registered {
				return "", fmt.Errorf("repo_id %q is ambiguous for project repositories", repoID)
			}
			match = registered
		}
	}
	if match != "" {
		return match, nil
	}
	return repoID, nil
}

func repoIDAliasMatches(alias, registered string) bool {
	registered = strings.TrimSpace(registered)
	if alias == registered {
		return true
	}
	if idx := strings.LastIndex(registered, "/"); idx >= 0 && idx+1 < len(registered) {
		return alias == registered[idx+1:]
	}
	return false
}

func boolToInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func nowSQLUTC() string {
	return time.Now().UTC().Format("2006-01-02 15:04:05")
}

func sha256HexString(val string) string {
	sum := sha256.Sum256([]byte(val))
	return hex.EncodeToString(sum[:])
}

func runFinalRedactionScan(srcs []ContextSource, blockers []sources.SourceBlocker, coverage []ContextCoverageEntry) ([]ContextSource, []sources.SourceBlocker, []ContextCoverageEntry) {
	var finalSources []ContextSource
	blockedSourceIDs := make(map[string]sources.SourceBlocker)

	for _, source := range srcs {
		isBlocked := false
		var newContent, newSnippet string
		var contentStatus, snippetStatus string

		if source.Content != "" {
			var st string
			newContent, st = sources.RedactSourceContent(source.Content)
			if st == sources.RedactionStatusBlocked {
				isBlocked = true
			} else if st == sources.RedactionStatusRedacted {
				contentStatus = st
			}
		}
		if source.Snippet != "" && !isBlocked {
			var st string
			newSnippet, st = sources.RedactSourceContent(source.Snippet)
			if st == sources.RedactionStatusBlocked {
				isBlocked = true
			} else if st == sources.RedactionStatusRedacted {
				snippetStatus = st
			}
		}

		if isBlocked {
			blocker := sources.SourceBlocker{
				RepoID:  source.RepoID,
				Code:    sources.SourceBlockerRedactionBlocked,
				Message: fmt.Sprintf("source content for %s contains blocked secret material", source.Path),
			}
			blockedSourceIDs[source.SourceID] = blocker
			blockers = append(blockers, blocker)
		} else {
			if contentStatus == sources.RedactionStatusRedacted {
				source.Content = newContent
				source.RedactionStatus = sources.RedactionStatusRedacted
				if source.SourceType == SourceTypeFileRead {
					source.SnippetHash = sha256HexString(newContent)
				}
			}
			if snippetStatus == sources.RedactionStatusRedacted {
				source.Snippet = newSnippet
				source.RedactionStatus = sources.RedactionStatusRedacted
				if source.SourceType == SourceTypeSearchMatch {
					source.SnippetHash = sha256HexString(newSnippet)
				}
			}
			finalSources = append(finalSources, source)
		}
	}

	// Update coverage entries to reflect blocked/removed sources
	for idx, entry := range coverage {
		var newSourceIDs []string
		entryBlocked := false
		var entryBlockers []sources.SourceBlocker

		for _, srcID := range entry.SourceIDs {
			if blocker, ok := blockedSourceIDs[srcID]; ok {
				entryBlocked = true
				entryBlockers = append(entryBlockers, blocker)
			} else {
				newSourceIDs = append(newSourceIDs, srcID)
			}
		}

		if entryBlocked {
			coverage[idx].SourceIDs = newSourceIDs
			coverage[idx].Blockers = append(coverage[idx].Blockers, entryBlockers...)
			if entry.Required {
				coverage[idx].Status = CoverageStatusBlocked
			} else {
				coverage[idx].Status = CoverageStatusPartial
			}
		}
	}
	return finalSources, blockers, coverage
}
