package sourcegateway

import "relay/internal/operations/registry"

const (
	SearchOrderVersion    = "relay.source-search-order.v1"
	MaxSearchPageMatches  = MaxTreePageEntries
	MaxSearchLiteralBytes = MaxBlobPageBytes
)

type SearchMode string

const (
	SearchModeTextLiteral SearchMode = "text_literal"
	SearchModeByteLiteral SearchMode = "byte_literal"
)

type SearchCompletion string

const (
	SearchCompletionComplete         SearchCompletion = "complete"
	SearchCompletionPageIncomplete   SearchCompletion = "page_incomplete"
	SearchCompletionBudgetIncomplete SearchCompletion = "budget_incomplete"
)

type SearchBudget struct {
	ExaminedObjects int64
	ExaminedBytes   int64
}

type SearchRequest struct {
	PacketID        string
	SurfaceContract registry.SurfaceContractID
	OperationID     registry.OperationID
	RepositoryKey   string
	Revision        RevisionReference
	Mode            SearchMode
	TextLiteral     string
	ByteLiteral     []byte
	Prefixes        []PathReference
	Limit           int
	Budget          SearchBudget
	Cursor          string
}

type SearchMatch struct {
	MatchID           string
	Path              PathIdentity
	FileMode          string
	BlobOID           string
	ByteOffset        int64
	MatchLength       int64
	OccurrenceOrdinal int64
}

type SearchResult struct {
	Source                SourceIdentity
	Mode                  SearchMode
	QueryID               string
	FilterID              string
	Matches               []SearchMatch
	ExaminedObjects       int64
	ExaminedBytes         int64
	ObjectBudgetExhausted bool
	ByteBudgetExhausted   bool
	Completion            SearchCompletion
	Cursor                string
}

type canonicalSearchPrefix struct {
	bytes    []byte
	identity PathIdentity
}

type searchPhase string

const (
	searchPhaseTextValidation searchPhase = "text_validation"
	searchPhaseLiteralScan    searchPhase = "literal_scan"
)

func validSearchPhase(value searchPhase) bool {
	return value == searchPhaseTextValidation || value == searchPhaseLiteralScan
}

type searchResume struct {
	path           []byte
	pathID         string
	blobOID        string
	phase          searchPhase
	nextOffset     int64
	ordinal        int64
	totalSize      int64
	totalSizeKnown bool
}
