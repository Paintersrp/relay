package mcp

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxPlannerHandoffFileBytes = 1 * 1024 * 1024
	fileDownloadTimeout        = 15 * time.Second
	fileDownloadRedirects      = 5
)

type ChatGPTFileReference struct {
	DownloadURL string `json:"download_url"`
	FileID      string `json:"file_id"`
	MIMEType    string `json:"mime_type,omitempty"`
	FileName    string `json:"file_name,omitempty"`
}

type FileParameterContent struct {
	Bytes       []byte
	DisplayName string
}

type FileParameterFetcher interface {
	FetchPlannerHandoff(ctx context.Context, ref ChatGPTFileReference) (FileParameterContent, *FileParameterError)
}

type FileParameterError struct {
	Code    string
	Message string
}

func (e *FileParameterError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

type HTTPSFileParameterFetcher struct {
	Client   *http.Client
	Resolver interface {
		LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
	}
	Timeout   time.Duration
	MaxBytes  int64
	Redirects int
}

func NewHTTPSFileParameterFetcher() *HTTPSFileParameterFetcher {
	return &HTTPSFileParameterFetcher{}
}

func (f *HTTPSFileParameterFetcher) FetchPlannerHandoff(ctx context.Context, ref ChatGPTFileReference) (FileParameterContent, *FileParameterError) {
	displayName, err := plannerHandoffDisplayName(ref.FileName)
	if err != nil {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, err.Error())
	}
	if strings.TrimSpace(ref.FileID) == "" {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.file_id is required")
	}
	rawURL := strings.TrimSpace(ref.DownloadURL)
	if rawURL == "" {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.download_url is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u == nil || !u.IsAbs() {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.download_url must be an absolute HTTPS URL")
	}
	if err := f.validateURL(ctx, u); err != nil {
		return FileParameterContent{}, err
	}

	timeout := f.Timeout
	if timeout <= 0 {
		timeout = fileDownloadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := f.client()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.download_url could not be requested")
	}
	req.Header.Set("Accept", "text/markdown, text/plain, application/octet-stream;q=0.8, */*;q=0.1")

	resp, err := client.Do(req)
	if err != nil {
		var fileErr *FileParameterError
		if errors.As(err, &fileErr) {
			return FileParameterContent{}, fileErr
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file download timed out")
		}
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file could not be downloaded")
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadStatus, fmt.Sprintf("planner_handoff_file download returned HTTP %d", resp.StatusCode))
	}
	limit := f.MaxBytes
	if limit <= 0 {
		limit = maxPlannerHandoffFileBytes
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file response could not be read")
	}
	if len(data) == 0 {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadEmpty, "planner_handoff_file response was empty")
	}
	if int64(len(data)) > limit {
		return FileParameterContent{}, fileParamErr(MCPBlockerFileDownloadTooLarge, "planner_handoff_file exceeds the 1 MiB limit")
	}
	return FileParameterContent{Bytes: data, DisplayName: displayName}, nil
}

func (f *HTTPSFileParameterFetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	redirects := f.Redirects
	if redirects <= 0 {
		redirects = fileDownloadRedirects
	}
	return &http.Client{
		Timeout: f.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= redirects {
				return fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file download redirected too many times")
			}
			if err := f.validateURL(req.Context(), req.URL); err != nil {
				return err
			}
			return nil
		},
	}
}

func (f *HTTPSFileParameterFetcher) validateURL(ctx context.Context, u *url.URL) *FileParameterError {
	if !strings.EqualFold(u.Scheme, "https") {
		return fileParamErr(MCPBlockerUnsafeDownloadTarget, "planner_handoff_file.download_url must use HTTPS")
	}
	if u.User != nil {
		return fileParamErr(MCPBlockerUnsafeDownloadTarget, "planner_handoff_file.download_url must not include userinfo")
	}
	host := u.Hostname()
	if strings.TrimSpace(host) == "" {
		return fileParamErr(MCPBlockerFileReferenceInvalid, "planner_handoff_file.download_url host is required")
	}
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicRoutableIP(ip) {
			return fileParamErr(MCPBlockerUnsafeDownloadTarget, "planner_handoff_file.download_url target is not public routable")
		}
		return nil
	}
	resolver := f.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	addrs, err := resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file.download_url host could not be resolved")
	}
	for _, addr := range addrs {
		if !isPublicRoutableIP(addr.IP) {
			return fileParamErr(MCPBlockerUnsafeDownloadTarget, "planner_handoff_file.download_url target is not public routable")
		}
	}
	return nil
}

func plannerHandoffDisplayName(name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "planner-handoff.md", nil
	}
	base := safeArtifactDisplayName(filepath.Base(name), "planner-handoff.md")
	if !strings.EqualFold(filepath.Ext(base), ".md") {
		return "", fmt.Errorf("planner_handoff_file.file_name must use the .md extension")
	}
	return base, nil
}

func isPublicRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast())
}

func fileParamErr(code, message string) *FileParameterError {
	return &FileParameterError{Code: code, Message: message}
}

func fileParameterBlocker(err *FileParameterError) MCPBlocker {
	if err == nil {
		err = fileParamErr(MCPBlockerFileDownloadFailed, "planner_handoff_file could not be downloaded")
	}
	recoverable := err.Code != MCPBlockerUnsafeDownloadTarget
	return newMCPBlocker(err.Code, err.Message, recoverable, []MCPBlockerEvidence{{Kind: "field", Ref: "planner_handoff_file"}}, []string{"Attach one reviewed Markdown handoff file and retry."})
}
