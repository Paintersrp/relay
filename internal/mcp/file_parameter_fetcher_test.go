package mcp

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
)

type fakeResolver struct {
	addrs []net.IPAddr
	err   error
}

func (r fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.addrs, r.err
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHTTPSFileParameterFetcherValidation(t *testing.T) {
	cases := []struct {
		name string
		ref  ChatGPTFileReference
		code string
	}{
		{name: "missing download url", ref: ChatGPTFileReference{FileID: "file-1", FileName: "handoff.md"}, code: MCPBlockerFileReferenceInvalid},
		{name: "missing file id", ref: ChatGPTFileReference{DownloadURL: "https://files.example.test/handoff", FileName: "handoff.md"}, code: MCPBlockerFileReferenceInvalid},
		{name: "non https", ref: ChatGPTFileReference{DownloadURL: "http://files.example.test/handoff", FileID: "file-1", FileName: "handoff.md"}, code: MCPBlockerUnsafeDownloadTarget},
		{name: "userinfo", ref: ChatGPTFileReference{DownloadURL: "https://user:pass@files.example.test/handoff", FileID: "file-1", FileName: "handoff.md"}, code: MCPBlockerUnsafeDownloadTarget},
		{name: "bad extension", ref: ChatGPTFileReference{DownloadURL: "https://files.example.test/handoff", FileID: "file-1", FileName: "handoff.txt"}, code: MCPBlockerFileReferenceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &HTTPSFileParameterFetcher{Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}}}
			_, err := fetcher.FetchPlannerHandoff(context.Background(), tc.ref)
			if err == nil || err.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
}

func TestHTTPSFileParameterFetcherRejectsUnsafeResolvedAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "10.1.2.3", "169.254.1.1", "0.0.0.0", "224.0.0.1"} {
		t.Run(ip, func(t *testing.T) {
			fetcher := &HTTPSFileParameterFetcher{Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP(ip)}}}}
			_, err := fetcher.FetchPlannerHandoff(context.Background(), ChatGPTFileReference{
				DownloadURL: "https://files.example.test/handoff",
				FileID:      "file-1",
				FileName:    "handoff.md",
			})
			if err == nil || err.Code != MCPBlockerUnsafeDownloadTarget {
				t.Fatalf("expected unsafe target for %s, got %v", ip, err)
			}
		})
	}
}

func TestHTTPSFileParameterFetcherReadsBoundedExactBytes(t *testing.T) {
	body := []byte(validMCPHandoffMarkdown("Fetcher", "test-repo"))
	fetcher := &HTTPSFileParameterFetcher{
		Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
		Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Fatal("fetcher must not send relay credentials")
			}
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(string(body))),
				Header:     http.Header{},
			}, nil
		})},
	}
	out, err := fetcher.FetchPlannerHandoff(context.Background(), ChatGPTFileReference{
		DownloadURL: "https://files.example.test/handoff?signature=secret",
		FileID:      "file-1",
		FileName:    "Reviewed.MD",
		MIMEType:    "text/markdown",
	})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if string(out.Bytes) != string(body) {
		t.Fatal("fetcher changed response bytes")
	}
	if out.DisplayName != "Reviewed.MD" {
		t.Fatalf("unexpected display name %q", out.DisplayName)
	}
}

func TestHTTPSFileParameterFetcherRejectsEmptyAndOversizedBodies(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code string
	}{
		{name: "empty", body: "", code: MCPBlockerFileDownloadEmpty},
		{name: "oversized", body: strings.Repeat("a", maxPlannerHandoffFileBytes+1), code: MCPBlockerFileDownloadTooLarge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fetcher := &HTTPSFileParameterFetcher{
				Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
				Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
				})},
			}
			_, err := fetcher.FetchPlannerHandoff(context.Background(), ChatGPTFileReference{
				DownloadURL: "https://files.example.test/handoff",
				FileID:      "file-1",
				FileName:    "handoff.md",
			})
			if err == nil || err.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
		})
	}
}
