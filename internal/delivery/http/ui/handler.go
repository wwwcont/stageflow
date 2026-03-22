package ui

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"stageflow/internal/config"
	"stageflow/internal/delivery/http/ui/forms"
	"stageflow/internal/delivery/http/ui/viewmodels"
	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

const (
	flashCookieName = "stageflow_flash"
	langCookieName  = "stageflow_lang"
)

type Handler struct {
	cfg      config.Config
	logger   *zap.Logger
	renderer *renderer
	services services
}

type services struct {
	workspaceManagement    usecase.WorkspaceManagementUseCase
	savedRequestManagement usecase.SavedRequestManagementUseCase
	flowManagement         usecase.FlowManagementUseCase
	runService             usecase.RunService
	curlImport             usecase.CurlImportUseCase
	runEvents              usecase.RunEventUseCase
}

func NewHandler(cfg config.Config, logger *zap.Logger, workspaceManagement usecase.WorkspaceManagementUseCase, savedRequestManagement usecase.SavedRequestManagementUseCase, flowManagement usecase.FlowManagementUseCase, runService usecase.RunService, curlImport usecase.CurlImportUseCase, runEvents usecase.RunEventUseCase) http.Handler {
	h := &Handler{
		cfg:      cfg,
		logger:   logger,
		renderer: newRenderer(),
		services: services{workspaceManagement: workspaceManagement, savedRequestManagement: savedRequestManagement, flowManagement: flowManagement, runService: runService, curlImport: curlImport, runEvents: runEvents},
	}
	mux := http.NewServeMux()
	prefix := cfg.UI.Prefix
	if prefix == "" {
		prefix = "/ui"
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS()))))
	mux.HandleFunc("GET "+prefix, h.home)
	mux.HandleFunc("GET "+prefix+"/", h.home)
	mux.HandleFunc("GET "+prefix+"/docs", h.docs)
	mux.HandleFunc("GET "+prefix+"/language", h.setLanguage)
	mux.HandleFunc("GET "+prefix+"/workspaces", h.workspaces)
	mux.HandleFunc("GET "+prefix+"/workspaces/new", h.workspaceNew)
	mux.HandleFunc("POST "+prefix+"/workspaces", h.workspaceCreate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}", h.workspaceOverview)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/edit", h.workspaceEdit)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/edit", h.workspaceUpdate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/archive", h.workspaceArchive)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/unarchive", h.workspaceUnarchive)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/requests", h.requests)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/requests/new", h.requestNew)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/requests", h.requestCreate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/requests/import-curl", h.requestImportCurl)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/requests/import-curl", h.requestImportCurlSubmit)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/requests/{requestId}", h.requestDetails)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/requests/{requestId}/edit", h.requestEdit)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/requests/{requestId}/edit", h.requestUpdate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/requests/{requestId}/run", h.requestRun)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows", h.flows)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/new", h.flowNew)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows", h.flowCreate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/{flowId}", h.flowDetails)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/{flowId}/edit", h.flowEdit)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/edit", h.flowUpdate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/validate", h.flowValidate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/run", h.flowRun)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/{flowId}/steps/import-curl", h.flowStepImportCurl)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps/import-curl", h.flowStepImportCurlSubmit)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/{flowId}/steps/new", h.flowStepNew)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps", h.flowStepCreate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/flows/{flowId}/steps/{stepId}/edit", h.flowStepEdit)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps/{stepId}/edit", h.flowStepUpdate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps/{stepId}/delete", h.flowStepDelete)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps/{stepId}/move-up", h.flowStepMoveUp)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/flows/{flowId}/steps/{stepId}/move-down", h.flowStepMoveDown)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/variables", h.variables)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/variables/new", h.variableNew)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/variables", h.variableCreate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/variables/{varId}/edit", h.variableEdit)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/variables/{varId}/edit", h.variableUpdate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/variables/{varId}/delete", h.variableDelete)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/secrets", h.secrets)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/secrets/new", h.secretNew)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/secrets", h.secretCreate)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/secrets/{secretId}/delete", h.secretDelete)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/policy", h.policy)
	mux.HandleFunc("POST "+prefix+"/workspaces/{id}/policy", h.policyUpdate)
	mux.HandleFunc("GET "+prefix+"/workspaces/{id}/runs", h.runs)
	mux.HandleFunc("GET "+prefix+"/runs/{id}", h.runDetails)
	mux.HandleFunc("GET "+prefix+"/runs/{id}/events", h.runEventsStream)
	return mux
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	workspaces, err := h.services.workspaceManagement.ListWorkspaces(r.Context(), usecase.ListWorkspacesQuery{})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	items := make([]viewmodels.WorkspaceListItem, 0, len(workspaces))
	for _, ws := range workspaces {
		items = append(items, workspaceItem(ws))
	}
	h.renderPage(w, http.StatusOK, "templates/pages/home.html", viewmodels.HomePage{
		Page:       h.basePage(r, h.text(r, "StageFlow UI", "StageFlow UI"), h.text(r, "Built-in server-rendered admin UI", "Встроенный серверный интерфейс администрирования"), nil, ""),
		Workspaces: items,
	})
}

func (h *Handler) docs(w http.ResponseWriter, r *http.Request) {
	page := h.basePage(r, h.text(r, "Documentation", "Документация"), h.text(r, "Practical guides for flows, cURL imports, sandbox usage, and troubleshooting.", "Практические инструкции по flow, импорту cURL, sandbox и отладке."), []viewmodels.Breadcrumb{{Label: h.text(r, "Docs", "Документация"), URL: h.uiPath("/docs")}}, h.uiPath("/docs"))
	h.renderPage(w, http.StatusOK, "templates/pages/docs.html", viewmodels.DocsPage{Page: page})
}

func (h *Handler) setLanguage(w http.ResponseWriter, r *http.Request) {
	lang := normalizeLang(r.URL.Query().Get("lang"))
	next := strings.TrimSpace(r.URL.Query().Get("next"))
	if next == "" || !strings.HasPrefix(next, h.uiPath("")) {
		next = h.uiPath("")
	}
	next = applyLangQuery(stripLangQuery(next), lang)
	http.SetCookie(w, &http.Cookie{Name: langCookieName, Value: lang, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 365 * 24 * 60 * 60})
	http.Redirect(w, r, next, http.StatusSeeOther)
}

func (h *Handler) workspaces(w http.ResponseWriter, r *http.Request) {
	items, err := h.services.workspaceManagement.ListWorkspaces(r.Context(), usecase.ListWorkspacesQuery{})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	list := make([]viewmodels.WorkspaceListItem, 0, len(items))
	for _, item := range items {
		list = append(list, workspaceItem(item))
	}
	h.renderPage(w, http.StatusOK, "templates/pages/workspaces_list.html", viewmodels.WorkspaceListPage{Page: h.basePage(r, "Workspaces", "Browse and manage isolation boundaries", []viewmodels.Breadcrumb{{Label: "Workspaces", URL: h.uiPath("/workspaces")}}, ""), Workspaces: list})
}

func (h *Handler) workspaceNew(w http.ResponseWriter, r *http.Request) {
	h.renderPage(w, http.StatusOK, "templates/pages/workspace_form.html", viewmodels.WorkspaceFormPage{Page: h.basePage(r, "New workspace", "Create a new workspace", []viewmodels.Breadcrumb{{Label: "Workspaces", URL: h.uiPath("/workspaces")}, {Label: "New", URL: h.uiPath("/workspaces/new")}}, ""), Form: forms.WorkspaceForm{}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces")), SubmitLabel: "Create workspace"})
}

func (h *Handler) workspaceCreate(w http.ResponseWriter, r *http.Request) {
	form := forms.WorkspaceForm{ID: r.FormValue("id"), Name: r.FormValue("name"), Slug: r.FormValue("slug"), Description: r.FormValue("description"), OwnerTeam: r.FormValue("owner_team")}
	policy := defaultPolicy(h.cfg.Runtime.AllowedHosts)
	view, err := h.services.workspaceManagement.CreateWorkspace(r.Context(), usecase.CreateWorkspaceCommand{WorkspaceID: domain.WorkspaceID(strings.TrimSpace(form.ID)), Name: form.Name, Slug: form.Slug, Description: form.Description, OwnerTeam: form.OwnerTeam, Status: domain.WorkspaceStatusActive, Policy: policy})
	if err != nil {
		h.renderWorkspaceFormError(w, r, form, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(view.Workspace.ID)), forms.Flash{Kind: "success", Message: "Workspace created."})
}

func (h *Handler) workspaceOverview(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	requests, _ := h.services.savedRequestManagement.ListSavedRequests(r.Context(), usecase.ListSavedRequestsQuery{WorkspaceID: workspace.ID})
	flows, _ := h.services.flowManagement.ListFlows(r.Context(), usecase.ListFlowsQuery{WorkspaceID: workspace.ID})
	runs, _ := h.services.runService.ListRuns(r.Context(), usecase.ListRunsQuery{WorkspaceID: workspace.ID})
	policySummary := map[string]string{
		"Allowed hosts":      strings.Join(workspace.Policy.AllowedHosts, ", "),
		"Max saved requests": strconv.Itoa(workspace.Policy.MaxSavedRequests),
		"Max flows":          strconv.Itoa(workspace.Policy.MaxFlows),
		"Max steps per flow": strconv.Itoa(workspace.Policy.MaxStepsPerFlow),
		"Default timeout":    fmt.Sprintf("%d ms", workspace.Policy.DefaultTimeoutMS),
	}
	page := h.workspacePage(r, workspace, "Overview", h.uiPath("/workspaces/"+string(workspace.ID)), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/workspace_overview.html", viewmodels.WorkspaceOverviewPage{Page: page, Workspace: workspaceItem(workspace), Policy: policySummary, Stats: viewmodels.WorkspaceOverviewStats{SavedRequests: len(requests), Flows: len(flows), Runs: len(runs)}})
}

func (h *Handler) workspaceEdit(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.WorkspaceForm{ID: string(workspace.ID), Name: workspace.Name, Slug: workspace.Slug, Description: workspace.Description, OwnerTeam: workspace.OwnerTeam}
	page := h.workspacePage(r, workspace, "Edit workspace", h.uiPath("/workspaces/"+string(workspace.ID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/edit")}})
	h.renderPage(w, http.StatusOK, "templates/pages/workspace_form.html", viewmodels.WorkspaceFormPage{Page: page, Form: form, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/edit")), SubmitLabel: "Save workspace", WorkspaceID: string(workspace.ID), IsEdit: true})
}

func (h *Handler) workspaceUpdate(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.WorkspaceForm{ID: string(workspace.ID), Name: r.FormValue("name"), Slug: r.FormValue("slug"), Description: r.FormValue("description"), OwnerTeam: r.FormValue("owner_team")}
	_, err = h.services.workspaceManagement.UpdateWorkspace(r.Context(), usecase.UpdateWorkspaceCommand{WorkspaceID: workspace.ID, Name: form.Name, Slug: form.Slug, Description: form.Description, OwnerTeam: form.OwnerTeam})
	if err != nil {
		page := h.workspacePage(r, workspace, "Edit workspace", h.uiPath("/workspaces/"+string(workspace.ID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/edit")}})
		h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/workspace_form.html", viewmodels.WorkspaceFormPage{Page: withError(page, err), Form: form, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/edit")), SubmitLabel: "Save workspace", WorkspaceID: string(workspace.ID), IsEdit: true})
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)), forms.Flash{Kind: "success", Message: "Workspace updated."})
}

func (h *Handler) workspaceArchive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.services.workspaceManagement.ArchiveWorkspace(r.Context(), usecase.ArchiveWorkspaceCommand{WorkspaceID: domain.WorkspaceID(id)})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+id), forms.Flash{Kind: "success", Message: "Workspace archived."})
}

func (h *Handler) workspaceUnarchive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := h.services.workspaceManagement.UnarchiveWorkspace(r.Context(), usecase.UnarchiveWorkspaceCommand{WorkspaceID: domain.WorkspaceID(id)})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+id), forms.Flash{Kind: "success", Message: "Workspace restored from archive."})
}

func (h *Handler) requests(w http.ResponseWriter, r *http.Request) {
	workspace, items, err := h.loadWorkspaceAndRequests(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Saved requests", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/requests_list.html", viewmodels.SavedRequestListPage{Page: page, Workspace: workspaceItem(workspace), Requests: items})
}

func (h *Handler) requestNew(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "New saved request", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/new"), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: "New", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/new")}})
	h.renderPage(w, http.StatusOK, "templates/pages/request_form.html", viewmodels.SavedRequestFormPage{Page: page, Workspace: workspaceItem(workspace), Form: forms.RequestForm{Method: "GET"}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests")), SubmitLabel: "Create request"})
}

func (h *Handler) requestCreate(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := requestFormFromHTTP(r)
	cmd, err := h.requestCommandFromForm(workspace.ID, form, true)
	if err != nil {
		h.renderRequestFormError(w, r, workspace, form, err, false)
		return
	}
	view, err := h.services.savedRequestManagement.CreateSavedRequest(r.Context(), cmd)
	if err != nil {
		h.renderRequestFormError(w, r, workspace, form, err, false)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/"+string(view.SavedRequest.ID)), forms.Flash{Kind: "success", Message: "Saved request created."})
}

func (h *Handler) requestDetails(w http.ResponseWriter, r *http.Request) {
	workspace, req, err := h.loadSavedRequest(r.Context(), r.PathValue("id"), r.PathValue("requestId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, req.Name, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/"+string(req.ID)), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: req.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/" + string(req.ID))}})
	spec := requestSpecSummary(req.RequestSpec)
	h.renderPage(w, http.StatusOK, "templates/pages/request_detail.html", viewmodels.SavedRequestDetailsPage{Page: page, Workspace: workspaceItem(workspace), Request: savedRequestItem(req), Spec: spec, Form: forms.RunLaunchForm{InitiatedBy: "ui-user", InputJSON: "{}"}})
}

func (h *Handler) requestEdit(w http.ResponseWriter, r *http.Request) {
	workspace, req, err := h.loadSavedRequest(r.Context(), r.PathValue("id"), r.PathValue("requestId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Edit saved request", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/"+string(req.ID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: req.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/" + string(req.ID))}, {Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/" + string(req.ID) + "/edit")}})
	h.renderPage(w, http.StatusOK, "templates/pages/request_form.html", viewmodels.SavedRequestFormPage{Page: page, Workspace: workspaceItem(workspace), Form: formFromRequest(req), SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/"+string(req.ID)+"/edit")), SubmitLabel: "Save request", IsEdit: true})
}

func (h *Handler) requestUpdate(w http.ResponseWriter, r *http.Request) {
	workspace, req, err := h.loadSavedRequest(r.Context(), r.PathValue("id"), r.PathValue("requestId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := requestFormFromHTTP(r)
	cmd, err := h.requestUpdateCommandFromForm(workspace.ID, req.ID, form)
	if err != nil {
		h.renderRequestFormError(w, r, workspace, form, err, true)
		return
	}
	_, err = h.services.savedRequestManagement.UpdateSavedRequest(r.Context(), cmd)
	if err != nil {
		h.renderRequestFormError(w, r, workspace, form, err, true)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/"+string(req.ID)), forms.Flash{Kind: "success", Message: "Saved request updated."})
}

func (h *Handler) requestRun(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.PathValue("id"))
	requestID := domain.SavedRequestID(r.PathValue("requestId"))
	launch, err := h.parseRunLaunchForm(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	run, err := h.services.runService.RunSavedRequest(r.Context(), usecase.LaunchSavedRequestInput{WorkspaceID: workspaceID, SavedRequestID: requestID, InitiatedBy: launch.InitiatedBy, InputJSON: launch.InputJSON, Queue: launch.Queue, IdempotencyKey: launch.IdempotencyKey})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/runs/"+string(run.ID)+"?workspace_id="+url.QueryEscape(string(workspaceID))), forms.Flash{Kind: "success", Message: "Saved request queued."})
}

func (h *Handler) requestImportCurl(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Import cURL", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/import-curl"), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: "Import cURL", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/import-curl")}})
	h.renderPage(w, http.StatusOK, "templates/pages/curl_import.html", viewmodels.CurlImportPage{Page: page, Workspace: workspaceItem(workspace)})
}

func (h *Handler) requestImportCurlSubmit(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.CurlImportForm{Command: r.FormValue("command")}
	result, err := h.services.curlImport.ImportCurl(r.Context(), usecase.ImportCurlInput{Command: form.Command})
	if err != nil {
		page := h.workspacePage(r, workspace, "Import cURL", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/import-curl"), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: "Import cURL", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/import-curl")}})
		h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/curl_import.html", viewmodels.CurlImportPage{Page: withError(page, err), Workspace: workspaceItem(workspace), Form: form})
		return
	}
	preview := formFromRequest(domain.SavedRequest{RequestSpec: result.RequestSpec})
	h.renderPage(w, http.StatusOK, "templates/pages/curl_import.html", viewmodels.CurlImportPage{Page: h.workspacePage(r, workspace, "Import cURL", h.uiPath("/workspaces/"+string(workspace.ID)+"/requests/import-curl"), []viewmodels.Breadcrumb{{Label: "Saved requests", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests")}, {Label: "Import cURL", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/requests/import-curl")}}), Workspace: workspaceItem(workspace), Form: form, Preview: &preview})
}

func (h *Handler) flows(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	items, err := h.services.flowManagement.ListFlows(r.Context(), usecase.ListFlowsQuery{WorkspaceID: workspace.ID})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	list := make([]viewmodels.FlowListItem, 0, len(items))
	for _, item := range items {
		steps, _ := h.services.flowManagement.GetFlow(r.Context(), usecase.GetFlowQuery{WorkspaceID: workspace.ID, FlowID: item.ID})
		list = append(list, flowItem(item, len(steps.Steps)))
	}
	page := h.workspacePage(r, workspace, "Flows", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/flows_list.html", viewmodels.FlowListPage{Page: page, Workspace: workspaceItem(workspace), Flows: list})
}

func (h *Handler) flowNew(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "New flow", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/new"), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: "New", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/new")}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_form.html", viewmodels.FlowFormPage{Page: page, Workspace: workspaceItem(workspace), Form: forms.FlowForm{Status: string(domain.FlowStatusDraft)}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows")), SubmitLabel: "Create flow"})
}

func (h *Handler) flowCreate(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.FlowForm{ID: r.FormValue("id"), Name: r.FormValue("name"), Description: r.FormValue("description"), Status: r.FormValue("status")}
	view, err := h.services.flowManagement.CreateFlow(r.Context(), usecase.CreateFlowCommand{WorkspaceID: workspace.ID, FlowID: domain.FlowID(form.ID), Name: form.Name, Description: form.Description, Status: domain.FlowStatus(form.Status), Steps: []usecase.FlowStepDraft{}})
	if err != nil {
		h.renderFlowFormError(w, r, workspace, form, err, false, nil)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)), forms.Flash{Kind: "success", Message: "Flow created. Add steps next."})
}

func (h *Handler) flowDetails(w http.ResponseWriter, r *http.Request) {
	workspace, view, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, view.Flow.Name, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: view.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(view.Flow.ID))}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_detail.html", viewmodels.FlowDetailsPage{Page: page, Workspace: workspaceItem(workspace), Flow: flowItem(view.Flow, len(view.Steps)), Steps: flowStepItems(view.Steps), RunForm: forms.RunLaunchForm{InitiatedBy: "ui-user", InputJSON: "{}"}})
}

func (h *Handler) flowEdit(w http.ResponseWriter, r *http.Request) {
	workspace, view, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Edit flow", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: view.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(view.Flow.ID))}, {Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(view.Flow.ID) + "/edit")}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_form.html", viewmodels.FlowFormPage{Page: page, Workspace: workspaceItem(workspace), Form: forms.FlowForm{ID: string(view.Flow.ID), Name: view.Flow.Name, Description: view.Flow.Description, Status: string(view.Flow.Status)}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)+"/edit")), SubmitLabel: "Save flow", IsEdit: true, Steps: flowStepItems(view.Steps)})
}

func (h *Handler) flowUpdate(w http.ResponseWriter, r *http.Request) {
	workspace, view, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.FlowForm{ID: string(view.Flow.ID), Name: r.FormValue("name"), Description: r.FormValue("description"), Status: r.FormValue("status")}
	_, err = h.services.flowManagement.UpdateFlow(r.Context(), usecase.UpdateFlowCommand{WorkspaceID: workspace.ID, FlowID: view.Flow.ID, Name: form.Name, Description: form.Description, Status: domain.FlowStatus(form.Status), Steps: h.toStepDrafts(view.Steps)})
	if err != nil {
		h.renderFlowFormError(w, r, workspace, form, err, true, flowStepItems(view.Steps))
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)), forms.Flash{Kind: "success", Message: "Flow updated."})
}

func (h *Handler) flowValidate(w http.ResponseWriter, r *http.Request) {
	workspace, view, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	result, err := h.services.flowManagement.ValidateFlow(r.Context(), usecase.ValidateFlowCommand{WorkspaceID: workspace.ID, FlowID: view.Flow.ID, Name: view.Flow.Name, Description: view.Flow.Description, Status: view.Flow.Status, Steps: h.toStepDrafts(view.Steps)})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Edit flow", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: view.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(view.Flow.ID))}, {Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(view.Flow.ID) + "/edit")}})
	flash := forms.Flash{Kind: "success", Message: "Flow validation passed."}
	if !result.Valid {
		flash = forms.Flash{Kind: "error", Message: "Flow validation found issues."}
	}
	page.Flash = &flash
	h.renderPage(w, http.StatusOK, "templates/pages/flow_form.html", viewmodels.FlowFormPage{Page: page, Workspace: workspaceItem(workspace), Form: forms.FlowForm{ID: string(view.Flow.ID), Name: view.Flow.Name, Description: view.Flow.Description, Status: string(view.Flow.Status)}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(view.Flow.ID)+"/edit")), SubmitLabel: "Save flow", IsEdit: true, Steps: flowStepItems(view.Steps), ValidationIssues: result.Issues})
}

func (h *Handler) flowRun(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.PathValue("id"))
	flowID := domain.FlowID(r.PathValue("flowId"))
	launch, err := h.parseRunLaunchForm(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	run, err := h.services.runService.LaunchFlow(r.Context(), usecase.LaunchFlowInput{WorkspaceID: workspaceID, FlowID: flowID, InitiatedBy: launch.InitiatedBy, InputJSON: launch.InputJSON, Queue: launch.Queue, IdempotencyKey: launch.IdempotencyKey})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/runs/"+string(run.ID)+"?workspace_id="+url.QueryEscape(string(workspaceID))), forms.Flash{Kind: "success", Message: "Flow queued."})
}

func (h *Handler) flowStepNew(w http.ResponseWriter, r *http.Request) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	_, requests, _ := h.loadWorkspaceAndRequests(r.Context(), r.PathValue("id"))
	page := h.workspacePage(r, workspace, "New step", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps/new"), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: flow.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID))}, {Label: "New step", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID) + "/steps/new")}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_step_form.html", viewmodels.FlowStepFormPage{Page: page, Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: forms.FlowStepForm{StepType: string(domain.FlowStepTypeInlineRequest), InlineRequest: forms.RequestForm{Method: "GET"}, ExtractionRules: defaultExtractionRuleForms(nil)}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps")), SubmitLabel: "Add step", SavedRequests: requests})
}

func (h *Handler) flowStepImportCurl(w http.ResponseWriter, r *http.Request) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, h.text(r, "Import step from cURL", "Импорт шага из cURL"), h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps/import-curl"), []viewmodels.Breadcrumb{{Label: h.text(r, "Flows", "Флоу"), URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: flow.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID))}, {Label: h.text(r, "Import from cURL", "Импорт из cURL"), URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID) + "/steps/import-curl")}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_step_import_curl.html", viewmodels.FlowStepCurlImportPage{Page: page, Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: forms.CurlImportForm{}})
}

func (h *Handler) flowStepImportCurlSubmit(w http.ResponseWriter, r *http.Request) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.CurlImportForm{Command: r.FormValue("command")}
	result, err := h.services.curlImport.ImportCurl(r.Context(), usecase.ImportCurlInput{Command: form.Command})
	if err != nil {
		page := h.workspacePage(r, workspace, h.text(r, "Import step from cURL", "Импорт шага из cURL"), r.URL.Path, nil)
		h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/flow_step_import_curl.html", viewmodels.FlowStepCurlImportPage{Page: withError(page, err), Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: form})
		return
	}
	preview := h.previewStepFormFromCurl(result.RequestSpec, len(flow.Steps))
	page := h.workspacePage(r, workspace, h.text(r, "Import step from cURL", "Импорт шага из cURL"), r.URL.Path, nil)
	h.renderPage(w, http.StatusOK, "templates/pages/flow_step_import_curl.html", viewmodels.FlowStepCurlImportPage{Page: page, Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: form, Preview: &preview})
}

func (h *Handler) flowStepCreate(w http.ResponseWriter, r *http.Request) {
	h.flowStepMutate(w, r, "create")
}
func (h *Handler) flowStepEdit(w http.ResponseWriter, r *http.Request) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	stepID := domain.FlowStepID(r.PathValue("stepId"))
	step, ok := findStep(flow.Steps, stepID)
	if !ok {
		h.renderError(w, r, &domain.NotFoundError{Entity: "flow step", ID: string(stepID)})
		return
	}
	_, reqItems, _ := h.loadWorkspaceAndRequests(r.Context(), r.PathValue("id"))
	page := h.workspacePage(r, workspace, "Edit step", h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps/"+string(stepID)+"/edit"), []viewmodels.Breadcrumb{{Label: "Flows", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows")}, {Label: flow.Flow.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID))}, {Label: "Edit step", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/flows/" + string(flow.Flow.ID) + "/steps/" + string(stepID) + "/edit")}})
	h.renderPage(w, http.StatusOK, "templates/pages/flow_step_form.html", viewmodels.FlowStepFormPage{Page: page, Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: formFromStep(step), SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps/"+string(stepID)+"/edit")), SubmitLabel: "Save step", IsEdit: true, SavedRequests: reqItems})
}
func (h *Handler) flowStepUpdate(w http.ResponseWriter, r *http.Request) {
	h.flowStepMutate(w, r, "update")
}
func (h *Handler) flowStepDelete(w http.ResponseWriter, r *http.Request) {
	h.flowStepMutate(w, r, "delete")
}
func (h *Handler) flowStepMoveUp(w http.ResponseWriter, r *http.Request)   { h.flowStepMove(w, r, -1) }
func (h *Handler) flowStepMoveDown(w http.ResponseWriter, r *http.Request) { h.flowStepMove(w, r, 1) }

func (h *Handler) variables(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	items := make([]viewmodels.VariableListItem, 0, len(workspace.Variables))
	for _, item := range workspace.Variables {
		items = append(items, viewmodels.VariableListItem{Name: item.Name, Value: item.Value})
	}
	page := h.workspacePage(r, workspace, "Variables", h.uiPath("/workspaces/"+string(workspace.ID)+"/variables"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/variables.html", viewmodels.VariablesPage{Page: page, Workspace: workspaceItem(workspace), Variables: items})
}

func (h *Handler) variableNew(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "New variable", h.uiPath("/workspaces/"+string(workspace.ID)+"/variables/new"), []viewmodels.Breadcrumb{{Label: "Variables", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/variables")}, {Label: "New", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/variables/new")}})
	h.renderPage(w, http.StatusOK, "templates/pages/variable_form.html", viewmodels.VariableFormPage{Page: page, Workspace: workspaceItem(workspace), SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/variables")), SubmitLabel: "Save variable"})
}
func (h *Handler) variableCreate(w http.ResponseWriter, r *http.Request) { h.variableMutate(w, r, "") }
func (h *Handler) variableEdit(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	name := r.PathValue("varId")
	var value string
	for _, item := range workspace.Variables {
		if item.Name == name {
			value = item.Value
			break
		}
	}
	page := h.workspacePage(r, workspace, "Edit variable", h.uiPath("/workspaces/"+string(workspace.ID)+"/variables/"+name+"/edit"), []viewmodels.Breadcrumb{{Label: "Variables", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/variables")}, {Label: "Edit", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/variables/" + name + "/edit")}})
	h.renderPage(w, http.StatusOK, "templates/pages/variable_form.html", viewmodels.VariableFormPage{Page: page, Workspace: workspaceItem(workspace), Form: forms.VariableForm{Name: name, Value: value}, SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/variables/"+name+"/edit")), SubmitLabel: "Save variable", IsEdit: true, OriginalKey: name})
}
func (h *Handler) variableUpdate(w http.ResponseWriter, r *http.Request) {
	h.variableMutate(w, r, r.PathValue("varId"))
}
func (h *Handler) variableDelete(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	name := r.PathValue("varId")
	vars := make([]domain.WorkspaceVariable, 0, len(workspace.Variables))
	for _, item := range workspace.Variables {
		if item.Name != name {
			vars = append(vars, item)
		}
	}
	_, err = h.services.workspaceManagement.UpdateWorkspaceVariables(r.Context(), usecase.UpdateWorkspaceVariablesCommand{WorkspaceID: workspace.ID, Variables: vars})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/variables"), forms.Flash{Kind: "success", Message: "Variable deleted."})
}

func (h *Handler) secrets(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	secrets, err := h.services.workspaceManagement.ListWorkspaceSecrets(r.Context(), usecase.ListWorkspaceSecretsQuery{WorkspaceID: workspace.ID})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	items := make([]viewmodels.SecretListItem, 0, len(secrets))
	for _, item := range secrets {
		items = append(items, viewmodels.SecretListItem{Name: item.Name})
	}
	page := h.workspacePage(r, workspace, "Secrets", h.uiPath("/workspaces/"+string(workspace.ID)+"/secrets"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/secrets.html", viewmodels.SecretsPage{Page: page, Workspace: workspaceItem(workspace), Secrets: items})
}
func (h *Handler) secretNew(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "New secret", h.uiPath("/workspaces/"+string(workspace.ID)+"/secrets/new"), []viewmodels.Breadcrumb{{Label: "Secrets", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/secrets")}, {Label: "New", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/secrets/new")}})
	h.renderPage(w, http.StatusOK, "templates/pages/secret_form.html", viewmodels.SecretFormPage{Page: page, Workspace: workspaceItem(workspace), SubmitURL: h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/secrets")), SubmitLabel: "Save secret"})
}
func (h *Handler) secretCreate(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.PathValue("id"))
	form := forms.SecretForm{Name: r.FormValue("name"), Value: r.FormValue("value")}
	if err := h.services.workspaceManagement.PutWorkspaceSecret(r.Context(), usecase.PutWorkspaceSecretCommand{WorkspaceID: workspaceID, SecretName: form.Name, SecretValue: form.Value}); err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspaceID)+"/secrets"), forms.Flash{Kind: "success", Message: "Secret stored. Value is hidden after creation."})
}
func (h *Handler) secretDelete(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.PathValue("id"))
	if err := h.services.workspaceManagement.DeleteWorkspaceSecret(r.Context(), usecase.DeleteWorkspaceSecretCommand{WorkspaceID: workspaceID, SecretName: r.PathValue("secretId")}); err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspaceID)+"/secrets"), forms.Flash{Kind: "success", Message: "Secret deleted."})
}

func (h *Handler) policy(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	page := h.workspacePage(r, workspace, "Policy", h.uiPath("/workspaces/"+string(workspace.ID)+"/policy"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/policy.html", viewmodels.PolicyPage{Page: page, Workspace: workspaceItem(workspace), Form: policyForm(workspace.Policy)})
}
func (h *Handler) policyUpdate(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.PathValue("id"))
	policy, err := parsePolicyForm(r)
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	_, err = h.services.workspaceManagement.UpdateWorkspacePolicy(r.Context(), usecase.UpdateWorkspacePolicyCommand{WorkspaceID: workspaceID, Policy: policy})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspaceID)+"/policy"), forms.Flash{Kind: "success", Message: "Policy updated."})
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	runs, err := h.services.runService.ListRuns(r.Context(), usecase.ListRunsQuery{WorkspaceID: workspace.ID})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	items := make([]viewmodels.RunListItem, 0, len(runs))
	for _, run := range runs {
		items = append(items, h.runItem(r.Context(), run))
	}
	page := h.workspacePage(r, workspace, "Runs", h.uiPath("/workspaces/"+string(workspace.ID)+"/runs"), nil)
	h.renderPage(w, http.StatusOK, "templates/pages/runs.html", viewmodels.RunsPage{Page: page, Workspace: workspaceItem(workspace), Runs: items})
}

func (h *Handler) runDetails(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(r.URL.Query().Get("workspace_id"))
	view, err := h.services.runService.GetRunStatus(r.Context(), usecase.GetRunStatusQuery{WorkspaceID: workspaceID, RunID: domain.RunID(r.PathValue("id"))})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	workspace, err := h.loadWorkspace(r.Context(), string(view.Run.WorkspaceID))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	targetDetails := map[string]string{}
	if view.Run.IsFlowRun() {
		if flow, err := h.services.flowManagement.GetFlow(r.Context(), usecase.GetFlowQuery{WorkspaceID: workspace.ID, FlowID: view.Run.FlowID}); err == nil {
			targetDetails["Flow"] = flow.Flow.Name
		}
	} else if view.Run.IsSavedRequestRun() {
		if req, err := h.services.savedRequestManagement.GetSavedRequest(r.Context(), usecase.GetSavedRequestQuery{WorkspaceID: workspace.ID, SavedRequestID: view.Run.SavedRequestID}); err == nil {
			targetDetails["Saved request"] = req.SavedRequest.Name
		}
	}
	steps := make([]viewmodels.RunStepItem, 0, len(view.Steps))
	for _, step := range view.Steps {
		steps = append(steps, viewmodels.RunStepItem{Name: step.StepName, Status: string(step.Status), RetryCount: step.RetryCount, Duration: durationString(step.StartedAt, step.FinishedAt), ErrorMessage: step.ErrorMessage, RequestSnapshot: prettyJSON(step.RequestSnapshotJSON), ResponseSnapshot: prettyJSON(step.ResponseSnapshotJSON), ExtractedValues: prettyJSON(step.ExtractedValuesJSON), Attempts: prettyJSON(step.AttemptHistoryJSON)})
	}
	eventItems := []viewmodels.RunEventItem{}
	var lastEventSequence int64
	if h.services.runEvents != nil {
		events, listErr := h.services.runEvents.ListRunEvents(r.Context(), usecase.ListRunEventsQuery{WorkspaceID: workspace.ID, RunID: view.Run.ID})
		if listErr != nil {
			h.renderError(w, r, listErr)
			return
		}
		for _, event := range events {
			eventItems = append(eventItems, mapRunEvent(event))
			if event.Sequence > lastEventSequence {
				lastEventSequence = event.Sequence
			}
		}
	}
	page := h.workspacePage(r, workspace, "Run details", h.uiPath("/runs/"+string(view.Run.ID)+"?workspace_id="+url.QueryEscape(string(workspace.ID))), []viewmodels.Breadcrumb{{Label: "Runs", URL: h.uiPath("/workspaces/" + string(workspace.ID) + "/runs")}, {Label: string(view.Run.ID), URL: h.uiPath("/runs/" + string(view.Run.ID) + "?workspace_id=" + url.QueryEscape(string(workspace.ID)))}})
	if !view.Run.IsTerminal() {
		page.HasAutoRefresh = true
		page.AutoRefreshURL = h.uiURL(r, h.uiPath("/runs/"+string(view.Run.ID)+"?workspace_id="+url.QueryEscape(string(workspace.ID))))
	}
	streamState := "live"
	if view.Run.IsTerminal() {
		streamState = "completed"
	}
	h.renderPage(w, http.StatusOK, "templates/pages/run_detail.html", viewmodels.RunDetailsPage{Page: page, Workspace: workspaceItem(workspace), Run: h.runItem(r.Context(), view.Run), ErrorSummary: view.Run.ErrorMessage, InputJSON: prettyJSON(view.Run.InputJSON), RunSteps: steps, TargetDetails: targetDetails, RunEvents: eventItems, LastEventSequence: lastEventSequence, EventsStreamURL: h.uiURL(r, h.uiPath("/runs/"+string(view.Run.ID)+"/events?workspace_id="+url.QueryEscape(string(workspace.ID))+"&after="+strconv.FormatInt(lastEventSequence, 10))), StreamState: streamState})
}

func (h *Handler) runEventsStream(w http.ResponseWriter, r *http.Request) {
	if h.services.runEvents == nil {
		http.Error(w, "run event streaming is not configured", http.StatusNotImplemented)
		return
	}
	workspaceID := domain.WorkspaceID(r.URL.Query().Get("workspace_id"))
	runID := domain.RunID(r.PathValue("id"))
	afterSequence := parseAfterSequence(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	live, cancel, err := h.services.runEvents.SubscribeRunEvents(r.Context(), usecase.ListRunEventsQuery{WorkspaceID: workspaceID, RunID: runID, AfterSequence: afterSequence})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	defer cancel()

	history, err := h.services.runEvents.ListRunEvents(r.Context(), usecase.ListRunEventsQuery{WorkspaceID: workspaceID, RunID: runID, AfterSequence: afterSequence})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	lastSequence := afterSequence
	for _, event := range history {
		if err := writeSSEEvent(w, event); err != nil {
			return
		}
		flusher.Flush()
		if event.Sequence > lastSequence {
			lastSequence = event.Sequence
		}
	}

	keepAlive := time.NewTicker(15 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-keepAlive.C:
			writeString(w, ": keep-alive\n\n")
			flusher.Flush()
		case event, ok := <-live:
			if !ok {
				return
			}
			if event.Sequence <= lastSequence {
				continue
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			flusher.Flush()
			lastSequence = event.Sequence
		}
	}
}

func (h *Handler) flowStepMutate(w http.ResponseWriter, r *http.Request, mode string) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	steps := append([]domain.FlowStep(nil), flow.Steps...)
	switch mode {
	case "create":
		stepForm := flowStepFormFromHTTP(r)
		step, err := parseStepForm(stepForm, len(steps))
		if err != nil {
			h.renderStepFormError(w, r, workspace, flow, stepForm, err, false)
			return
		}
		steps = append(steps, step)
	case "update":
		stepForm := flowStepFormFromHTTP(r)
		step, err := parseStepForm(stepForm, 0)
		if err != nil {
			h.renderStepFormError(w, r, workspace, flow, stepForm, err, true)
			return
		}
		for i := range steps {
			if steps[i].ID == domain.FlowStepID(r.PathValue("stepId")) {
				step.OrderIndex = steps[i].OrderIndex
				step.CreatedAt = steps[i].CreatedAt
				step.UpdatedAt = time.Now().UTC()
				steps[i] = step
			}
		}
	case "delete":
		stepID := domain.FlowStepID(r.PathValue("stepId"))
		next := make([]domain.FlowStep, 0, len(steps))
		for _, step := range steps {
			if step.ID != stepID {
				next = append(next, step)
			}
		}
		steps = next
	}
	for i := range steps {
		steps[i].FlowID = flow.Flow.ID
		steps[i].OrderIndex = i
		if steps[i].UpdatedAt.IsZero() {
			steps[i].UpdatedAt = time.Now().UTC()
		}
		if steps[i].CreatedAt.IsZero() {
			steps[i].CreatedAt = time.Now().UTC()
		}
	}
	_, err = h.services.flowManagement.UpdateFlow(r.Context(), usecase.UpdateFlowCommand{WorkspaceID: workspace.ID, FlowID: flow.Flow.ID, Name: flow.Flow.Name, Description: flow.Flow.Description, Status: flow.Flow.Status, Steps: h.toStepDrafts(steps)})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	message := "Step saved."
	if mode == "delete" {
		message = "Step deleted."
	}
	if mode == "create" {
		message = "Step added."
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)), forms.Flash{Kind: "success", Message: message})
}

func (h *Handler) flowStepMove(w http.ResponseWriter, r *http.Request, delta int) {
	workspace, flow, err := h.loadFlow(r.Context(), r.PathValue("id"), r.PathValue("flowId"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	steps := append([]domain.FlowStep(nil), flow.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].OrderIndex < steps[j].OrderIndex })
	idx := -1
	stepID := domain.FlowStepID(r.PathValue("stepId"))
	for i, step := range steps {
		if step.ID == stepID {
			idx = i
			break
		}
	}
	if idx == -1 {
		h.renderError(w, r, &domain.NotFoundError{Entity: "flow step", ID: string(stepID)})
		return
	}
	next := idx + delta
	if next < 0 || next >= len(steps) {
		h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)), forms.Flash{Kind: "info", Message: "Step is already at the edge."})
		return
	}
	steps[idx], steps[next] = steps[next], steps[idx]
	for i := range steps {
		steps[i].OrderIndex = i
		steps[i].UpdatedAt = time.Now().UTC()
	}
	_, err = h.services.flowManagement.UpdateFlow(r.Context(), usecase.UpdateFlowCommand{WorkspaceID: workspace.ID, FlowID: flow.Flow.ID, Name: flow.Flow.Name, Description: flow.Flow.Description, Status: flow.Flow.Status, Steps: h.toStepDrafts(steps)})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)), forms.Flash{Kind: "success", Message: "Step order updated."})
}

func (h *Handler) variableMutate(w http.ResponseWriter, r *http.Request, replace string) {
	workspace, err := h.loadWorkspace(r.Context(), r.PathValue("id"))
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	form := forms.VariableForm{Name: r.FormValue("name"), Value: r.FormValue("value")}
	vars := make([]domain.WorkspaceVariable, 0, len(workspace.Variables)+1)
	for _, item := range workspace.Variables {
		if replace != "" && item.Name == replace {
			continue
		}
		if replace == "" && item.Name == form.Name {
			continue
		}
		vars = append(vars, item)
	}
	vars = append(vars, domain.WorkspaceVariable{Name: form.Name, Value: form.Value})
	_, err = h.services.workspaceManagement.UpdateWorkspaceVariables(r.Context(), usecase.UpdateWorkspaceVariablesCommand{WorkspaceID: workspace.ID, Variables: vars})
	if err != nil {
		h.renderError(w, r, err)
		return
	}
	h.redirectWithFlash(w, r, h.uiPath("/workspaces/"+string(workspace.ID)+"/variables"), forms.Flash{Kind: "success", Message: "Variable saved."})
}

func (h *Handler) renderPage(w http.ResponseWriter, status int, page string, data any) {
	h.renderer.render(w, status, page, data)
}
func (h *Handler) renderError(w http.ResponseWriter, r *http.Request, err error) {
	h.logger.Warn("ui request failed", zap.String("path", r.URL.Path), zap.Error(err))
	page := h.basePage(r, "UI error", "The requested operation could not be completed", nil, "")
	page.Flash = &forms.Flash{Kind: "error", Message: err.Error()}
	h.renderPage(w, http.StatusInternalServerError, "templates/pages/error.html", struct{ Page viewmodels.Page }{Page: page})
}
func (h *Handler) renderWorkspaceFormError(w http.ResponseWriter, r *http.Request, form forms.WorkspaceForm, err error) {
	page := h.basePage(r, "New workspace", "Create a new workspace", []viewmodels.Breadcrumb{{Label: "Workspaces", URL: h.uiPath("/workspaces")}, {Label: "New", URL: h.uiPath("/workspaces/new")}}, "")
	h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/workspace_form.html", viewmodels.WorkspaceFormPage{Page: withError(page, err), Form: form, SubmitURL: h.uiURL(r, h.uiPath("/workspaces")), SubmitLabel: "Create workspace"})
}
func (h *Handler) renderRequestFormError(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, form forms.RequestForm, err error, isEdit bool) {
	title := "New saved request"
	submitURL := h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/requests"))
	submitLabel := "Create request"
	if isEdit {
		title = "Edit saved request"
		submitURL = r.URL.Path
		submitLabel = "Save request"
	}
	page := h.workspacePage(r, workspace, title, r.URL.Path, nil)
	h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/request_form.html", viewmodels.SavedRequestFormPage{Page: withError(page, err), Workspace: workspaceItem(workspace), Form: form, SubmitURL: submitURL, SubmitLabel: submitLabel, IsEdit: isEdit})
}
func (h *Handler) renderFlowFormError(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, form forms.FlowForm, err error, isEdit bool, steps []viewmodels.FlowStepListItem) {
	title := "New flow"
	submitURL := h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows"))
	submitLabel := "Create flow"
	if isEdit {
		title = "Edit flow"
		submitURL = r.URL.Path
		submitLabel = "Save flow"
	}
	page := h.workspacePage(r, workspace, title, r.URL.Path, nil)
	h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/flow_form.html", viewmodels.FlowFormPage{Page: withError(page, err), Workspace: workspaceItem(workspace), Form: form, SubmitURL: submitURL, SubmitLabel: submitLabel, IsEdit: isEdit, Steps: steps})
}
func (h *Handler) renderStepFormError(w http.ResponseWriter, r *http.Request, workspace domain.Workspace, flow usecase.FlowDefinitionView, form forms.FlowStepForm, err error, isEdit bool) {
	_, requests, _ := h.loadWorkspaceAndRequests(r.Context(), string(workspace.ID))
	title := "New step"
	submitURL := h.uiURL(r, h.uiPath("/workspaces/"+string(workspace.ID)+"/flows/"+string(flow.Flow.ID)+"/steps"))
	submitLabel := "Add step"
	if isEdit {
		title = "Edit step"
		submitURL = r.URL.Path
		submitLabel = "Save step"
	}
	page := h.workspacePage(r, workspace, title, r.URL.Path, nil)
	h.renderPage(w, http.StatusUnprocessableEntity, "templates/pages/flow_step_form.html", viewmodels.FlowStepFormPage{Page: withError(page, err), Workspace: workspaceItem(workspace), Flow: flowItem(flow.Flow, len(flow.Steps)), Form: form, SubmitURL: submitURL, SubmitLabel: submitLabel, IsEdit: isEdit, SavedRequests: requests})
}

func (h *Handler) basePage(r *http.Request, title, description string, breadcrumbs []viewmodels.Breadcrumb, current string) viewmodels.Page {
	lang := h.lang(r)
	page := viewmodels.Page{
		Title:       title,
		Description: description,
		Lang:        lang,
		CurrentPath: r.URL.Path,
		Breadcrumbs: localizeBreadcrumbs(breadcrumbs, lang),
		PrimaryNav: []viewmodels.NavItem{
			{Label: h.text(r, "Home", "Главная"), URL: h.uiURL(r, h.uiPath("")), Active: r.URL.Path == h.uiPath("") || r.URL.Path == h.uiPath("/")},
			{Label: h.text(r, "Workspaces", "Воркспейсы"), URL: h.uiURL(r, h.uiPath("/workspaces")), Active: strings.Contains(r.URL.Path, "/workspaces")},
			{Label: h.text(r, "Docs", "Документация"), URL: h.uiURL(r, h.uiPath("/docs")), Active: strings.Contains(r.URL.Path, "/docs")},
		},
		LanguageOptions: []viewmodels.LanguageOption{
			{Code: "en", Label: "EN", URL: h.languageURL("en", r), Active: lang == "en"},
			{Code: "ru", Label: "RU", URL: h.languageURL("ru", r), Active: lang == "ru"},
		},
	}
	page.Flash = h.readFlash(r)
	return page
}
func (h *Handler) workspacePage(r *http.Request, workspace domain.Workspace, title, current string, extra []viewmodels.Breadcrumb) viewmodels.Page {
	crumbs := []viewmodels.Breadcrumb{{Label: h.text(r, "Workspaces", "Воркспейсы"), URL: h.uiPath("/workspaces")}, {Label: workspace.Name, URL: h.uiPath("/workspaces/" + string(workspace.ID))}}
	crumbs = append(crumbs, extra...)
	page := h.basePage(r, title, workspace.Description, crumbs, current)
	base := h.uiPath("/workspaces/" + string(workspace.ID))
	page.WorkspaceTabs = []viewmodels.WorkspaceTab{
		{Label: h.text(r, "Overview", "Обзор"), URL: h.uiURL(r, base), Active: r.URL.Path == base},
		{Label: h.text(r, "Requests", "Запросы"), URL: h.uiURL(r, base+"/requests"), Active: strings.Contains(r.URL.Path, "/requests")},
		{Label: h.text(r, "Flows", "Флоу"), URL: h.uiURL(r, base+"/flows"), Active: strings.Contains(r.URL.Path, "/flows")},
		{Label: h.text(r, "Variables", "Переменные"), URL: h.uiURL(r, base+"/variables"), Active: strings.Contains(r.URL.Path, "/variables")},
		{Label: h.text(r, "Secrets", "Секреты"), URL: h.uiURL(r, base+"/secrets"), Active: strings.Contains(r.URL.Path, "/secrets")},
		{Label: h.text(r, "Policy", "Политика"), URL: h.uiURL(r, base+"/policy"), Active: strings.Contains(r.URL.Path, "/policy")},
		{Label: h.text(r, "Runs", "Запуски"), URL: h.uiURL(r, base+"/runs"), Active: strings.Contains(r.URL.Path, "/runs")},
	}
	page.Flash = h.readFlash(r)
	return page
}
func withError(page viewmodels.Page, err error) viewmodels.Page {
	page.Flash = &forms.Flash{Kind: "error", Message: err.Error()}
	return page
}

func localizeBreadcrumbs(items []viewmodels.Breadcrumb, lang string) []viewmodels.Breadcrumb {
	if len(items) == 0 {
		return nil
	}
	localized := make([]viewmodels.Breadcrumb, len(items))
	for i, item := range items {
		localized[i] = item
		localized[i].URL = uiURL(lang, item.URL)
	}
	return localized
}

func (h *Handler) uiPath(suffix string) string {
	prefix := h.cfg.UI.Prefix
	if prefix == "" {
		prefix = "/ui"
	}
	if suffix == "" {
		return prefix
	}
	return path.Clean(prefix + "/" + strings.TrimPrefix(suffix, "/"))
}
func (h *Handler) uiURL(r *http.Request, raw string) string {
	return applyLangQuery(stripLangQuery(raw), h.lang(r))
}

func (h *Handler) lang(r *http.Request) string {
	if query := normalizeLang(r.URL.Query().Get("lang")); query != "" {
		return query
	}
	if cookie, err := r.Cookie(langCookieName); err == nil {
		return normalizeLang(cookie.Value)
	}
	return "en"
}
func normalizeLang(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "ru":
		return "ru"
	default:
		return "en"
	}
}
func (h *Handler) text(r *http.Request, en, ru string) string {
	if h.lang(r) == "ru" {
		return ru
	}
	return en
}
func (h *Handler) languageURL(lang string, r *http.Request) string {
	return h.uiPath("/language") + "?lang=" + url.QueryEscape(lang) + "&next=" + url.QueryEscape(stripLangQuery(r.URL.RequestURI()))
}

func stripLangQuery(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return raw
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	query.Del("lang")
	parsed.RawQuery = query.Encode()
	return parsed.RequestURI()
}

func applyLangQuery(raw, lang string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	query.Set("lang", normalizeLang(lang))
	parsed.RawQuery = query.Encode()
	return parsed.RequestURI()
}
func (h *Handler) redirectWithFlash(w http.ResponseWriter, r *http.Request, location string, flash forms.Flash) {
	payload := base64.RawURLEncoding.EncodeToString([]byte(flash.Kind + "|" + flash.Message))
	http.SetCookie(w, &http.Cookie{Name: flashCookieName, Value: payload, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, h.uiURL(r, location), http.StatusSeeOther)
}
func (h *Handler) readFlash(r *http.Request) *forms.Flash {
	cookie, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 {
		return nil
	}
	return &forms.Flash{Kind: parts[0], Message: parts[1]}
}

func (h *Handler) loadWorkspace(ctx context.Context, id string) (domain.Workspace, error) {
	view, err := h.services.workspaceManagement.GetWorkspace(ctx, usecase.GetWorkspaceQuery{WorkspaceID: domain.WorkspaceID(id)})
	if err != nil {
		return domain.Workspace{}, err
	}
	return view.Workspace, nil
}
func (h *Handler) loadWorkspaceAndRequests(ctx context.Context, id string) (domain.Workspace, []viewmodels.SavedRequestListItem, error) {
	workspace, err := h.loadWorkspace(ctx, id)
	if err != nil {
		return domain.Workspace{}, nil, err
	}
	requests, err := h.services.savedRequestManagement.ListSavedRequests(ctx, usecase.ListSavedRequestsQuery{WorkspaceID: workspace.ID})
	if err != nil {
		return domain.Workspace{}, nil, err
	}
	items := make([]viewmodels.SavedRequestListItem, 0, len(requests))
	for _, item := range requests {
		items = append(items, savedRequestItem(item))
	}
	return workspace, items, nil
}
func (h *Handler) loadSavedRequest(ctx context.Context, workspaceID, requestID string) (domain.Workspace, domain.SavedRequest, error) {
	workspace, err := h.loadWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, domain.SavedRequest{}, err
	}
	view, err := h.services.savedRequestManagement.GetSavedRequest(ctx, usecase.GetSavedRequestQuery{WorkspaceID: workspace.ID, SavedRequestID: domain.SavedRequestID(requestID)})
	if err != nil {
		return domain.Workspace{}, domain.SavedRequest{}, err
	}
	return workspace, view.SavedRequest, nil
}
func (h *Handler) loadFlow(ctx context.Context, workspaceID, flowID string) (domain.Workspace, usecase.FlowDefinitionView, error) {
	workspace, err := h.loadWorkspace(ctx, workspaceID)
	if err != nil {
		return domain.Workspace{}, usecase.FlowDefinitionView{}, err
	}
	view, err := h.services.flowManagement.GetFlow(ctx, usecase.GetFlowQuery{WorkspaceID: workspace.ID, FlowID: domain.FlowID(flowID)})
	if err != nil {
		return domain.Workspace{}, usecase.FlowDefinitionView{}, err
	}
	return workspace, view, nil
}
func (h *Handler) parseRunLaunchForm(r *http.Request) (struct {
	InitiatedBy    string
	InputJSON      json.RawMessage
	Queue          string
	IdempotencyKey string
}, error) {
	input := strings.TrimSpace(r.FormValue("input_json"))
	if input == "" {
		input = "{}"
	}
	if !json.Valid([]byte(input)) {
		return struct {
			InitiatedBy    string
			InputJSON      json.RawMessage
			Queue          string
			IdempotencyKey string
		}{}, fmt.Errorf("input_json must be valid JSON")
	}
	return struct {
		InitiatedBy    string
		InputJSON      json.RawMessage
		Queue          string
		IdempotencyKey string
	}{InitiatedBy: strings.TrimSpace(r.FormValue("initiated_by")), InputJSON: json.RawMessage([]byte(input)), Queue: strings.TrimSpace(r.FormValue("queue")), IdempotencyKey: strings.TrimSpace(r.FormValue("idempotency_key"))}, nil
}
func (h *Handler) requestCommandFromForm(workspaceID domain.WorkspaceID, form forms.RequestForm, create bool) (usecase.CreateSavedRequestCommand, error) {
	spec, err := parseRequestForm(form)
	if err != nil {
		return usecase.CreateSavedRequestCommand{}, err
	}
	return usecase.CreateSavedRequestCommand{WorkspaceID: workspaceID, SavedRequestID: domain.SavedRequestID(form.ID), Name: form.Name, Description: form.Description, RequestSpec: spec}, nil
}
func (h *Handler) requestUpdateCommandFromForm(workspaceID domain.WorkspaceID, requestID domain.SavedRequestID, form forms.RequestForm) (usecase.UpdateSavedRequestCommand, error) {
	spec, err := parseRequestForm(form)
	if err != nil {
		return usecase.UpdateSavedRequestCommand{}, err
	}
	return usecase.UpdateSavedRequestCommand{WorkspaceID: workspaceID, SavedRequestID: requestID, Name: form.Name, Description: form.Description, RequestSpec: spec}, nil
}
func (h *Handler) toStepDrafts(steps []domain.FlowStep) []usecase.FlowStepDraft {
	result := make([]usecase.FlowStepDraft, 0, len(steps))
	for _, step := range steps {
		result = append(result, usecase.FlowStepDraft{ID: step.ID, OrderIndex: step.OrderIndex, Name: step.Name, StepType: step.Type(), SavedRequestID: step.SavedRequestID, RequestSpec: step.RequestSpec, RequestSpecOverride: step.RequestSpecOverride, ExtractionSpec: step.ExtractionSpec, AssertionSpec: step.AssertionSpec})
	}
	return result
}
func workspaceItem(ws domain.Workspace) viewmodels.WorkspaceListItem {
	return viewmodels.WorkspaceListItem{ID: string(ws.ID), Name: ws.Name, Slug: ws.Slug, Description: ws.Description, OwnerTeam: ws.OwnerTeam, Status: string(ws.Status), UpdatedAt: formatTime(ws.UpdatedAt)}
}
func savedRequestItem(req domain.SavedRequest) viewmodels.SavedRequestListItem {
	return viewmodels.SavedRequestListItem{ID: string(req.ID), Name: req.Name, Description: req.Description, Method: req.RequestSpec.Method, URL: req.RequestSpec.URLTemplate, Timeout: req.RequestSpec.Timeout.String(), UpdatedAt: formatTime(req.UpdatedAt)}
}
func flowItem(flow domain.Flow, stepCount int) viewmodels.FlowListItem {
	return viewmodels.FlowListItem{ID: string(flow.ID), Name: flow.Name, Description: flow.Description, Status: string(flow.Status), Version: flow.Version, StepCount: stepCount, UpdatedAt: formatTime(flow.UpdatedAt)}
}
func flowStepItems(steps []domain.FlowStep) []viewmodels.FlowStepListItem {
	sort.Slice(steps, func(i, j int) bool { return steps[i].OrderIndex < steps[j].OrderIndex })
	out := make([]viewmodels.FlowStepListItem, 0, len(steps))
	for _, step := range steps {
		summary := step.RequestSpec.Method + " " + step.RequestSpec.URLTemplate
		if step.Type() == domain.FlowStepTypeSavedRequestRef {
			summary = "saved_request_id=" + string(step.SavedRequestID)
		}
		out = append(out, viewmodels.FlowStepListItem{ID: string(step.ID), OrderIndex: step.OrderIndex, Name: step.Name, StepType: string(step.Type()), SavedRequestID: string(step.SavedRequestID), Summary: summary})
	}
	return out
}
func suggestStepIdentity(spec domain.RequestSpec, order int) (string, string) {
	method := strings.ToLower(strings.TrimSpace(spec.Method))
	if method == "" {
		method = "step"
	}
	label := method
	if parsed, err := url.Parse(spec.URLTemplate); err == nil {
		pathPart := strings.Trim(parsed.Path, "/")
		if pathPart != "" {
			pathPart = strings.ReplaceAll(pathPart, "/", "-")
			label = method + "-" + pathPart
		} else if host := strings.TrimSpace(parsed.Hostname()); host != "" {
			label = method + "-" + strings.ReplaceAll(host, ".", "-")
		}
	}
	label = strings.Trim(label, "-")
	if label == "" {
		label = fmt.Sprintf("step-%d", order+1)
	}
	return label, strings.ReplaceAll(label, "-", " ")
}
func requestFormFromHTTP(r *http.Request) forms.RequestForm {
	return forms.RequestForm{ID: r.FormValue("id"), Name: r.FormValue("name"), Description: r.FormValue("description"), Method: r.FormValue("method"), URL: r.FormValue("url"), HeadersText: r.FormValue("headers"), QueryText: r.FormValue("query"), Body: r.FormValue("body"), Timeout: r.FormValue("timeout"), FollowRedirects: r.FormValue("follow_redirects") != "", RetryEnabled: r.FormValue("retry_enabled") != "", RetryMaxAttempts: r.FormValue("retry_max_attempts"), RetryBackoffStrategy: r.FormValue("retry_backoff_strategy"), RetryInitialInterval: r.FormValue("retry_initial_interval"), RetryMaxInterval: r.FormValue("retry_max_interval"), RetryStatusCodes: r.FormValue("retry_status_codes")}
}
func flowStepFormFromHTTP(r *http.Request) forms.FlowStepForm {
	_ = r.ParseForm()
	return forms.FlowStepForm{
		ID:                       r.FormValue("id"),
		Name:                     r.FormValue("name"),
		StepType:                 r.FormValue("step_type"),
		SavedRequestID:           r.FormValue("saved_request_id"),
		InlineRequest:            requestFormFromHTTP(r),
		OverrideHeadersText:      r.FormValue("override_headers"),
		OverrideQueryText:        r.FormValue("override_query"),
		OverrideBody:             r.FormValue("override_body"),
		OverrideTimeout:          r.FormValue("override_timeout"),
		OverrideRetryEnabled:     r.FormValue("override_retry_enabled") != "",
		OverrideRetryMaxAttempts: r.FormValue("override_retry_max_attempts"),
		OverrideRetryBackoff:     r.FormValue("override_retry_backoff"),
		OverrideRetryInitial:     r.FormValue("override_retry_initial"),
		OverrideRetryMax:         r.FormValue("override_retry_max"),
		OverrideRetryStatusCodes: r.FormValue("override_retry_status_codes"),
		ExtractionRules:          extractionRuleFormsFromRequest(r),
		ExtractionRulesText:      r.FormValue("extraction_rules"),
		AssertionRulesText:       r.FormValue("assertion_rules"),
	}
}
func formFromRequest(req domain.SavedRequest) forms.RequestForm {
	return forms.RequestForm{ID: string(req.ID), Name: req.Name, Description: req.Description, Method: req.RequestSpec.Method, URL: req.RequestSpec.URLTemplate, HeadersText: kvText(req.RequestSpec.Headers), QueryText: kvText(req.RequestSpec.Query), Body: req.RequestSpec.BodyTemplate, Timeout: req.RequestSpec.Timeout.String(), FollowRedirects: req.RequestSpec.FollowRedirects, RetryEnabled: req.RequestSpec.RetryPolicy.Enabled, RetryMaxAttempts: strconv.Itoa(req.RequestSpec.RetryPolicy.MaxAttempts), RetryBackoffStrategy: string(req.RequestSpec.RetryPolicy.BackoffStrategy), RetryInitialInterval: req.RequestSpec.RetryPolicy.InitialInterval.String(), RetryMaxInterval: req.RequestSpec.RetryPolicy.MaxInterval.String(), RetryStatusCodes: intListText(req.RequestSpec.RetryPolicy.RetryableStatusCodes)}
}
func (h *Handler) previewStepFormFromCurl(spec domain.RequestSpec, order int) forms.FlowStepForm {
	id, name := suggestStepIdentity(spec, order)
	return forms.FlowStepForm{
		ID:              id,
		Name:            name,
		StepType:        string(domain.FlowStepTypeInlineRequest),
		ExtractionRules: defaultExtractionRuleForms(nil),
		InlineRequest: forms.RequestForm{
			Name:                 name,
			Method:               spec.Method,
			URL:                  spec.URLTemplate,
			HeadersText:          kvText(spec.Headers),
			QueryText:            kvText(spec.Query),
			Body:                 spec.BodyTemplate,
			Timeout:              spec.Timeout.String(),
			FollowRedirects:      spec.FollowRedirects,
			RetryEnabled:         spec.RetryPolicy.Enabled,
			RetryMaxAttempts:     strconv.Itoa(spec.RetryPolicy.MaxAttempts),
			RetryBackoffStrategy: string(spec.RetryPolicy.BackoffStrategy),
			RetryInitialInterval: spec.RetryPolicy.InitialInterval.String(),
			RetryMaxInterval:     spec.RetryPolicy.MaxInterval.String(),
			RetryStatusCodes:     intListText(spec.RetryPolicy.RetryableStatusCodes),
		},
	}
}
func formFromStep(step domain.FlowStep) forms.FlowStepForm {
	form := forms.FlowStepForm{
		ID:                  string(step.ID),
		Name:                step.Name,
		StepType:            string(step.Type()),
		SavedRequestID:      string(step.SavedRequestID),
		InlineRequest:       formFromRequest(domain.SavedRequest{RequestSpec: step.RequestSpec}),
		ExtractionRules:     defaultExtractionRuleForms(step.ExtractionSpec.Rules),
		ExtractionRulesText: extractionRulesText(step.ExtractionSpec.Rules),
		AssertionRulesText:  assertionRulesText(step.AssertionSpec.Rules),
	}
	form.OverrideHeadersText = kvText(step.RequestSpecOverride.Headers)
	form.OverrideQueryText = kvText(step.RequestSpecOverride.Query)
	if step.RequestSpecOverride.BodyTemplate != nil {
		form.OverrideBody = *step.RequestSpecOverride.BodyTemplate
	}
	if step.RequestSpecOverride.Timeout != nil {
		form.OverrideTimeout = step.RequestSpecOverride.Timeout.String()
	}
	if step.RequestSpecOverride.RetryPolicy != nil {
		form.OverrideRetryEnabled = step.RequestSpecOverride.RetryPolicy.Enabled
		form.OverrideRetryMaxAttempts = strconv.Itoa(step.RequestSpecOverride.RetryPolicy.MaxAttempts)
		form.OverrideRetryBackoff = string(step.RequestSpecOverride.RetryPolicy.BackoffStrategy)
		form.OverrideRetryInitial = step.RequestSpecOverride.RetryPolicy.InitialInterval.String()
		form.OverrideRetryMax = step.RequestSpecOverride.RetryPolicy.MaxInterval.String()
		form.OverrideRetryStatusCodes = intListText(step.RequestSpecOverride.RetryPolicy.RetryableStatusCodes)
	}
	return form
}
func policyForm(policy domain.WorkspacePolicy) forms.PolicyForm {
	return forms.PolicyForm{AllowedHosts: strings.Join(policy.AllowedHosts, "\n"), MaxSavedRequests: strconv.Itoa(policy.MaxSavedRequests), MaxFlows: strconv.Itoa(policy.MaxFlows), MaxStepsPerFlow: strconv.Itoa(policy.MaxStepsPerFlow), MaxRequestBodyBytes: strconv.Itoa(policy.MaxRequestBodyBytes), DefaultTimeoutMS: strconv.Itoa(policy.DefaultTimeoutMS), MaxRunDurationSeconds: strconv.Itoa(policy.MaxRunDurationSeconds), RetryEnabled: policy.DefaultRetryPolicy.Enabled, RetryMaxAttempts: strconv.Itoa(policy.DefaultRetryPolicy.MaxAttempts), RetryBackoffStrategy: string(policy.DefaultRetryPolicy.BackoffStrategy), RetryInitialInterval: policy.DefaultRetryPolicy.InitialInterval.String(), RetryMaxInterval: policy.DefaultRetryPolicy.MaxInterval.String(), RetryStatusCodes: intListText(policy.DefaultRetryPolicy.RetryableStatusCodes)}
}
func defaultPolicy(hosts []string) domain.WorkspacePolicy {
	return domain.WorkspacePolicy{AllowedHosts: hosts, MaxSavedRequests: 100, MaxFlows: 100, MaxStepsPerFlow: 25, MaxRequestBodyBytes: 1 << 20, DefaultTimeoutMS: 10000, MaxRunDurationSeconds: 600, DefaultRetryPolicy: domain.RetryPolicy{Enabled: false}}
}
func parseRequestForm(form forms.RequestForm) (domain.RequestSpec, error) {
	timeout, err := parseDurationOrDefault(form.Timeout, 10*time.Second)
	if err != nil {
		return domain.RequestSpec{}, err
	}
	retry, err := parseRetryPolicy(form.RetryEnabled, form.RetryMaxAttempts, form.RetryBackoffStrategy, form.RetryInitialInterval, form.RetryMaxInterval, form.RetryStatusCodes)
	if err != nil {
		return domain.RequestSpec{}, err
	}
	return domain.RequestSpec{Method: strings.ToUpper(strings.TrimSpace(form.Method)), URLTemplate: strings.TrimSpace(form.URL), Headers: parseKVLines(form.HeadersText), Query: parseKVLines(form.QueryText), BodyTemplate: form.Body, Timeout: timeout, FollowRedirects: form.FollowRedirects, RetryPolicy: retry}, nil
}
func parseStepForm(form forms.FlowStepForm, order int) (domain.FlowStep, error) {
	step := domain.FlowStep{ID: domain.FlowStepID(strings.TrimSpace(form.ID)), Name: strings.TrimSpace(form.Name), OrderIndex: order, StepType: domain.FlowStepType(strings.TrimSpace(form.StepType)), SavedRequestID: domain.SavedRequestID(strings.TrimSpace(form.SavedRequestID))}
	extraction, err := parseExtractionRuleForms(form.ExtractionRules, form.ExtractionRulesText)
	if err != nil {
		return domain.FlowStep{}, err
	}
	assertions, err := parseAssertionRules(form.AssertionRulesText)
	if err != nil {
		return domain.FlowStep{}, err
	}
	step.ExtractionSpec = extraction
	step.AssertionSpec = assertions
	if step.Type() == domain.FlowStepTypeInlineRequest {
		spec, err := parseRequestForm(form.InlineRequest)
		if err != nil {
			return domain.FlowStep{}, err
		}
		step.RequestSpec = spec
	} else {
		override, err := parseRequestOverride(form)
		if err != nil {
			return domain.FlowStep{}, err
		}
		step.RequestSpecOverride = override
	}
	return step, nil
}
func parseRequestOverride(form forms.FlowStepForm) (domain.RequestSpecOverride, error) {
	timeout, hasTimeout, err := parseOptionalDuration(form.OverrideTimeout)
	if err != nil {
		return domain.RequestSpecOverride{}, err
	}
	retry, err := parseRetryPolicy(form.OverrideRetryEnabled, form.OverrideRetryMaxAttempts, form.OverrideRetryBackoff, form.OverrideRetryInitial, form.OverrideRetryMax, form.OverrideRetryStatusCodes)
	if err != nil {
		return domain.RequestSpecOverride{}, err
	}
	var retryPtr *domain.RetryPolicy
	if form.OverrideRetryEnabled || strings.TrimSpace(form.OverrideRetryMaxAttempts) != "" || strings.TrimSpace(form.OverrideRetryBackoff) != "" {
		retryPtr = &retry
	}
	var bodyPtr *string
	if strings.TrimSpace(form.OverrideBody) != "" {
		body := form.OverrideBody
		bodyPtr = &body
	}
	var timeoutPtr *time.Duration
	if hasTimeout {
		timeoutPtr = &timeout
	}
	return domain.RequestSpecOverride{Headers: parseKVLines(form.OverrideHeadersText), Query: parseKVLines(form.OverrideQueryText), BodyTemplate: bodyPtr, Timeout: timeoutPtr, RetryPolicy: retryPtr}, nil
}
func parseRetryPolicy(enabled bool, maxAttempts, backoff, initial, max, statusCodes string) (domain.RetryPolicy, error) {
	p := domain.RetryPolicy{Enabled: enabled, BackoffStrategy: domain.BackoffStrategy(strings.TrimSpace(backoff))}
	if strings.TrimSpace(maxAttempts) != "" {
		n, err := strconv.Atoi(strings.TrimSpace(maxAttempts))
		if err != nil {
			return p, fmt.Errorf("max attempts must be an integer")
		}
		p.MaxAttempts = n
	}
	if strings.TrimSpace(initial) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(initial))
		if err != nil {
			return p, fmt.Errorf("initial interval must be a duration")
		}
		p.InitialInterval = d
	}
	if strings.TrimSpace(max) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(max))
		if err != nil {
			return p, fmt.Errorf("max interval must be a duration")
		}
		p.MaxInterval = d
	}
	if strings.TrimSpace(statusCodes) != "" {
		for _, part := range strings.Split(statusCodes, ",") {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil {
				return p, fmt.Errorf("retry status codes must be comma-separated integers")
			}
			p.RetryableStatusCodes = append(p.RetryableStatusCodes, n)
		}
	}
	return p, nil
}
func parseDurationOrDefault(value string, fallback time.Duration) (time.Duration, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.ParseDuration(strings.TrimSpace(value))
}
func parseOptionalDuration(value string) (time.Duration, bool, error) {
	if strings.TrimSpace(value) == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(strings.TrimSpace(value))
	return d, true, err
}
func parseKVLines(value string) map[string]string {
	result := map[string]string{}
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 1 {
			parts = strings.SplitN(line, "=", 2)
		}
		if len(parts) != 2 {
			continue
		}
		result[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
func parseExtractionRules(text string) (domain.ExtractionSpec, error) {
	spec := domain.ExtractionSpec{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 2 {
			return spec, fmt.Errorf("extraction rules use format name|source|selector|required|json_encode")
		}
		rule := domain.ExtractionRule{Name: strings.TrimSpace(parts[0]), Source: domain.ExtractionSource(strings.TrimSpace(parts[1]))}
		if len(parts) > 2 {
			rule.Selector = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			rule.Required = strings.EqualFold(strings.TrimSpace(parts[3]), "true")
		}
		if len(parts) > 4 {
			rule.JSONEncode = strings.EqualFold(strings.TrimSpace(parts[4]), "true")
		}
		spec.Rules = append(spec.Rules, rule)
	}
	return spec, nil
}

func parseExtractionRuleForms(rows []forms.ExtractionRuleForm, fallback string) (domain.ExtractionSpec, error) {
	hasStructuredInput := false
	spec := domain.ExtractionSpec{}
	for _, row := range rows {
		if strings.TrimSpace(row.Name) == "" && strings.TrimSpace(row.Selector) == "" {
			continue
		}
		hasStructuredInput = true
		spec.Rules = append(spec.Rules, domain.ExtractionRule{
			Name:       strings.TrimSpace(row.Name),
			Source:     domain.ExtractionSource(strings.TrimSpace(row.Source)),
			Selector:   strings.TrimSpace(row.Selector),
			Required:   strings.EqualFold(strings.TrimSpace(row.Required), "true"),
			JSONEncode: strings.EqualFold(strings.TrimSpace(row.JSONEncode), "true"),
		})
	}
	if hasStructuredInput {
		return spec, nil
	}
	return parseExtractionRules(fallback)
}

func parseAssertionRules(text string) (domain.AssertionSpec, error) {
	spec := domain.AssertionSpec{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "|")
		if len(parts) < 3 {
			return spec, fmt.Errorf("assertion rules use format name|target|operator|selector|expected|expected_list")
		}
		rule := domain.AssertionRule{Name: strings.TrimSpace(parts[0]), Target: domain.AssertionTarget(strings.TrimSpace(parts[1])), Operator: domain.AssertionOperator(strings.TrimSpace(parts[2]))}
		if len(parts) > 3 {
			rule.Selector = strings.TrimSpace(parts[3])
		}
		if len(parts) > 4 {
			rule.ExpectedValue = strings.TrimSpace(parts[4])
		}
		if len(parts) > 5 && strings.TrimSpace(parts[5]) != "" {
			for _, item := range strings.Split(parts[5], ",") {
				rule.ExpectedValues = append(rule.ExpectedValues, strings.TrimSpace(item))
			}
		}
		spec.Rules = append(spec.Rules, rule)
	}
	return spec, nil
}
func parsePolicyForm(r *http.Request) (domain.WorkspacePolicy, error) {
	atoi := func(name string) (int, error) { return strconv.Atoi(strings.TrimSpace(r.FormValue(name))) }
	maxSaved, err := atoi("max_saved_requests")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	maxFlows, err := atoi("max_flows")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	maxSteps, err := atoi("max_steps_per_flow")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	maxBody, err := atoi("max_request_body_bytes")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	timeoutMS, err := atoi("default_timeout_ms")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	maxDuration, err := atoi("max_run_duration_seconds")
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	retry, err := parseRetryPolicy(r.FormValue("retry_enabled") != "", r.FormValue("retry_max_attempts"), r.FormValue("retry_backoff_strategy"), r.FormValue("retry_initial_interval"), r.FormValue("retry_max_interval"), r.FormValue("retry_status_codes"))
	if err != nil {
		return domain.WorkspacePolicy{}, err
	}
	return domain.WorkspacePolicy{AllowedHosts: normalizeAllowedHostEntries(splitLines(r.FormValue("allowed_hosts"))), MaxSavedRequests: maxSaved, MaxFlows: maxFlows, MaxStepsPerFlow: maxSteps, MaxRequestBodyBytes: maxBody, DefaultTimeoutMS: timeoutMS, MaxRunDurationSeconds: maxDuration, DefaultRetryPolicy: retry}, nil
}
func splitLines(value string) []string {
	out := []string{}
	for _, line := range strings.Split(value, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func extractionRuleFormsFromRequest(r *http.Request) []forms.ExtractionRuleForm {
	names := r.Form["extraction_name"]
	sources := r.Form["extraction_source"]
	selectors := r.Form["extraction_selector"]
	required := r.Form["extraction_required"]
	jsonEncode := r.Form["extraction_json_encode"]
	size := maxInt(len(names), len(sources), len(selectors), len(required), len(jsonEncode))
	if size == 0 {
		return defaultExtractionRuleForms(nil)
	}
	rows := make([]forms.ExtractionRuleForm, 0, size)
	for idx := 0; idx < size; idx++ {
		rows = append(rows, forms.ExtractionRuleForm{
			Name:       valueAt(names, idx),
			Source:     valueAt(sources, idx),
			Selector:   valueAt(selectors, idx),
			Required:   defaultIfEmpty(valueAt(required, idx), "true"),
			JSONEncode: defaultIfEmpty(valueAt(jsonEncode, idx), "false"),
		})
	}
	return defaultExtractionRuleFormsFromRows(rows)
}

func defaultExtractionRuleForms(rules []domain.ExtractionRule) []forms.ExtractionRuleForm {
	rows := make([]forms.ExtractionRuleForm, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, forms.ExtractionRuleForm{
			Name:       rule.Name,
			Source:     string(rule.Source),
			Selector:   rule.Selector,
			Required:   boolString(rule.Required),
			JSONEncode: boolString(rule.JSONEncode),
		})
	}
	return defaultExtractionRuleFormsFromRows(rows)
}

func defaultExtractionRuleFormsFromRows(rows []forms.ExtractionRuleForm) []forms.ExtractionRuleForm {
	if len(rows) == 0 {
		return []forms.ExtractionRuleForm{{Source: string(domain.ExtractionSourceBody), Required: "true", JSONEncode: "false"}}
	}
	return rows
}

func boolString(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func valueAt(values []string, idx int) string {
	if idx < 0 || idx >= len(values) {
		return ""
	}
	return values[idx]
}

func defaultIfEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func maxInt(values ...int) int {
	max := 0
	for _, value := range values {
		if value > max {
			max = value
		}
	}
	return max
}

func normalizeAllowedHostEntries(hosts []string) []string {
	if len(hosts) == 0 {
		return nil
	}
	out := make([]string, 0, len(hosts))
	for _, host := range hosts {
		normalized := strings.ToLower(strings.TrimSpace(host))
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "://") {
			if parsed, err := url.Parse(normalized); err == nil && parsed.Hostname() != "" {
				normalized = parsed.Hostname()
			}
		} else if strings.Contains(normalized, "/") {
			normalized = strings.SplitN(normalized, "/", 2)[0]
		}
		if parsedHost, port, err := net.SplitHostPort(normalized); err == nil && parsedHost != "" && port != "" {
			normalized = parsedHost
		}
		out = append(out, normalized)
	}
	return out
}

func kvText(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+": "+values[k])
	}
	return strings.Join(lines, "\n")
}
func intListText(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.Itoa(v)
	}
	return strings.Join(parts, ",")
}
func extractionRulesText(rules []domain.ExtractionRule) string {
	lines := make([]string, 0, len(rules))
	for _, rule := range rules {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%t|%t", rule.Name, rule.Source, rule.Selector, rule.Required, rule.JSONEncode))
	}
	return strings.Join(lines, "\n")
}
func assertionRulesText(rules []domain.AssertionRule) string {
	lines := make([]string, 0, len(rules))
	for _, rule := range rules {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%s|%s|%s", rule.Name, rule.Target, rule.Operator, rule.Selector, rule.ExpectedValue, strings.Join(rule.ExpectedValues, ",")))
	}
	return strings.Join(lines, "\n")
}
func requestSpecSummary(spec domain.RequestSpec) map[string]string {
	return map[string]string{"Method": spec.Method, "URL": spec.URLTemplate, "Headers": kvText(spec.Headers), "Query": kvText(spec.Query), "Timeout": spec.Timeout.String(), "Retry": fmt.Sprintf("enabled=%t attempts=%d", spec.RetryPolicy.Enabled, spec.RetryPolicy.MaxAttempts)}
}
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "{}"
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	value = redactSecrets(value)
	out, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(out)
}
func redactSecrets(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range v {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "token") || strings.Contains(lower, "password") || lower == "authorization" {
				out[key] = "***"
			} else {
				out[key] = redactSecrets(item)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = redactSecrets(item)
		}
		return out
	default:
		return value
	}
}
func durationString(started, finished *time.Time) string {
	if started == nil {
		return "—"
	}
	end := time.Now().UTC()
	if finished != nil {
		end = *finished
	}
	return end.Sub(*started).Round(time.Millisecond).String()
}
func findStep(steps []domain.FlowStep, id domain.FlowStepID) (domain.FlowStep, bool) {
	for _, step := range steps {
		if step.ID == id {
			return step, true
		}
	}
	return domain.FlowStep{}, false
}
func (h *Handler) runItem(ctx context.Context, run domain.FlowRun) viewmodels.RunListItem {
	targetName := string(run.FlowID)
	if run.IsSavedRequestRun() {
		targetName = string(run.SavedRequestID)
	}
	if run.IsFlowRun() {
		if flow, err := h.services.flowManagement.GetFlow(ctx, usecase.GetFlowQuery{WorkspaceID: run.WorkspaceID, FlowID: run.FlowID}); err == nil {
			targetName = flow.Flow.Name
		}
	} else if run.IsSavedRequestRun() {
		if req, err := h.services.savedRequestManagement.GetSavedRequest(ctx, usecase.GetSavedRequestQuery{WorkspaceID: run.WorkspaceID, SavedRequestID: run.SavedRequestID}); err == nil {
			targetName = req.SavedRequest.Name
		}
	}
	return viewmodels.RunListItem{ID: string(run.ID), Status: string(run.Status), TargetType: string(run.Target()), TargetName: targetName, StartedAt: formatTimePtr(run.StartedAt), FinishedAt: formatTimePtr(run.FinishedAt), Duration: durationString(run.StartedAt, run.FinishedAt), InitiatedBy: run.InitiatedBy}
}

func mapRunEvent(event domain.RunEvent) viewmodels.RunEventItem {
	stepLabel := "run"
	if event.StepName != "" {
		stepLabel = event.StepName
		if event.StepOrder != nil {
			stepLabel = fmt.Sprintf("#%d %s", *event.StepOrder, event.StepName)
		}
	}
	details := prettyJSON(event.DetailsJSON)
	return viewmodels.RunEventItem{
		Sequence:   event.Sequence,
		Timestamp:  formatTime(event.CreatedAt),
		Level:      string(event.Level),
		EventType:  string(event.EventType),
		StepLabel:  stepLabel,
		Message:    event.Message,
		Details:    details,
		HasDetails: len(strings.TrimSpace(details)) > 2,
	}
}

func parseAfterSequence(r *http.Request) int64 {
	if header := strings.TrimSpace(r.Header.Get("Last-Event-ID")); header != "" {
		if parsed, err := strconv.ParseInt(header, 10, 64); err == nil && parsed >= 0 {
			return parsed
		}
	}
	if query := strings.TrimSpace(r.URL.Query().Get("after")); query != "" {
		if parsed, err := strconv.ParseInt(query, 10, 64); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return 0
}

func writeSSEEvent(w http.ResponseWriter, event domain.RunEvent) error {
	payload, err := json.Marshal(mapRunEvent(event))
	if err != nil {
		return err
	}
	writeString(w, "id: "+strconv.FormatInt(event.Sequence, 10)+"\n")
	writeString(w, "event: run_event\n")
	writeString(w, "data: "+string(payload)+"\n\n")
	return nil
}
