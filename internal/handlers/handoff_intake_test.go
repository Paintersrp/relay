package handlers

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDeriveRunTitle_ProvidedTitleWins(t *testing.T) {
	got := deriveRunTitle("Manual title", "# H1 title\n\nbody")
	if got != "Manual title" {
		t.Fatalf("expected 'Manual title', got %q", got)
	}
}

func TestDeriveRunTitle_BlankTitleDerivesH1(t *testing.T) {
	got := deriveRunTitle("", "# H1 title\n\nbody")
	if got != "H1 title" {
		t.Fatalf("expected 'H1 title', got %q", got)
	}
}

func TestDeriveRunTitle_BlankTitleNoH1Fallback(t *testing.T) {
	got := deriveRunTitle("", "no h1 here")
	if got != "Untitled handoff" {
		t.Fatalf("expected 'Untitled handoff', got %q", got)
	}
}

func TestDeriveRunTitle_WhitespaceTitleFallsBack(t *testing.T) {
	got := deriveRunTitle("  ", "# H1 title\n\nbody")
	if got != "H1 title" {
		t.Fatalf("expected 'H1 title', got %q", got)
	}
}

func TestResolveHandoffText_PasteOnly(t *testing.T) {
	form := url.Values{"handoff_text": {"Pasted text"}}
	req := httptest.NewRequest(http.MethodPost, "/handoffs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	text, source, err := resolveHandoffText(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "Pasted text" {
		t.Fatalf("expected 'Pasted text', got %q", text)
	}
	if source != "paste" {
		t.Fatalf("expected source 'paste', got %q", source)
	}
}

func TestResolveHandoffText_UploadWinsOverPaste(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("handoff_text", "pasted content")
	fileWriter, _ := w.CreateFormFile("handoff_file", "handoff.md")
	fileWriter.Write([]byte("uploaded content"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/handoffs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	text, source, err := resolveHandoffText(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "uploaded content" {
		t.Fatalf("expected 'uploaded content', got %q", text)
	}
	if source != "upload" {
		t.Fatalf("expected source 'upload', got %q", source)
	}
}

func TestResolveHandoffText_UploadOnly(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fileWriter, _ := w.CreateFormFile("handoff_file", "handoff.txt")
	fileWriter.Write([]byte("file content"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/handoffs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	text, source, err := resolveHandoffText(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "file content" {
		t.Fatalf("expected 'file content', got %q", text)
	}
	if source != "upload" {
		t.Fatalf("expected source 'upload', got %q", source)
	}
}

func TestResolveHandoffText_UnsupportedExtension(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fileWriter, _ := w.CreateFormFile("handoff_file", "handoff.pdf")
	fileWriter.Write([]byte("content"))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/handoffs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	_, _, err := resolveHandoffText(req)
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
	if !strings.Contains(err.Error(), ".txt or .md") {
		t.Fatalf("expected error about .txt or .md, got %v", err)
	}
}

func TestResolveHandoffText_EmptyPasteAndNoUpload(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/handoffs", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, _, err := resolveHandoffText(req)
	if err == nil {
		t.Fatal("expected error for empty submission")
	}
}

func TestResolveHandoffText_EmptyUpload(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	w.WriteField("handoff_text", "")
	fileWriter, _ := w.CreateFormFile("handoff_file", "empty.md")
	fileWriter.Write([]byte(""))
	w.Close()

	req := httptest.NewRequest(http.MethodPost, "/handoffs", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())

	_, _, err := resolveHandoffText(req)
	if err == nil {
		t.Fatal("expected error for empty upload")
	}
}

func TestResolveHandoffText_PastePreservesBody(t *testing.T) {
	form := url.Values{"handoff_text": {"# Title\n\nBody content\n"}}
	req := httptest.NewRequest(http.MethodPost, "/handoffs", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	text, source, err := resolveHandoffText(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "# Title\n\nBody content\n" {
		t.Fatalf("expected original body preserved, got %q", text)
	}
	if source != "paste" {
		t.Fatalf("expected source 'paste', got %q", source)
	}
}
