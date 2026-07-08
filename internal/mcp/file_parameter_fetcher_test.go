package mcp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeResolver struct {
	addrs []net.IPAddr
	err   error
}

func (r fakeResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return r.addrs, r.err
}

type hostResolver struct {
	addrs map[string][]net.IPAddr
	errs  map[string]error
	calls []string
}

func (r *hostResolver) LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	r.calls = append(r.calls, host)
	if err := r.errs[host]; err != nil {
		return nil, err
	}
	return r.addrs[host], nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type recordingDialer struct {
	addresses []string
	err       error
}

func (d *recordingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	d.addresses = append(d.addresses, address)
	if d.err != nil {
		return nil, d.err
	}
	return fakeConn{}, nil
}

type fakeConn struct{}

func (fakeConn) Read(b []byte) (int, error)         { return 0, io.EOF }
func (fakeConn) Write(b []byte) (int, error)        { return len(b), nil }
func (fakeConn) Close() error                       { return nil }
func (fakeConn) LocalAddr() net.Addr                { return fakeAddr("local") }
func (fakeConn) RemoteAddr() net.Addr               { return fakeAddr("remote") }
func (fakeConn) SetDeadline(t time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(t time.Time) error { return nil }

type fakeAddr string

func (a fakeAddr) Network() string { return string(a) }
func (a fakeAddr) String() string  { return string(a) }

type errorBody struct{}

func (errorBody) Read(b []byte) (int, error) { return 0, errors.New("read failed") }
func (errorBody) Close() error               { return nil }

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

func TestHTTPSFileParameterFetcherDialsValidatedLiteralIP(t *testing.T) {
	resolver := fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}}
	dialer := &recordingDialer{}
	fetcher := &HTTPSFileParameterFetcher{Resolver: resolver, Dialer: dialer}
	targets := newValidatedTargetRegistry()
	u := mustParseURL(t, "https://files.example.test:8443/handoff?signature=secret")
	if err := fetcher.validateURL(context.Background(), u, targets); err != nil {
		t.Fatalf("expected validated URL, got %v", err)
	}
	conn, err := fetcher.validatedDialContext(targets)(context.Background(), "tcp", "files.example.test:8443")
	if err != nil {
		t.Fatalf("expected successful dial, got %v", err)
	}
	_ = conn.Close()
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "93.184.216.34:8443" {
		t.Fatalf("expected literal approved IP dial, got %#v", dialer.addresses)
	}
}

func TestHTTPSFileParameterFetcherValidationResultControlsDialTarget(t *testing.T) {
	resolver := &hostResolver{addrs: map[string][]net.IPAddr{
		"files.example.test": {{IP: net.ParseIP("93.184.216.34")}},
	}}
	dialer := &recordingDialer{}
	fetcher := &HTTPSFileParameterFetcher{Resolver: resolver, Dialer: dialer}
	targets := newValidatedTargetRegistry()
	u := mustParseURL(t, "https://files.example.test/handoff")
	if err := fetcher.validateURL(context.Background(), u, targets); err != nil {
		t.Fatalf("expected validated URL, got %v", err)
	}
	resolver.addrs["files.example.test"] = []net.IPAddr{{IP: net.ParseIP("93.184.216.35")}}
	_, err := fetcher.validatedDialContext(targets)(context.Background(), "tcp", "files.example.test:443")
	if err != nil {
		t.Fatalf("expected successful dial, got %v", err)
	}
	if len(resolver.calls) != 1 {
		t.Fatalf("expected one resolver call before dialing, got %d", len(resolver.calls))
	}
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "93.184.216.34:443" {
		t.Fatalf("expected originally approved IP, got %#v", dialer.addresses)
	}
}

func TestHTTPSFileParameterFetcherMissingDialTargetFailsClosed(t *testing.T) {
	dialer := &recordingDialer{}
	fetcher := &HTTPSFileParameterFetcher{Dialer: dialer}
	_, err := fetcher.validatedDialContext(newValidatedTargetRegistry())(context.Background(), "tcp", "files.example.test:443")
	fileErr := expectFileParamError(t, err, MCPBlockerUnsafeDownloadTarget)
	assertNoDownloadLeak(t, fileErr)
	if len(dialer.addresses) != 0 {
		t.Fatalf("unexpected dial for unapproved target: %#v", dialer.addresses)
	}
}

func TestHTTPSFileParameterFetcherRedirectTargetIsIndependentlyBound(t *testing.T) {
	resolver := &hostResolver{addrs: map[string][]net.IPAddr{
		"files.example.test":    {{IP: net.ParseIP("93.184.216.34")}},
		"redirect.example.test": {{IP: net.ParseIP("93.184.216.35")}},
	}}
	dialer := &recordingDialer{}
	fetcher := &HTTPSFileParameterFetcher{Resolver: resolver, Dialer: dialer}
	targets := newValidatedTargetRegistry()
	if err := fetcher.validateURL(context.Background(), mustParseURL(t, "https://files.example.test/handoff"), targets); err != nil {
		t.Fatalf("expected initial target validation, got %v", err)
	}
	client, _ := fetcher.client(targets)
	req := &http.Request{URL: mustParseURL(t, "https://redirect.example.test:9443/handoff")}
	via := []*http.Request{{URL: mustParseURL(t, "https://files.example.test/handoff")}}
	if err := client.CheckRedirect(req, via); err != nil {
		t.Fatalf("expected redirect validation, got %v", err)
	}
	_, err := fetcher.validatedDialContext(targets)(context.Background(), "tcp", "redirect.example.test:9443")
	if err != nil {
		t.Fatalf("expected redirect dial, got %v", err)
	}
	if len(dialer.addresses) != 1 || dialer.addresses[0] != "93.184.216.35:9443" {
		t.Fatalf("expected redirect IP binding, got %#v", dialer.addresses)
	}
}

func TestHTTPSFileParameterFetcherRejectsUnsafeRedirectBeforeFollow(t *testing.T) {
	resolver := &hostResolver{addrs: map[string][]net.IPAddr{
		"files.example.test":    {{IP: net.ParseIP("93.184.216.34")}},
		"redirect.example.test": {{IP: net.ParseIP("127.0.0.1")}},
	}}
	dialer := &recordingDialer{}
	fetcher := &HTTPSFileParameterFetcher{Resolver: resolver, Dialer: dialer}
	targets := newValidatedTargetRegistry()
	if err := fetcher.validateURL(context.Background(), mustParseURL(t, "https://files.example.test/handoff"), targets); err != nil {
		t.Fatalf("expected initial target validation, got %v", err)
	}
	client, _ := fetcher.client(targets)
	req := &http.Request{URL: mustParseURL(t, "https://redirect.example.test/handoff?signature=secret")}
	via := []*http.Request{{URL: mustParseURL(t, "https://files.example.test/handoff")}}
	fileErr := expectFileParamError(t, client.CheckRedirect(req, via), MCPBlockerUnsafeDownloadTarget)
	assertNoDownloadLeak(t, fileErr)
	_, err := fetcher.validatedDialContext(targets)(context.Background(), "tcp", "redirect.example.test:443")
	expectFileParamError(t, err, MCPBlockerUnsafeDownloadTarget)
	if len(dialer.addresses) != 0 {
		t.Fatalf("unsafe redirect should not dial, got %#v", dialer.addresses)
	}
}

func TestHTTPSFileParameterFetcherRedirectLimit(t *testing.T) {
	fetcher := &HTTPSFileParameterFetcher{Redirects: 1}
	client, _ := fetcher.client(newValidatedTargetRegistry())
	req := &http.Request{URL: mustParseURL(t, "https://files.example.test/again")}
	via := []*http.Request{{URL: mustParseURL(t, "https://files.example.test/handoff")}}
	fileErr := expectFileParamError(t, client.CheckRedirect(req, via), MCPBlockerFileDownloadFailed)
	assertNoDownloadLeak(t, fileErr)
}

func TestHTTPSFileParameterFetcherTransportFailureMappings(t *testing.T) {
	cases := []struct {
		name    string
		fetcher *HTTPSFileParameterFetcher
		code    string
	}{
		{
			name: "timeout",
			fetcher: &HTTPSFileParameterFetcher{
				Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
				Timeout:  5 * time.Millisecond,
				Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					<-req.Context().Done()
					return nil, req.Context().Err()
				})},
			},
			code: MCPBlockerFileDownloadFailed,
		},
		{
			name: "dns failure",
			fetcher: &HTTPSFileParameterFetcher{
				Resolver: fakeResolver{err: errors.New("no such host")},
			},
			code: MCPBlockerFileDownloadFailed,
		},
		{
			name: "non 2xx",
			fetcher: &HTTPSFileParameterFetcher{
				Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
				Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("blocked")), Header: http.Header{}}, nil
				})},
			},
			code: MCPBlockerFileDownloadStatus,
		},
		{
			name: "read failure",
			fetcher: &HTTPSFileParameterFetcher{
				Resolver: fakeResolver{addrs: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}},
				Client: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: 200, Body: errorBody{}, Header: http.Header{}}, nil
				})},
			},
			code: MCPBlockerFileDownloadFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.fetcher.FetchPlannerHandoff(context.Background(), ChatGPTFileReference{
				DownloadURL: "https://files.example.test/handoff?signature=secret",
				FileID:      "file-1",
				FileName:    "handoff.md",
			})
			if err == nil || err.Code != tc.code {
				t.Fatalf("expected %s, got %v", tc.code, err)
			}
			assertNoDownloadLeak(t, err)
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

func TestHTTPSFileParameterFetcherArtifactValidation(t *testing.T) {
	body := []byte("{\"schema_version\":\"1.0\"}\n")
	fetcher := &HTTPSFileParameterFetcher{
		Resolver: fakeResolver{addrs: []net.IPAddr{
			{IP: net.ParseIP("93.184.216.34")},
		}},
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
	for _, name := range []string{"feature.plan.json", "feature.execution-spec.json"} {
		t.Run(name, func(t *testing.T) {
			out, err := fetcher.FetchArtifact(context.Background(), ChatGPTFileReference{
				DownloadURL: "https://files.example.test/artifact?signature=secret",
				FileID:      "file-1",
				FileName:    name,
				MIMEType:    "application/json",
			})
			if err != nil {
				t.Fatal(err)
			}
			if out.DisplayName != name || string(out.Bytes) != string(body) {
				t.Fatalf("unexpected canonical file result: %+v", out)
			}
		})
	}

	for _, tc := range []struct {
		name     string
		fileName string
	}{
		{name: "missing", fileName: ""},
		{name: "markdown", fileName: "feature.md"},
		{name: "traversal", fileName: "../feature.plan.json"},
		{name: "nested", fileName: "dir/feature.plan.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := fetcher.FetchArtifact(context.Background(), ChatGPTFileReference{
				DownloadURL: "https://files.example.test/artifact",
				FileID:      "file-1",
				FileName:    tc.fileName,
			})
			if err == nil || err.Code != MCPBlockerFileReferenceInvalid {
				t.Fatalf("expected file_reference_invalid, got %v", err)
			}
		})
	}
}

func validMCPHandoffMarkdown(title, repoTarget string) string {
	return "---\ntitle: " + title + "\nrepo_target: " + repoTarget + "\nbranch_context: main\n---\n\n# " + title + "\n\nSynthetic handoff fixture body.\n"
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func expectFileParamError(t *testing.T, err error, code string) *FileParameterError {
	t.Helper()
	var fileErr *FileParameterError
	if !errors.As(err, &fileErr) {
		t.Fatalf("expected FileParameterError, got %T %[1]v", err)
	}
	if fileErr.Code != code {
		t.Fatalf("expected %s, got %s", code, fileErr.Code)
	}
	return fileErr
}

func assertNoDownloadLeak(t *testing.T, err *FileParameterError) {
	t.Helper()
	text := err.Code + " " + err.Message
	for _, secret := range []string{"signature=secret", "files.example.test", "redirect.example.test", "93.184.216.34", "93.184.216.35", "127.0.0.1"} {
		if strings.Contains(text, secret) {
			t.Fatalf("download error leaked %q in %q", secret, text)
		}
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
