package fileacquisition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

var mediaTypePattern = regexp.MustCompile(`^[A-Za-z0-9!#$&^_.+-]+/[A-Za-z0-9!#$&^_.+-]+(?:;[ -~]+)?$`)

func Acquire(ctx context.Context, fetcher FetchOne, request Request) (Result, error) {
	if fetcher == nil {
		return Result{}, acquisitionError(ErrorInvalidRequest)
	}
	declared, err := validateRequest(request)
	if err != nil {
		return Result{}, err
	}
	result := Result{files: make([]AcquiredFile, 0, len(declared))}
	for _, declaration := range declared {
		fetched, fetchErr := fetcher.FetchFile(ctx, request.Files[declaration.FileIndex])
		if fetchErr != nil {
			releaseBytes(fetched.Bytes)
			result.Release()
			return Result{}, acquisitionError(ErrorFetchFailed)
		}
		owned := append([]byte(nil), fetched.Bytes...)
		releaseBytes(fetched.Bytes)
		if len(owned) == 0 {
			releaseBytes(owned)
			result.Release()
			return Result{}, acquisitionError(ErrorFetchFailed)
		}
		nextTotal := result.totalBytes + int64(len(owned))
		if nextTotal > MaxAggregateBytes {
			releaseBytes(owned)
			result.Release()
			return Result{}, acquisitionError(ErrorAggregateLimit)
		}
		sum := sha256.Sum256(owned)
		actual := hex.EncodeToString(sum[:])
		if actual != declaration.ExpectedSHA256 {
			releaseBytes(owned)
			result.Release()
			return Result{}, acquisitionError(ErrorDigestMismatch)
		}
		result.files = append(result.files, AcquiredFile{
			FileIndex:   declaration.FileIndex,
			Bytes:       owned,
			DisplayName: declaration.DisplayName,
			MediaType:   declaration.MediaType,
			SHA256:      actual,
			SizeBytes:   int64(len(owned)),
		})
		result.totalBytes = nextTotal
	}
	return result, nil
}

func validateRequest(request Request) ([]DeclaredFile, error) {
	if len(request.Files) > MaxFiles || len(request.Declared) > MaxFiles {
		return nil, acquisitionError(ErrorFileCount)
	}
	if len(request.Files) != len(request.Declared) {
		return nil, acquisitionError(ErrorFileCoverage)
	}
	declared := append([]DeclaredFile(nil), request.Declared...)
	seen := make(map[int64]struct{}, len(declared))
	for _, value := range declared {
		if value.FileIndex < 0 || value.FileIndex > MaxFileIndex || value.FileIndex >= int64(len(request.Files)) {
			return nil, acquisitionError(ErrorFileIndex)
		}
		if _, duplicate := seen[value.FileIndex]; duplicate {
			return nil, acquisitionError(ErrorFileCoverage)
		}
		seen[value.FileIndex] = struct{}{}
		if !validSHA256(value.ExpectedSHA256) || !validDisplayName(value.DisplayName) || !validMediaType(value.MediaType) {
			return nil, acquisitionError(ErrorFileIdentity)
		}
	}
	for index := range request.Files {
		if _, ok := seen[int64(index)]; !ok {
			return nil, acquisitionError(ErrorFileCoverage)
		}
	}
	sort.Slice(declared, func(i, j int) bool { return declared[i].FileIndex < declared[j].FileIndex })
	return declared, nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validDisplayName(value string) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) && len(value) >= 1 && len(value) <= 1024
}

func validMediaType(value string) bool {
	return utf8.ValidString(value) && value == strings.TrimSpace(value) && len(value) <= 255 && mediaTypePattern.MatchString(value)
}

func acquisitionError(code ErrorCode) error {
	return &Error{Code: code}
}

func releaseFiles(files []AcquiredFile) {
	for index := range files {
		releaseBytes(files[index].Bytes)
		files[index].Bytes = nil
	}
}

func releaseBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
