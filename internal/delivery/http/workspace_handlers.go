package http

import (
	"net/http"
	"strings"

	"stageflow/internal/domain"
	"stageflow/internal/usecase"
)

func (h *handler) createWorkspace(w http.ResponseWriter, r *http.Request) {
	var request workspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Slug) == "" || strings.TrimSpace(request.OwnerTeam) == "" {
		writeValidationError(w, r, "id, name, slug and owner_team are required", nil)
		return
	}
	command, err := toCreateWorkspaceCommand(request)
	if err != nil {
		writeValidationError(w, r, "invalid workspace payload", err.Error())
		return
	}
	view, err := h.services.workspaceManagement.CreateWorkspace(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapWorkspace(view))
}

func (h *handler) listWorkspaces(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePagination(r)
	if err != nil {
		writeValidationError(w, r, "invalid pagination query", err.Error())
		return
	}
	items, err := h.services.workspaceManagement.ListWorkspaces(r.Context(), usecase.ListWorkspacesQuery{
		Statuses: parseWorkspaceStatuses(r.URL.Query().Get("status")),
		NameLike: strings.TrimSpace(r.URL.Query().Get("name")),
		Limit:    limit,
		Offset:   offset,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	response := make([]workspaceResponse, 0, len(items))
	for _, item := range items {
		response = append(response, mapWorkspace(usecase.WorkspaceView{Workspace: item}))
	}
	writeJSON(w, http.StatusOK, listResponse[workspaceResponse]{Items: response})
}

func (h *handler) getWorkspace(w http.ResponseWriter, r *http.Request) {
	view, err := h.services.workspaceManagement.GetWorkspace(r.Context(), usecase.GetWorkspaceQuery{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) updateWorkspace(w http.ResponseWriter, r *http.Request) {
	var request workspaceRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Slug) == "" || strings.TrimSpace(request.OwnerTeam) == "" {
		writeValidationError(w, r, "name, slug and owner_team are required", nil)
		return
	}
	view, err := h.services.workspaceManagement.UpdateWorkspace(r.Context(), toUpdateWorkspaceCommand(r.PathValue("id"), request))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) archiveWorkspace(w http.ResponseWriter, r *http.Request) {
	view, err := h.services.workspaceManagement.ArchiveWorkspace(r.Context(), usecase.ArchiveWorkspaceCommand{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) unarchiveWorkspace(w http.ResponseWriter, r *http.Request) {
	view, err := h.services.workspaceManagement.UnarchiveWorkspace(r.Context(), usecase.UnarchiveWorkspaceCommand{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) updateWorkspacePolicy(w http.ResponseWriter, r *http.Request) {
	var request workspacePolicyDTO
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	command, err := toUpdateWorkspacePolicyCommand(r.PathValue("id"), request)
	if err != nil {
		writeValidationError(w, r, "invalid workspace policy payload", err.Error())
		return
	}
	view, err := h.services.workspaceManagement.UpdateWorkspacePolicy(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) updateWorkspaceVariables(w http.ResponseWriter, r *http.Request) {
	var request []workspaceVarDTO
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	view, err := h.services.workspaceManagement.UpdateWorkspaceVariables(r.Context(), toUpdateWorkspaceVariablesCommand(r.PathValue("id"), request))
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapWorkspace(view))
}

func (h *handler) putWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	var request workspaceSecretRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.Value) == "" {
		writeValidationError(w, r, "name and value are required", nil)
		return
	}
	if err := h.services.workspaceManagement.PutWorkspaceSecret(r.Context(), usecase.PutWorkspaceSecretCommand{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
		SecretName:  request.Name,
		SecretValue: request.Value,
	}); err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, workspaceSecretResponse{ID: strings.ToLower(strings.TrimSpace(request.Name)), Name: strings.ToLower(strings.TrimSpace(request.Name))})
}

func (h *handler) listWorkspaceSecrets(w http.ResponseWriter, r *http.Request) {
	items, err := h.services.workspaceManagement.ListWorkspaceSecrets(r.Context(), usecase.ListWorkspaceSecretsQuery{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, listResponse[workspaceSecretResponse]{Items: mapWorkspaceSecrets(items)})
}

func (h *handler) deleteWorkspaceSecret(w http.ResponseWriter, r *http.Request) {
	if err := h.services.workspaceManagement.DeleteWorkspaceSecret(r.Context(), usecase.DeleteWorkspaceSecretCommand{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
		SecretName:  r.PathValue("secretId"),
	}); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) createSavedRequest(w http.ResponseWriter, r *http.Request) {
	var request savedRequestRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Name) == "" {
		writeValidationError(w, r, "id and name are required", nil)
		return
	}
	command, err := toCreateSavedRequestCommand(r.PathValue("id"), request)
	if err != nil {
		writeValidationError(w, r, "invalid saved request payload", err.Error())
		return
	}
	view, err := h.services.savedRequestManagement.CreateSavedRequest(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, mapSavedRequest(view))
}

func (h *handler) listSavedRequests(w http.ResponseWriter, r *http.Request) {
	limit, offset, err := parsePagination(r)
	if err != nil {
		writeValidationError(w, r, "invalid pagination query", err.Error())
		return
	}
	items, err := h.services.savedRequestManagement.ListSavedRequests(r.Context(), usecase.ListSavedRequestsQuery{
		WorkspaceID: domain.WorkspaceID(r.PathValue("id")),
		NameLike:    strings.TrimSpace(r.URL.Query().Get("name")),
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	response := make([]savedRequestResponse, 0, len(items))
	for _, item := range items {
		response = append(response, mapSavedRequest(usecase.SavedRequestView{SavedRequest: item}))
	}
	writeJSON(w, http.StatusOK, listResponse[savedRequestResponse]{Items: response})
}

func (h *handler) getSavedRequest(w http.ResponseWriter, r *http.Request) {
	view, err := h.services.savedRequestManagement.GetSavedRequest(r.Context(), usecase.GetSavedRequestQuery{
		WorkspaceID:    domain.WorkspaceID(r.PathValue("id")),
		SavedRequestID: domain.SavedRequestID(r.PathValue("requestId")),
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapSavedRequest(view))
}

func (h *handler) updateSavedRequest(w http.ResponseWriter, r *http.Request) {
	var request savedRequestRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		writeValidationError(w, r, "name is required", nil)
		return
	}
	command, err := toUpdateSavedRequestCommand(r.PathValue("id"), r.PathValue("requestId"), request)
	if err != nil {
		writeValidationError(w, r, "invalid saved request payload", err.Error())
		return
	}
	view, err := h.services.savedRequestManagement.UpdateSavedRequest(r.Context(), command)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, mapSavedRequest(view))
}

func (h *handler) runSavedRequest(w http.ResponseWriter, r *http.Request) {
	var request runLaunchRequest
	if err := decodeJSON(r, &request); err != nil {
		writeValidationError(w, r, "invalid JSON body", err.Error())
		return
	}
	if strings.TrimSpace(request.InitiatedBy) == "" {
		writeValidationError(w, r, "initiated_by is required", nil)
		return
	}
	run, err := h.services.runService.RunSavedRequest(r.Context(), usecase.LaunchSavedRequestInput{
		WorkspaceID:    domain.WorkspaceID(r.PathValue("id")),
		SavedRequestID: domain.SavedRequestID(r.PathValue("requestId")),
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

func parseWorkspaceStatuses(raw string) []domain.WorkspaceStatus {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]domain.WorkspaceStatus, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		result = append(result, domain.WorkspaceStatus(trimmed))
	}
	return result
}
