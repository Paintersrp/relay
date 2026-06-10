package handlers

import (
	"html"
	"net/http"

	"relay/internal/instructions"

	"github.com/go-chi/chi/v5"
)

type InstructionsHandler struct{}

func NewInstructionsHandler() *InstructionsHandler {
	return &InstructionsHandler{}
}

type InstructionAssetView struct {
	Key         string
	Label       string
	Filename    string
	Source      string
	Description string
	Content     string
}

func (h *InstructionsHandler) List(w http.ResponseWriter, r *http.Request) {
	assets := instructions.Registry()
	views := make([]InstructionAssetView, 0, len(assets))
	for _, a := range assets {
		views = append(views, InstructionAssetView{
			Key:         a.Key,
			Label:       a.Label,
			Filename:    a.Filename,
			Source:      a.Source,
			Description: a.Description,
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Instruction Assets - Relay</title>
<link rel="stylesheet" href="/static/app.css">
</head>
<body class="bg-gray-950 text-gray-200 min-h-screen">
<div class="max-w-4xl mx-auto p-4 sm:p-6">
  <div class="flex items-center justify-between mb-6">
    <h1 class="text-xl font-bold">Instruction Assets</h1>
    <a href="/" class="text-xs text-indigo-400 hover:text-indigo-300 min-h-[36px] inline-flex items-center">&larr; Back to Dashboard</a>
  </div>
  <p class="text-sm text-gray-500 mb-6">
    Canonical project instruction files. These are the source-of-truth files that repo agents and tools use.
  </p>
  <div class="grid grid-cols-1 gap-4">
`))

	for _, v := range views {
		viewPath := "/instructions/" + v.Key
		downloadPath := viewPath + "/download"
		label := html.EscapeString(v.Label)
		source := html.EscapeString(v.Source)
		desc := html.EscapeString(v.Description)

		w.Write([]byte(`    <div class="relay-card">
      <div class="relay-card-header">
        <div class="min-w-0">
          <h3 class="font-medium text-sm sm:text-base">` + label + `</h3>
          <p class="text-xs text-gray-500 mt-0.5">Source: ` + source + `</p>
        </div>
      </div>
      <div class="px-4 pb-4">
        <p class="text-xs text-gray-400 mb-3">` + desc + `</p>
        <div class="flex flex-wrap items-center gap-3">
          <a href="` + viewPath + `" class="text-xs text-indigo-400 hover:text-indigo-300 min-h-[36px] inline-flex items-center">View</a>
          <a href="` + downloadPath + `" class="text-xs text-gray-500 hover:text-gray-300 min-h-[36px] inline-flex items-center">Download</a>
        </div>
      </div>
    </div>
`))
	}

	w.Write([]byte(`  </div>
</div>
</body>
</html>`))
}

func (h *InstructionsHandler) View(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "kind")
	asset := instructions.FindAsset(key)
	if asset == nil {
		http.Error(w, "instruction asset not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Write([]byte(asset.Content))
}

func (h *InstructionsHandler) Download(w http.ResponseWriter, r *http.Request) {
	key := chi.URLParam(r, "kind")
	asset := instructions.FindAsset(key)
	if asset == nil {
		http.Error(w, "instruction asset not found", http.StatusNotFound)
		return
	}

	contentType, disposition := instructions.AssetDownloadHeaders(asset.Filename)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Write([]byte(asset.Content))
}
