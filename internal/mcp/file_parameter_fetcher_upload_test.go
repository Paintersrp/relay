package mcp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"relay/internal/mcp/fileacquisition"
)

func TestHTTPSFileParameterFetcherUploadedFileBoundary(t *testing.T) {
	fetcher := &HTTPSFileParameterFetcher{
		Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("uploaded bytes")), Header: http.Header{}}, nil
		})},
	}
	content, err := fetcher.FetchFile(context.Background(), fileacquisition.FileParameter{DownloadURL: "https://files.example/item", FileID: "file-1", FileName: "provider-name.bin"})
	if err != nil {
		t.Fatal(err)
	}
	if string(content.Bytes) != "uploaded bytes" {
		t.Fatalf("bytes = %q", content.Bytes)
	}

	fetcher.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", maxPlannerHandoffFileBytes+1))), Header: http.Header{}}, nil
	})}
	_, err = fetcher.FetchFile(context.Background(), fileacquisition.FileParameter{DownloadURL: "https://files.example/item", FileID: "file-1"})
	var fileErr *FileParameterError
	if !errors.As(err, &fileErr) || fileErr.Code != MCPBlockerFileDownloadTooLarge {
		t.Fatalf("oversize error = %v", err)
	}
}
