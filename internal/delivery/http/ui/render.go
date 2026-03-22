package ui

import (
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	webassets "stageflow/web"
)

type renderer struct{}

func newRenderer() *renderer { return &renderer{} }

func (r *renderer) render(w http.ResponseWriter, status int, page string, data any) {
	t, err := template.New(path.Base(page)).Funcs(template.FuncMap{
		"join":          strings.Join,
		"contains":      strings.Contains,
		"eq":            func(left, right string) bool { return left == right },
		"uiURL":         uiURL,
		"statusClass":   statusClass,
		"stepTypeLabel": stepTypeLabel,
	}).ParseFS(webassets.Assets,
		"templates/layouts/*.html",
		"templates/partials/*.html",
		page,
	)
	if err != nil {
		http.Error(w, fmt.Sprintf("render templates: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", data); err != nil {
		http.Error(w, fmt.Sprintf("execute template: %v", err), http.StatusInternalServerError)
	}
}

func staticFS() fs.FS {
	sub, err := fs.Sub(webassets.Assets, "static")
	if err != nil {
		panic(err)
	}
	return sub
}

func statusClass(value string) string {
	switch strings.ToLower(value) {
	case "active", "succeeded":
		return "badge badge-success"
	case "draft", "queued", "running", "pending":
		return "badge badge-info"
	case "archived", "disabled", "canceled":
		return "badge badge-muted"
	case "failed":
		return "badge badge-danger"
	default:
		return "badge"
	}
}

func stepTypeLabel(value string) string {
	switch value {
	case "inline_request":
		return "Inline request"
	case "saved_request_ref":
		return "Saved request ref"
	default:
		return value
	}
}

func formatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.UTC().Format(time.RFC3339)
}

func writeString(w io.Writer, value string) {
	_, _ = io.WriteString(w, value)
}

func uiURL(lang, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	if strings.HasPrefix(raw, "/ui") {
		return applyLangQuery(stripLangQuery(raw), lang)
	}
	return raw
}
