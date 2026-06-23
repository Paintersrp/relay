package projectmemory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"relay/internal/sources"
	"relay/internal/store"
)

var tagPattern = regexp.MustCompile(`[^a-z0-9._-]+`)

type Service struct {
	store *store.Store
}

func NewService(st *store.Store) *Service {
	return &Service{store: st}
}

func (s *Service) SearchProjectContextMemory(ctx context.Context, input SearchInput) (SearchResult, error) {
	project, err := s.loadProject(ctx, input.ProjectID)
	if err != nil {
		return SearchResult{}, err
	}
	statuses := input.Statuses
	if len(statuses) == 0 {
		statuses = []string{StatusActive}
	}
	limit, queryLimit := boundedLimit(input.Limit)
	rows, err := s.store.SearchProjectContextRecords(ctx, store.SearchProjectContextRecordsParams{
		ProjectRowID:   project.ID,
		Query:          strings.TrimSpace(input.Query),
		KindsJson:      filterJSON(input.Kinds),
		StatusesJson:   filterJSON(statuses),
		ImportanceJson: filterJSON(input.Importance),
		TagsJsonFilter: filterJSON(normalizeTags(input.Tags)),
		Limit:          int64(queryLimit),
	})
	if err != nil {
		return SearchResult{}, err
	}
	records, truncated := recordsFromRows(rows, limit, input.IncludeBody)
	return SearchResult{ProjectID: project.ProjectID, Records: records, Limit: limit, Truncated: truncated, IncludeBody: input.IncludeBody}, nil
}

func (s *Service) ListProjectContextRecords(ctx context.Context, input ListInput) (ListResult, error) {
	project, err := s.loadProject(ctx, input.ProjectID)
	if err != nil {
		return ListResult{}, err
	}
	statuses := input.Statuses
	if len(statuses) == 0 {
		statuses = []string{StatusActive}
	}
	limit, queryLimit := boundedLimit(input.Limit)
	rows, err := s.store.ListProjectContextRecords(ctx, store.ListProjectContextRecordsParams{
		ProjectRowID:   project.ID,
		KindsJson:      filterJSON(input.Kinds),
		StatusesJson:   filterJSON(statuses),
		ImportanceJson: filterJSON(input.Importance),
		TagsJsonFilter: filterJSON(normalizeTags(input.Tags)),
		Limit:          int64(queryLimit),
	})
	if err != nil {
		return ListResult{}, err
	}
	records, truncated := recordsFromRows(rows, limit, false)
	return ListResult{ProjectID: project.ProjectID, Records: records, Limit: limit, Truncated: truncated}, nil
}

func (s *Service) GetProjectContextRecord(ctx context.Context, input GetInput) (*Record, error) {
	project, err := s.loadProject(ctx, input.ProjectID)
	if err != nil {
		return nil, err
	}
	recordID := strings.TrimSpace(input.RecordID)
	if recordID == "" {
		return nil, brokerLikeError("VALIDATION_ERROR", "record_id is required")
	}
	row, err := s.store.GetProjectContextRecordByRecordID(ctx, store.GetProjectContextRecordByRecordIDParams{
		ProjectRowID:    project.ID,
		ContextRecordID: recordID,
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, brokerLikeError("NOT_FOUND", fmt.Sprintf("project context record %q not found", recordID))
	}
	if err != nil {
		return nil, err
	}
	record := recordFromRow(*row, true)
	return &record, nil
}

func (s *Service) CreateProjectContextRecord(ctx context.Context, input CreateInput) (*Record, []ValidationIssue, error) {
	project, issues, normalized, err := s.normalizeCreate(ctx, input)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}
	if duplicate, err := s.store.GetActiveProjectContextRecordByBodyHash(ctx, store.GetActiveProjectContextRecordByBodyHashParams{
		ProjectRowID: project.ID,
		BodyHash:     normalized.bodyHash,
	}); err == nil && duplicate != nil {
		return nil, []ValidationIssue{issue("body", "duplicate_context", "an active project context record already has this sanitized body")}, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	row, err := s.store.CreateProjectContextRecord(ctx, normalized.toStoreParams(project))
	if err != nil {
		return nil, nil, err
	}
	record := recordFromRow(*row, true)
	return &record, nil, nil
}

func (s *Service) SupersedeProjectContextRecord(ctx context.Context, input SupersedeInput) (*SupersedeResult, []ValidationIssue, error) {
	project, err := s.loadProject(ctx, input.ProjectID)
	if err != nil {
		return nil, nil, err
	}
	old, err := s.store.GetProjectContextRecordByRecordID(ctx, store.GetProjectContextRecordByRecordIDParams{
		ProjectRowID:    project.ID,
		ContextRecordID: strings.TrimSpace(input.RecordID),
	})
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, brokerLikeError("NOT_FOUND", fmt.Sprintf("project context record %q not found", strings.TrimSpace(input.RecordID)))
	}
	if err != nil {
		return nil, nil, err
	}
	if old.Status != StatusActive {
		return nil, []ValidationIssue{issue("record_id", "not_active", "only active project context records can be superseded")}, nil
	}
	createInput := CreateInput{
		ProjectID:     input.ProjectID,
		Kind:          input.Kind,
		Title:         input.Title,
		Body:          input.Body,
		Importance:    input.Importance,
		Tags:          input.Tags,
		Source:        input.Source,
		CreatedBy:     input.CreatedBy,
		DedupeReason:  input.DedupeReason,
		SupersedesID:  old.ContextRecordID,
	}
	_, issues, normalized, err := s.normalizeCreate(ctx, createInput)
	if err != nil || len(issues) > 0 {
		return nil, issues, err
	}
	if duplicate, err := s.store.GetActiveProjectContextRecordByBodyHash(ctx, store.GetActiveProjectContextRecordByBodyHashParams{
		ProjectRowID: project.ID,
		BodyHash:     normalized.bodyHash,
	}); err == nil && duplicate != nil {
		return nil, []ValidationIssue{issue("body", "duplicate_context", "an active project context record already has this sanitized body")}, nil
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, nil, err
	}
	txResult, err := s.store.SupersedeProjectContextRecord(ctx, store.SupersedeProjectContextRecordParams{
		Create: normalized.toStoreParams(project),
		MarkOld: store.MarkProjectContextRecordSupersededParams{
			ProjectRowID:    project.ID,
			ContextRecordID: old.ContextRecordID,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	return &SupersedeResult{
		OldRecord: recordFromRow(txResult.Old, true),
		NewRecord: recordFromRow(txResult.New, true),
	}, nil, nil
}

type normalizedCreate struct {
	contextRecordID string
	kind            string
	title           string
	body            string
	bodyHash        string
	importance      string
	tagsJSON        string
	source          string
	createdBy       string
	dedupeReason    string
	redactionStatus string
	supersedesID    string
}

func (s *Service) normalizeCreate(ctx context.Context, input CreateInput) (*store.Project, []ValidationIssue, normalizedCreate, error) {
	project, err := s.loadProject(ctx, input.ProjectID)
	if err != nil {
		return nil, nil, normalizedCreate{}, err
	}
	issues := []ValidationIssue{}
	kind := strings.TrimSpace(input.Kind)
	if !allowed(kind, allowedKinds()) {
		issues = append(issues, issue("kind", "invalid", "kind must be an allowed project context memory kind"))
	}
	importance := strings.TrimSpace(input.Importance)
	if importance == "" {
		importance = ImportanceNormal
	}
	if !allowed(importance, allowedImportance()) {
		issues = append(issues, issue("importance", "invalid", "importance must be low, normal, high, or critical"))
	}
	source := strings.TrimSpace(input.Source)
	if source == "" {
		source = SourceChat
	}
	if !allowed(source, allowedSources()) {
		issues = append(issues, issue("source", "invalid", "source must be an allowed project context memory source"))
	}
	createdBy := strings.TrimSpace(input.CreatedBy)
	if createdBy == "" {
		createdBy = CreatedByChatAgent
	}
	if !allowed(createdBy, allowedCreatedBy()) {
		issues = append(issues, issue("created_by", "invalid", "created_by must be chat_agent, operator, or system"))
	}
	dedupeReason := strings.TrimSpace(input.DedupeReason)
	if dedupeReason == "" {
		issues = append(issues, issue("dedupe_reason", "required", "dedupe_reason is required"))
	}
	if utf8.RuneCountInString(dedupeReason) > MaxDedupeRunes {
		issues = append(issues, issue("dedupe_reason", "too_long", "dedupe_reason must be at most 500 characters"))
	}
	tags, tagIssues := normalizeTagsWithIssues(input.Tags)
	issues = append(issues, tagIssues...)

	title, titleStatus := sources.RedactSourceContent(strings.TrimSpace(input.Title))
	body, bodyStatus := sources.RedactSourceContent(strings.TrimSpace(input.Body))
	if titleStatus == sources.RedactionStatusBlocked {
		issues = append(issues, issue("title", "redaction_blocked", "title contains blocked secret-like content"))
	}
	if bodyStatus == sources.RedactionStatusBlocked {
		issues = append(issues, issue("body", "redaction_blocked", "body contains blocked secret-like content"))
	}
	if strings.TrimSpace(title) == "" {
		issues = append(issues, issue("title", "required", "title is required"))
	}
	if strings.TrimSpace(body) == "" {
		issues = append(issues, issue("body", "required", "body is required"))
	}
	if utf8.RuneCountInString(title) > MaxTitleRunes {
		issues = append(issues, issue("title", "too_long", "title must be at most 180 characters"))
	}
	if utf8.RuneCountInString(body) > MaxBodyRunes {
		issues = append(issues, issue("body", "too_long", "body must be at most 32768 characters after redaction"))
	}
	if len(issues) > 0 {
		return project, issues, normalizedCreate{}, nil
	}
	tagsJSONBytes, err := json.Marshal(tags)
	if err != nil {
		return nil, nil, normalizedCreate{}, err
	}
	redactionStatus := sources.RedactionStatusNotNeeded
	if titleStatus == sources.RedactionStatusRedacted || bodyStatus == sources.RedactionStatusRedacted {
		redactionStatus = sources.RedactionStatusRedacted
	}
	bodyHash := sha256.Sum256([]byte(body))
	recordID, err := randomRecordID()
	if err != nil {
		return nil, nil, normalizedCreate{}, err
	}
	return project, nil, normalizedCreate{
		contextRecordID: recordID,
		kind:            kind,
		title:           title,
		body:            body,
		bodyHash:        hex.EncodeToString(bodyHash[:]),
		importance:      importance,
		tagsJSON:        string(tagsJSONBytes),
		source:          source,
		createdBy:       createdBy,
		dedupeReason:    dedupeReason,
		redactionStatus: redactionStatus,
		supersedesID:    strings.TrimSpace(input.SupersedesID),
	}, nil
}

func (n normalizedCreate) toStoreParams(project *store.Project) store.CreateProjectContextRecordParams {
	return store.CreateProjectContextRecordParams{
		ContextRecordID:    n.contextRecordID,
		ProjectRowID:       project.ID,
		ProjectID:          project.ProjectID,
		Kind:               n.kind,
		Title:              n.title,
		Body:               n.body,
		BodyHash:           n.bodyHash,
		Status:             StatusActive,
		Importance:         n.importance,
		TagsJson:           n.tagsJSON,
		Source:             n.source,
		CreatedBy:          n.createdBy,
		DedupeReason:       n.dedupeReason,
		RedactionStatus:    n.redactionStatus,
		SupersedesRecordID: n.supersedesID,
	}
}

func (s *Service) loadProject(ctx context.Context, projectID string) (*store.Project, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, brokerLikeError("VALIDATION_ERROR", "project_id is required")
	}
	project, err := s.store.GetProjectByProjectID(projectID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, brokerLikeError("NOT_FOUND", fmt.Sprintf("project %q not found", projectID))
	}
	if err != nil {
		return nil, err
	}
	_ = ctx
	return project, nil
}

func recordsFromRows(rows []store.ProjectContextRecord, limit int, includeBody bool) ([]Record, bool) {
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	records := make([]Record, 0, len(rows))
	for _, row := range rows {
		records = append(records, recordFromRow(row, includeBody))
	}
	return records, truncated
}

func recordFromRow(row store.ProjectContextRecord, includeBody bool) Record {
	record := Record{
		ContextRecordID:      row.ContextRecordID,
		ProjectID:            row.ProjectID,
		Kind:                 row.Kind,
		Title:                row.Title,
		BodyHash:             row.BodyHash,
		Status:               row.Status,
		Importance:           row.Importance,
		Tags:                 decodeTags(row.TagsJson),
		Source:               row.Source,
		CreatedBy:            row.CreatedBy,
		DedupeReason:         row.DedupeReason,
		RedactionStatus:      row.RedactionStatus,
		SupersedesRecordID:   row.SupersedesRecordID,
		SupersededByRecordID: row.SupersededByRecordID,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
	if includeBody {
		record.Body = row.Body
	} else {
		record.BodyExcerpt = excerpt(row.Body)
	}
	return record
}

func boundedLimit(limit int) (int, int) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}
	return limit, limit + 1
}

func filterJSON(values []string) string {
	if len(values) == 0 {
		return ""
	}
	b, _ := json.Marshal(values)
	return string(b)
}

func normalizeTags(values []string) []string {
	tags, _ := normalizeTagsWithIssues(values)
	return tags
}

func normalizeTagsWithIssues(values []string) ([]string, []ValidationIssue) {
	issues := []ValidationIssue{}
	seen := map[string]struct{}{}
	tags := []string{}
	for _, raw := range values {
		tag := strings.ToLower(strings.TrimSpace(raw))
		tag = tagPattern.ReplaceAllString(tag, "-")
		tag = strings.Trim(tag, "-")
		if tag == "" {
			continue
		}
		if utf8.RuneCountInString(tag) > MaxTagRunes {
			issues = append(issues, issue("tags", "too_long", "tags must be at most 40 characters after normalization"))
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	if len(tags) > MaxTags {
		issues = append(issues, issue("tags", "too_many", "at most 20 tags are allowed"))
		tags = tags[:MaxTags]
	}
	return tags, issues
}

func decodeTags(raw string) []string {
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return []string{}
	}
	return tags
}

func excerpt(body string) string {
	runes := []rune(strings.TrimSpace(body))
	if len(runes) <= MaxExcerptRunes {
		return string(runes)
	}
	return string(runes[:MaxExcerptRunes])
}

func randomRecordID() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "ctxmem_" + hex.EncodeToString(buf[:]), nil
}

func allowed(value string, allowed map[string]struct{}) bool {
	_, ok := allowed[value]
	return ok
}

func allowedKinds() map[string]struct{} {
	return set(KindDecision, KindConstraint, KindArchitectureRationale, KindOperatorPreference, KindProjectPrinciple, KindRisk, KindTerminology, KindSupersession, KindOpenQuestion)
}

func allowedImportance() map[string]struct{} {
	return set(ImportanceLow, ImportanceNormal, ImportanceHigh, ImportanceCritical)
}

func allowedSources() map[string]struct{} {
	return set(SourceChat, SourceOperatorStatement, SourceHandoff, SourceAudit, SourceSourceDoc, SourceManual)
}

func allowedCreatedBy() map[string]struct{} {
	return set(CreatedByChatAgent, CreatedByOperator, CreatedBySystem)
}

func set(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func issue(field, code, message string) ValidationIssue {
	return ValidationIssue{Field: field, Code: code, Message: message}
}

type opError struct {
	Code    string
	Message string
}

func (e opError) Error() string {
	return e.Code + ": " + e.Message
}

func brokerLikeError(code, message string) error {
	return opError{Code: code, Message: message}
}

func ErrorCode(err error) (string, string, bool) {
	var opErr opError
	if errors.As(err, &opErr) {
		return opErr.Code, opErr.Message, true
	}
	return "", "", false
}
