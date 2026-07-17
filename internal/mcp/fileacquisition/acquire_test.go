package fileacquisition

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

type recordingFetcher struct {
	contents [][]byte
	order    []string
	failAt   int
}

func (f *recordingFetcher) FetchFile(_ context.Context, file FileParameter) (FetchedFile, error) {
	f.order = append(f.order, file.FileID)
	index := len(f.order) - 1
	if f.failAt >= 0 && index == f.failAt {
		return FetchedFile{}, errors.New("transport failure with hidden URL")
	}
	return FetchedFile{Bytes: append([]byte(nil), f.contents[index]...)}, nil
}

func TestAcquireBoundariesAndAscendingOrder(t *testing.T) {
	for _, test := range []struct {
		name  string
		count int
	}{{name: "zero", count: 0}, {name: "one", count: 1}, {name: "sixty_four", count: 64}} {
		t.Run(test.name, func(t *testing.T) {
			count := test.count
			request, contents := acquisitionFixture(count)
			fetcher := &recordingFetcher{contents: contents, failAt: -1}
			result, err := Acquire(context.Background(), fetcher, request)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Release()
			if len(result.Files()) != count {
				t.Fatalf("files = %d, want %d", len(result.Files()), count)
			}
			for index, id := range fetcher.order {
				if id != "file-"+decimal(index) {
					t.Fatalf("fetch order[%d] = %q", index, id)
				}
			}
		})
	}
}

func TestAcquireNeverFetchesMoreThanOneFileAtATime(t *testing.T) {
	request, contents := acquisitionFixture(4)
	var active atomic.Int64
	var maximum atomic.Int64
	fetcher := FetchFunc(func(_ context.Context, file FileParameter) (FetchedFile, error) {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		value := append([]byte(nil), contents[atoiFileID(file.FileID)]...)
		active.Add(-1)
		return FetchedFile{Bytes: value}, nil
	})
	result, err := Acquire(context.Background(), fetcher, request)
	if err != nil {
		t.Fatal(err)
	}
	result.Release()
	if maximum.Load() != 1 {
		t.Fatalf("maximum concurrent fetches = %d", maximum.Load())
	}
}

func TestAcquireRejectsInvalidCoverageBeforeFetch(t *testing.T) {
	request, contents := acquisitionFixture(2)
	cases := []struct {
		name string
		edit func(*Request)
		code ErrorCode
	}{
		{name: "count", edit: func(r *Request) { r.Files = append(r.Files, make([]FileParameter, 63)...) }, code: ErrorFileCount},
		{name: "negative", edit: func(r *Request) { r.Declared[0].FileIndex = -1 }, code: ErrorFileIndex},
		{name: "index sixty four", edit: func(r *Request) { r.Declared[0].FileIndex = 64 }, code: ErrorFileIndex},
		{name: "duplicate", edit: func(r *Request) { r.Declared[1].FileIndex = r.Declared[0].FileIndex }, code: ErrorFileCoverage},
		{name: "missing", edit: func(r *Request) { r.Declared = r.Declared[:1] }, code: ErrorFileCoverage},
		{name: "bad sha", edit: func(r *Request) { r.Declared[0].ExpectedSHA256 = strings.Repeat("A", 64) }, code: ErrorFileIdentity},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			candidate := Request{Files: append([]FileParameter(nil), request.Files...), Declared: append([]DeclaredFile(nil), request.Declared...)}
			test.edit(&candidate)
			fetcher := &recordingFetcher{contents: contents, failAt: -1}
			_, err := Acquire(context.Background(), fetcher, candidate)
			assertAcquisitionCode(t, err, test.code)
			if len(fetcher.order) != 0 {
				t.Fatalf("invalid request fetched %v", fetcher.order)
			}
		})
	}
}

func TestAcquireDigestAggregateFailureAndCleanup(t *testing.T) {
	t.Run("digest mismatch", func(t *testing.T) {
		request, contents := acquisitionFixture(2)
		request.Declared[1].ExpectedSHA256 = strings.Repeat("f", 64)
		fetcher := &recordingFetcher{contents: contents, failAt: -1}
		_, err := Acquire(context.Background(), fetcher, request)
		assertAcquisitionCode(t, err, ErrorDigestMismatch)
	})

	t.Run("fetch failure is bounded", func(t *testing.T) {
		request, contents := acquisitionFixture(2)
		fetcher := &recordingFetcher{contents: contents, failAt: 1}
		_, err := Acquire(context.Background(), fetcher, request)
		assertAcquisitionCode(t, err, ErrorFetchFailed)
		if strings.Contains(err.Error(), "hidden") || strings.Contains(err.Error(), "URL") {
			t.Fatalf("fetch error leaked transport detail: %v", err)
		}
	})

	t.Run("aggregate exact and oversize", func(t *testing.T) {
		request, contents := acquisitionFixture(64)
		for index := range contents {
			contents[index] = make([]byte, 1024*1024)
		}
		for index := range request.Declared {
			fileIndex := request.Declared[index].FileIndex
			request.Declared[index].ExpectedSHA256 = digest(contents[fileIndex])
		}
		result, err := Acquire(context.Background(), &recordingFetcher{contents: contents, failAt: -1}, request)
		if err != nil {
			t.Fatal(err)
		}
		if result.TotalBytes() != MaxAggregateBytes {
			t.Fatalf("total = %d", result.TotalBytes())
		}
		result.Release()

		contents[63] = append(contents[63], 0)
		for index := range request.Declared {
			if request.Declared[index].FileIndex == 63 {
				request.Declared[index].ExpectedSHA256 = digest(contents[63])
			}
		}
		_, err = Acquire(context.Background(), &recordingFetcher{contents: contents, failAt: -1}, request)
		assertAcquisitionCode(t, err, ErrorAggregateLimit)
	})

	t.Run("owned bytes are zeroed after failure", func(t *testing.T) {
		first := []byte("first-secret-bearing-transient")
		second := []byte("second")
		request, _ := acquisitionFixture(2)
		for index := range request.Declared {
			switch request.Declared[index].FileIndex {
			case 0:
				request.Declared[index].ExpectedSHA256 = digest(first)
			case 1:
				request.Declared[index].ExpectedSHA256 = strings.Repeat("f", 64)
			}
		}
		backing := [][]byte{first, second}
		fetcher := FetchFunc(func(_ context.Context, file FileParameter) (FetchedFile, error) {
			return FetchedFile{Bytes: backing[atoiFileID(file.FileID)]}, nil
		})
		_, err := Acquire(context.Background(), fetcher, request)
		assertAcquisitionCode(t, err, ErrorDigestMismatch)
		for fileIndex, value := range backing {
			for byteIndex, item := range value {
				if item != 0 {
					t.Fatalf("backing[%d][%d] was not zeroed", fileIndex, byteIndex)
				}
			}
		}
	})
}

func TestResultOwnership(t *testing.T) {
	request, contents := acquisitionFixture(1)
	result, err := Acquire(context.Background(), &recordingFetcher{contents: contents, failAt: -1}, request)
	if err != nil {
		t.Fatal(err)
	}
	file, ok := result.File(0)
	if !ok || string(file.Bytes) != "content-0" {
		t.Fatalf("file = %#v, %v", file, ok)
	}
	taken := result.TakeFiles()
	if len(taken) != 1 || len(result.Files()) != 0 || result.TotalBytes() != 0 {
		t.Fatalf("unexpected transfer state")
	}
	releaseFiles(taken)
}

func acquisitionFixture(count int) (Request, [][]byte) {
	request := Request{Files: make([]FileParameter, count), Declared: make([]DeclaredFile, count)}
	contents := make([][]byte, count)
	for index := 0; index < count; index++ {
		contents[index] = []byte("content-" + decimal(index))
		transportIndex := count - index - 1
		request.Files[transportIndex] = FileParameter{DownloadURL: "https://files.example/item", FileID: "file-" + decimal(transportIndex), MIMEType: "ignored/type", FileName: "ignored.name"}
		request.Declared[index] = DeclaredFile{FileIndex: int64(index), ExpectedSHA256: digest(contents[index]), DisplayName: "artifact-" + decimal(index) + ".json", MediaType: "application/json"}
	}
	transportContents := make([][]byte, count)
	for index := range request.Files {
		transportContents[index] = contents[index]
	}
	if count > 1 {
		request.Declared[0], request.Declared[count-1] = request.Declared[count-1], request.Declared[0]
	}
	return request, transportContents
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func assertAcquisitionCode(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var acquisitionErr *Error
	if !errors.As(err, &acquisitionErr) || acquisitionErr.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func decimal(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

func atoiFileID(value string) int {
	value = strings.TrimPrefix(value, "file-")
	result := 0
	for _, char := range value {
		result = result*10 + int(char-'0')
	}
	return result
}
