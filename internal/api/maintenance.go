package api

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/maintenance"
	"github.com/wyw14/solo-036/internal/service"
)

type applyPermitRequest struct {
	RunID          string   `json:"run_id"`
	StationID      string   `json:"station_id"`
	AffectedRoutes []string `json:"affected_routes"`
	WorkSummary    string   `json:"work_summary"`
	Applicant      string   `json:"applicant"`
}

type verifyIsolationRequest struct {
	Actor          string  `json:"actor"`
	IsolationEpoch int64   `json:"isolation_epoch"`
	PressureValue  float64 `json:"pressure_value"`
	Detail         string  `json:"detail"`
}

type authorizePermitRequest struct {
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
}

type revokePermitRequest struct {
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
}

type returnPermitRequest struct {
	Actor         string  `json:"actor"`
	Detail        string  `json:"detail"`
	PressureValue float64 `json:"pressure_value"`
}

func maintenanceErrorStatus(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.As(err, new(maintenance.NotFoundError)):
		writeError(w, r, http.StatusNotFound, err)
	case errors.As(err, new(maintenance.InvalidTransitionError)),
		errors.As(err, new(maintenance.LockoutError)):
		writeError(w, r, http.StatusConflict, err)
	default:
		writeError(w, r, http.StatusBadRequest, err)
	}
}

func (h *Handler) ApplyMaintenancePermit(w http.ResponseWriter, r *http.Request) {
	var request applyPermitRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.ApplyMaintenancePermit(service.ApplyMaintenancePermitRequest{
		RunID:          request.RunID,
		StationID:      request.StationID,
		AffectedRoutes: request.AffectedRoutes,
		WorkSummary:    request.WorkSummary,
		Applicant:      request.Applicant,
	})
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, permit)
}

func (h *Handler) ListMaintenancePermits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.runtime.MaintenancePermits())
}

func (h *Handler) GetMaintenancePermit(w http.ResponseWriter, r *http.Request) {
	permit, err := h.runtime.MaintenancePermit(chi.URLParam(r, "permitID"))
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}

func (h *Handler) VerifyMaintenanceIsolation(w http.ResponseWriter, r *http.Request) {
	var request verifyIsolationRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.VerifyMaintenanceIsolation(chi.URLParam(r, "permitID"), service.VerifyIsolationRequest{
		Actor:          request.Actor,
		IsolationEpoch: request.IsolationEpoch,
		PressureValue:  request.PressureValue,
		Detail:         request.Detail,
	})
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}

func (h *Handler) AuthorizeMaintenancePermit(w http.ResponseWriter, r *http.Request) {
	var request authorizePermitRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.AuthorizeMaintenancePermit(chi.URLParam(r, "permitID"), service.AuthorizeMaintenanceRequest{
		Actor:  request.Actor,
		Detail: request.Detail,
	})
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}

func (h *Handler) RevokeMaintenancePermit(w http.ResponseWriter, r *http.Request) {
	var request revokePermitRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.RevokeMaintenancePermit(chi.URLParam(r, "permitID"), service.RevokeMaintenanceRequest{
		Actor:  request.Actor,
		Detail: request.Detail,
	})
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}

func (h *Handler) ReturnMaintenancePermit(w http.ResponseWriter, r *http.Request) {
	var request returnPermitRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.ReturnMaintenancePermit(chi.URLParam(r, "permitID"), service.ReturnMaintenanceRequest{
		Actor:         request.Actor,
		Detail:        request.Detail,
		PressureValue: request.PressureValue,
	})
	if err != nil {
		maintenanceErrorStatus(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, permit)
}
