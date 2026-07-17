package fileacquisition

import "context"

const (
	MaxFiles          = 64
	MaxFileIndex      = 63
	MaxAggregateBytes = int64(64 * 1024 * 1024)
)

type FileParameter struct {
	DownloadURL string
	FileID      string
	MIMEType    string
	FileName    string
}

type DeclaredFile struct {
	FileIndex      int64
	ExpectedSHA256 string
	DisplayName    string
	MediaType      string
}

type FetchedFile struct {
	Bytes []byte
}

type AcquiredFile struct {
	FileIndex   int64
	Bytes       []byte
	DisplayName string
	MediaType   string
	SHA256      string
	SizeBytes   int64
}

type Request struct {
	Files    []FileParameter
	Declared []DeclaredFile
}

type Result struct {
	files      []AcquiredFile
	totalBytes int64
}

type FetchOne interface {
	FetchFile(context.Context, FileParameter) (FetchedFile, error)
}

type FetchFunc func(context.Context, FileParameter) (FetchedFile, error)

func (fn FetchFunc) FetchFile(ctx context.Context, file FileParameter) (FetchedFile, error) {
	return fn(ctx, file)
}

type ErrorCode string

const (
	ErrorInvalidRequest ErrorCode = "invalid_file_acquisition_request"
	ErrorFileCount      ErrorCode = "file_acquisition_count"
	ErrorFileIndex      ErrorCode = "file_acquisition_index"
	ErrorFileCoverage   ErrorCode = "file_acquisition_coverage"
	ErrorFileIdentity   ErrorCode = "file_acquisition_identity"
	ErrorFetchFailed    ErrorCode = "file_acquisition_fetch_failed"
	ErrorAggregateLimit ErrorCode = "file_acquisition_aggregate_limit"
	ErrorDigestMismatch ErrorCode = "file_acquisition_digest_mismatch"
)

type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string {
	if e == nil || e.Code == "" {
		return "file acquisition failed"
	}
	return "file acquisition failed: " + string(e.Code)
}

func (r *Result) Files() []AcquiredFile {
	if r == nil {
		return nil
	}
	return append([]AcquiredFile(nil), r.files...)
}

func (r *Result) TotalBytes() int64 {
	if r == nil {
		return 0
	}
	return r.totalBytes
}

func (r *Result) File(fileIndex int64) (AcquiredFile, bool) {
	if r == nil {
		return AcquiredFile{}, false
	}
	for _, file := range r.files {
		if file.FileIndex == fileIndex {
			return file, true
		}
	}
	return AcquiredFile{}, false
}

func (r *Result) TakeFiles() []AcquiredFile {
	if r == nil {
		return nil
	}
	files := r.files
	r.files = nil
	r.totalBytes = 0
	return files
}

func (r *Result) Release() {
	if r == nil {
		return
	}
	releaseFiles(r.files)
	r.files = nil
	r.totalBytes = 0
}
