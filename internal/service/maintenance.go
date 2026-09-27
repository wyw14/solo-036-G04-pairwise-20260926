package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

type MaintenanceApplyRequest struct {
	WorkRef   string   `json:"work_ref"`
	StationID string   `json:"station_id"`
	RouteIDs  []string `json:"route_ids"`
	Applicant string   `json:"applicant"`
	Note      string   `json:"note"`
}

func (r *Runtime) ApplyMaintenance(request MaintenanceApplyRequest) (model.MaintenancePermit, error) {
	control := r.ensureControl()
	if strings.TrimSpace(request.WorkRef) == "" {
		return model.MaintenancePermit{}, errors.New("work reference is required")
	}
	if strings.TrimSpace(request.Applicant) == "" {
		return model.MaintenancePermit{}, errors.New("applicant is required")
	}
	if _, ok := control.Stations[request.StationID]; !ok {
		return model.MaintenancePermit{}, fmt.Errorf("unknown maintenance station %s", request.StationID)
	}
	routeIDs := make([]string, 0, len(request.RouteIDs))
	seen := make(map[string]bool, len(request.RouteIDs))
	for _, routeID := range request.RouteIDs {
		if _, ok := control.Routes[routeID]; !ok {
			return model.MaintenancePermit{}, fmt.Errorf("unknown affected route %s", routeID)
		}
		if seen[routeID] {
			return model.MaintenancePermit{}, fmt.Errorf("affected route %s is listed twice", routeID)
		}
		seen[routeID] = true
		routeIDs = append(routeIDs, routeID)
	}
	if len(routeIDs) == 0 {
		return model.MaintenancePermit{}, errors.New("at least one affected route is required")
	}
	sort.Strings(routeIDs)
	now := time.Now().UTC()
	permit := model.MaintenancePermit{
		ID:        model.NewRevision(),
		WorkRef:   strings.TrimSpace(request.WorkRef),
		StationID: request.StationID,
		RouteIDs:  routeIDs,
		Status:    model.MaintenanceApplied,
		History: []model.MaintenanceTransition{{
			Sequence: 1,
			Status:   model.MaintenanceApplied,
			Actor:    strings.TrimSpace(request.Applicant),
			Note:     strings.TrimSpace(request.Note),
			At:       now,
		}},
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.mu.Lock()
	r.control.Maintenance[permit.ID] = permit
	r.control.Revision = model.NewRevision()
	r.mu.Unlock()
	if err := r.appendMaintenanceEvent(permit, model.MaintenanceApplied); err != nil {
		return model.MaintenancePermit{}, err
	}
	return permit, nil
}

func (r *Runtime) VerifyMaintenanceIsolation(permitID, actor, note string) (model.MaintenancePermit, error) {
	return r.advanceMaintenance(permitID, model.MaintenanceIsolated, actor, note, func(permit model.MaintenancePermit) (string, error) {
		if permit.Status != model.MaintenanceApplied {
			return "", fmt.Errorf("permit status %s cannot verify isolation", permit.Status)
		}
		return r.isolationEvidence(permit)
	})
}

func (r *Runtime) AuthorizeMaintenance(permitID, actor, note string) (model.MaintenancePermit, error) {
	return r.advanceMaintenance(permitID, model.MaintenanceAuthorized, actor, note, func(permit model.MaintenancePermit) (string, error) {
		if permit.Status != model.MaintenanceIsolated {
			return "", fmt.Errorf("permit status %s cannot be authorized", permit.Status)
		}
		return "", nil
	})
}

func (r *Runtime) RevokeMaintenance(permitID, actor, reason string) (model.MaintenancePermit, error) {
	if strings.TrimSpace(reason) == "" {
		return model.MaintenancePermit{}, errors.New("revocation reason is required")
	}
	return r.advanceMaintenance(permitID, model.MaintenanceRevoked, actor, reason, func(permit model.MaintenancePermit) (string, error) {
		if !permit.Active() {
			return "", fmt.Errorf("permit status %s cannot be revoked", permit.Status)
		}
		return "", nil
	})
}

func (r *Runtime) RestoreMaintenance(permitID, actor, note string) (model.MaintenancePermit, error) {
	return r.advanceMaintenance(permitID, model.MaintenanceRestored, actor, note, func(permit model.MaintenancePermit) (string, error) {
		if permit.Status != model.MaintenanceAuthorized {
			return "", fmt.Errorf("permit status %s cannot return to service", permit.Status)
		}
		return "", nil
	})
}

func (r *Runtime) MaintenancePermits() []model.MaintenancePermit {
	r.ensureControl()
	r.mu.Lock()
	defer r.mu.Unlock()
	permits := make([]model.MaintenancePermit, 0, len(r.control.Maintenance))
	for _, permit := range r.control.Maintenance {
		permits = append(permits, permit)
	}
	return model.SortedMaintenance(permits)
}

func (r *Runtime) MaintenancePermit(permitID string) (model.MaintenancePermit, error) {
	r.ensureControl()
	r.mu.Lock()
	defer r.mu.Unlock()
	permit, ok := r.control.Maintenance[permitID]
	if !ok {
		return model.MaintenancePermit{}, fmt.Errorf("maintenance permit %s not found", permitID)
	}
	return permit, nil
}

func (r *Runtime) maintenanceHold(stationID, routeID string) (model.MaintenancePermit, bool) {
	for _, permit := range r.control.Maintenance {
		if !permit.Active() {
			continue
		}
		if permit.StationID == stationID || permit.CoversRoute(routeID) {
			return permit, true
		}
	}
	return model.MaintenancePermit{}, false
}

func (r *Runtime) advanceMaintenance(permitID string, next model.MaintenanceStatus, actor, note string, check func(model.MaintenancePermit) (string, error)) (model.MaintenancePermit, error) {
	r.ensureControl()
	if strings.TrimSpace(actor) == "" {
		return model.MaintenancePermit{}, errors.New("actor is required")
	}
	r.mu.Lock()
	permit, ok := r.control.Maintenance[permitID]
	if !ok {
		r.mu.Unlock()
		return model.MaintenancePermit{}, fmt.Errorf("maintenance permit %s not found", permitID)
	}
	evidence, err := check(permit)
	if err != nil {
		r.mu.Unlock()
		return model.MaintenancePermit{}, err
	}
	now := time.Now().UTC()
	permit.History = append(permit.History, model.MaintenanceTransition{
		Sequence: int64(len(permit.History)) + 1,
		Status:   next,
		Actor:    strings.TrimSpace(actor),
		Note:     strings.TrimSpace(note),
		Evidence: evidence,
		At:       now,
	})
	permit.Status = next
	permit.UpdatedAt = now
	r.control.Maintenance[permit.ID] = permit
	r.control.Revision = model.NewRevision()
	r.mu.Unlock()
	if err := r.appendMaintenanceEvent(permit, next); err != nil {
		return model.MaintenancePermit{}, err
	}
	return permit, nil
}

func (r *Runtime) isolationEvidence(permit model.MaintenancePermit) (string, error) {
	valveID := permit.StationID + "/main"
	valve, ok := r.control.Valves[valveID]
	if !ok {
		return "", fmt.Errorf("no valve state recorded for station %s", permit.StationID)
	}
	if valve.Position != model.ValveClosed {
		return "", fmt.Errorf("valve %s is %s, not closed", valveID, valve.Position)
	}
	pressure := r.control.Pressures[permit.StationID]
	if pressure.Value > 0 || pressure.Target > 0 {
		return "", fmt.Errorf("station %s still pressurized at %.2f", permit.StationID, pressure.Value)
	}
	for _, routeID := range permit.RouteIDs {
		if routeState := r.control.Routes[routeID]; routeState.Reserved {
			return "", fmt.Errorf("route %s still fenced to run %s", routeID, routeState.RunID)
		}
	}
	return fmt.Sprintf("valve %s closed; station pressure %.2f; routes %s unreserved", valveID, pressure.Value, strings.Join(permit.RouteIDs, ",")), nil
}

func (r *Runtime) appendMaintenanceEvent(permit model.MaintenancePermit, status model.MaintenanceStatus) error {
	return r.appendEvent(model.Event{
		Kind:    "maintenance",
		Epoch:   int64(len(permit.History)),
		Payload: fmt.Sprintf("%s/%s/%s", permit.ID, status, permit.StationID),
	})
}
