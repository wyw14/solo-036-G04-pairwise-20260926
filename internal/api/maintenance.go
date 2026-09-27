package api

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/wyw14/solo-036/internal/model"
	"github.com/wyw14/solo-036/internal/service"
)

type maintenanceApplyRequest struct {
	WorkRef   string   `json:"work_ref"`
	StationID string   `json:"station_id"`
	RouteIDs  []string `json:"route_ids"`
	Applicant string   `json:"applicant"`
	Note      string   `json:"note"`
}

type maintenanceTransitionRequest struct {
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	Note   string `json:"note"`
}

type maintenanceView struct {
	model.MaintenancePermit
	BlockedOperations []model.MaintenanceBlock `json:"blocked_operations"`
}

func presentMaintenance(permit model.MaintenancePermit) maintenanceView {
	blocked := permit.BlockedOperations()
	if blocked == nil {
		blocked = []model.MaintenanceBlock{}
	}
	return maintenanceView{MaintenancePermit: permit, BlockedOperations: blocked}
}

func (h *Handler) ApplyMaintenance(w http.ResponseWriter, r *http.Request) {
	var request maintenanceApplyRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := h.runtime.ApplyMaintenance(service.MaintenanceApplyRequest{
		WorkRef:   request.WorkRef,
		StationID: request.StationID,
		RouteIDs:  request.RouteIDs,
		Applicant: request.Applicant,
		Note:      request.Note,
	})
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, presentMaintenance(permit))
}

func (h *Handler) MaintenancePermits(w http.ResponseWriter, r *http.Request) {
	station := strings.TrimSpace(r.URL.Query().Get("station_id"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	active := strings.TrimSpace(r.URL.Query().Get("active"))
	views := make([]maintenanceView, 0)
	for _, permit := range h.runtime.MaintenancePermits() {
		if station != "" && permit.StationID != station {
			continue
		}
		if status != "" && string(permit.Status) != status {
			continue
		}
		if active != "" && (active == "true") != permit.Active() {
			continue
		}
		views = append(views, presentMaintenance(permit))
	}
	writeJSON(w, http.StatusOK, views)
}

func (h *Handler) MaintenancePermit(w http.ResponseWriter, r *http.Request) {
	permit, err := h.runtime.MaintenancePermit(chi.URLParam(r, "permitID"))
	if err != nil {
		writeError(w, r, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, presentMaintenance(permit))
}

func (h *Handler) VerifyMaintenanceIsolation(w http.ResponseWriter, r *http.Request) {
	h.transitionMaintenance(w, r, func(permitID string, request maintenanceTransitionRequest) (model.MaintenancePermit, error) {
		return h.runtime.VerifyMaintenanceIsolation(permitID, request.Actor, request.Note)
	})
}

func (h *Handler) AuthorizeMaintenance(w http.ResponseWriter, r *http.Request) {
	h.transitionMaintenance(w, r, func(permitID string, request maintenanceTransitionRequest) (model.MaintenancePermit, error) {
		return h.runtime.AuthorizeMaintenance(permitID, request.Actor, request.Note)
	})
}

func (h *Handler) RevokeMaintenance(w http.ResponseWriter, r *http.Request) {
	h.transitionMaintenance(w, r, func(permitID string, request maintenanceTransitionRequest) (model.MaintenancePermit, error) {
		return h.runtime.RevokeMaintenance(permitID, request.Actor, request.Reason)
	})
}

func (h *Handler) RestoreMaintenance(w http.ResponseWriter, r *http.Request) {
	h.transitionMaintenance(w, r, func(permitID string, request maintenanceTransitionRequest) (model.MaintenancePermit, error) {
		return h.runtime.RestoreMaintenance(permitID, request.Actor, request.Note)
	})
}

func (h *Handler) transitionMaintenance(w http.ResponseWriter, r *http.Request, transition func(string, maintenanceTransitionRequest) (model.MaintenancePermit, error)) {
	var request maintenanceTransitionRequest
	if err := decodeBody(w, r, &request); err != nil {
		writeError(w, r, http.StatusBadRequest, err)
		return
	}
	permit, err := transition(chi.URLParam(r, "permitID"), request)
	if err != nil {
		writeError(w, r, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, presentMaintenance(permit))
}
