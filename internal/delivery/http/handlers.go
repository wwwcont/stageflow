package http

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

type services struct {
	workspaceManagement    usecase.WorkspaceManagementUseCase
	savedRequestManagement usecase.SavedRequestManagementUseCase
	flowManagement         usecase.FlowManagementUseCase
	runService             usecase.RunService
	runEvents              usecase.RunEventUseCase
	curlImport             usecase.CurlImportUseCase
}

type handler struct {
	services services
}

func newHandler(s services) *handler { return &handler{services: s} }

func (h *handler) createFlow(w http.ResponseWriter, r *http.Request) {
	var request flowRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Name) == "" {
		writeValidationError(w, r, "id and name are required", nil)
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("id"))
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(request.WorkspaceID)
	}
	if workspaceID == "" {
		writeValidationError(w, r, "workspace_id is required", nil)
		return
	}
	request.WorkspaceID = workspaceID
	command, err := toCreateFlowCommand(request)
	if err != nil {
		writeValidationError(w, r, "invalid flow payload", err.Error())
		return
	}
	view, err := h.services.flowManagement.CreateFlow(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapFlowDefinition(view))
}

func (h *handler) listFlows(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePagination(r)
	if err != nil {
		writeValidationError(w, r, "invalid pagination query", err.Error())
		return
	}
	flows, err := h.services.flowManagement.ListFlows(r.Context(), usecase.ListFlowsQuery{
		WorkspaceID: domain.WorkspaceID(firstNonEmpty(strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.URL.Query().Get("workspace_id")))),
		Statuses:    parseFlowStatuses(r.URL.Query().Get("status")),
		NameLike:    strings.TrimSpace(r.URL.Query().Get("name")),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]flowResponse, 0, len(flows))
	for _, flow := range flows {
		items = append(items, mapFlow(flow))
	}
	writeJSON(w, http.StatusOK, listResponse[flowResponse]{Items: items})
}

func (h *handler) getFlow(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(firstNonEmpty(strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(r.URL.Query().Get("workspace_id"))))
	if workspaceID == "" {
		writeValidationError(w, r, "workspace_id query parameter is required", nil)
		return
	}
	version, err := parseOptionalVersion(r.URL.Query().Get("version"))
	if err != nil {
		writeValidationError(w, r, "version must be a positive integer", nil)
		return
	}
	view, err := h.services.flowManagement.GetFlow(r.Context(), usecase.GetFlowQuery{WorkspaceID: workspaceID, FlowID: domain.FlowID(flowIDFromRequest(r)), Version: version})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapFlowDefinition(view))
}

func (h *handler) updateFlow(w http.ResponseWriter, r *http.Request) {
	var request flowRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.WorkspaceID) == "" {
		request.WorkspaceID = strings.TrimSpace(r.PathValue("id"))
	}
	if strings.TrimSpace(request.WorkspaceID) == "" {
		writeValidationError(w, r, "workspace_id is required", nil)
		return
	}
	command, err := toUpdateFlowCommand(flowIDFromRequest(r), request)
	if err != nil {
		writeValidationError(w, r, "invalid flow payload", err.Error())
		return
	}
	view, err := h.services.flowManagement.UpdateFlow(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapFlowDefinition(view))
}

func (h *handler) validateFlow(w http.ResponseWriter, r *http.Request) {
	var request flowRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.WorkspaceID) == "" {
		request.WorkspaceID = strings.TrimSpace(r.PathValue("id"))
	}
	if strings.TrimSpace(request.WorkspaceID) == "" {
		writeValidationError(w, r, "workspace_id is required", nil)
		return
	}
	command, err := toValidateFlowCommand(flowIDFromRequest(r), request)
	if err != nil {
		writeValidationError(w, r, "invalid flow payload", err.Error())
		return
	}
	result, err := h.services.flowManagement.ValidateFlow(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, validationResponse{Valid: result.Valid, Issues: result.Issues})
}

func (h *handler) launchRun(w http.ResponseWriter, r *http.Request) {
	var request runLaunchRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.InitiatedBy) == "" {
		writeValidationError(w, r, "initiated_by is required", nil)
		return
	}
	request.WorkspaceID = firstNonEmpty(strings.TrimSpace(r.PathValue("id")), strings.TrimSpace(request.WorkspaceID))
	if strings.TrimSpace(request.WorkspaceID) == "" {
		writeValidationError(w, r, "workspace_id is required", nil)
		return
	}
	run, err := h.services.runService.LaunchFlow(r.Context(), usecase.LaunchFlowInput{
		WorkspaceID:    domain.WorkspaceID(request.WorkspaceID),
		FlowID:         domain.FlowID(flowIDFromRequest(r)),
		InitiatedBy:    request.InitiatedBy,
		InputJSON:      request.Input,
		Queue:          request.Queue,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, mapRun(run))
}

func (h *handler) getRun(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	view, err := h.services.runService.GetRunStatus(r.Context(), usecase.GetRunStatusQuery{WorkspaceID: workspaceID, RunID: domain.RunID(r.PathValue("id"))})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapRun(view.Run))
}

func (h *handler) getRunSteps(w http.ResponseWriter, r *http.Request) {
	workspaceID := domain.WorkspaceID(strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	view, err := h.services.runService.GetRunStatus(r.Context(), usecase.GetRunStatusQuery{WorkspaceID: workspaceID, RunID: domain.RunID(r.PathValue("id"))})
	if err != nil {
		writeError(w, r, err)
		return
	}
	items := make([]runStepResponse, 0, len(view.Steps))
	for _, step := range view.Steps {
		items = append(items, mapRunStep(step))
	}
	writeJSON(w, http.StatusOK, listResponse[runStepResponse]{Items: items})
}

func (h *handler) getRunEvents(w http.ResponseWriter, r *http.Request) {
	if h.services.runEvents == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "run events are not configured"})
		return
	}
	workspaceID := domain.WorkspaceID(strings.TrimSpace(r.URL.Query().Get("workspace_id")))
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeValidationError(w, r, "after must be a non-negative integer", nil)
			return
		}
		after = parsed
	}
	items, err := h.services.runEvents.ListRunEvents(r.Context(), usecase.ListRunEventsQuery{
		WorkspaceID:   workspaceID,
		RunID:         domain.RunID(r.PathValue("id")),
		AfterSequence: after,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responseItems := make([]runEventResponse, 0, len(items))
	for _, item := range items {
		responseItems = append(responseItems, mapRunEvent(item))
	}
	writeJSON(w, http.StatusOK, listResponse[runEventResponse]{Items: responseItems})
}

func (h *handler) rerun(w http.ResponseWriter, r *http.Request) {
	var request rerunRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.InitiatedBy) == "" {
		writeValidationError(w, r, "initiated_by is required", nil)
		return
	}
	if strings.TrimSpace(request.WorkspaceID) == "" {
		writeValidationError(w, r, "workspace_id is required", nil)
		return
	}
	run, err := h.services.runService.Rerun(r.Context(), usecase.RerunInput{
		WorkspaceID:    domain.WorkspaceID(request.WorkspaceID),
		RunID:          domain.RunID(r.PathValue("id")),
		InitiatedBy:    request.InitiatedBy,
		OverrideJSON:   request.OverrideInput,
		Queue:          request.Queue,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, mapRun(run))
}

func (h *handler) importCurl(w http.ResponseWriter, r *http.Request) {
	var request importCurlRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.Command) == "" {
		writeValidationError(w, r, "command is required", nil)
		return
	}
	result, err := h.services.curlImport.ImportCurl(r.Context(), usecase.ImportCurlInput{Command: request.Command})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, importCurlResponse{RequestSpec: mapRequestSpec(result.RequestSpec)})
}

func parseOptionalVersion(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	version, err := strconv.Atoi(raw)
	if err != nil || version < 1 {
		return 0, fmt.Errorf("version must be a positive integer")
	}
	return version, nil
}

func parsePagination(r *http.Request) (int, int, error) {
	limit := 0
	offset := 0
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("limit must be a non-negative integer")
		}
		limit = parsed
	}
	if value := r.URL.Query().Get("offset"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("offset must be a non-negative integer")
		}
		offset = parsed
	}
	return limit, offset, nil
}

func parseFlowStatuses(raw string) []domain.FlowStatus {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	statuses := make([]domain.FlowStatus, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		statuses = append(statuses, domain.FlowStatus(trimmed))
	}
	return statuses
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func flowIDFromRequest(r *http.Request) string {
	return firstNonEmpty(r.PathValue("flowId"), r.PathValue("id"))
}
