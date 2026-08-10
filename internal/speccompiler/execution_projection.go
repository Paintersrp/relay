package speccompiler

// ExecutionProjection is the deterministic downstream-consumer projection of an
// authored execution document. It preserves source order and carries every
// execution-relevant field without exposing compiler internals.
type ExecutionProjection struct {
	Replay             ReplaySemantics
	Substeps           []ProjectedSubstep
	PathChains         []ProjectedPathChain
	FileWork           []ProjectedFileWork
	ValidationCommands []ProjectedValidationCommand
}

type ProjectedSubstep struct {
	Ref                  string
	AuthoredDependencies []string
	ImplicitDependencies []string
	Dependencies         []string
	AtomicPresent        bool
	Atomic               bool
	FileWorkRefs         []string
}

type ProjectedPathChain struct {
	Ref              string
	FileWorkRefs     []string
	PathEndpoints    []string
	SubstepRefs      []string
	Replay           ReplaySemantics
	FirstSourceOrder int
}

type ProjectedFileWork struct {
	Ref                 string
	SubstepRef          string
	PathChainRef        string
	SourceOrder         int
	Path                string
	DestinationPath     string
	Operation           string
	Purpose             string
	SourcePreconditions []ProjectedSourcePrecondition
	Directives          []ProjectedDirective
	Content             string
	DeleteFile          bool
	PreserveContent     bool
}

type ProjectedSourcePrecondition struct {
	Kind string
	Path string
}

type ProjectedDirective struct {
	Ref                 string
	SourceOrder         int
	Kind                string
	OldText             string
	NewText             string
	Anchor              string
	Content             string
	ExpectedOccurrences int
	Grounding           *SelectorGrounding
}

type ProjectedValidationCommand struct {
	Command          string
	WorkingDirectory string
	Expected         string
	SuccessSignal    string
}

type SelectorGrounding struct {
	DirectiveRef         string
	Selector             string
	ExpectedOccurrences  int
	Replay               ReplaySemantics
	BaseRequired         bool
	ProducerDirectiveRef string
}
