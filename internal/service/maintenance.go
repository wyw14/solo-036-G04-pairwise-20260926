package service

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/wyw14/solo-036/internal/maintenance"
	"github.com/wyw14/solo-036/internal/model"
)

type ApplyMaintenancePermitRequest struct {
	RunID          string   `json:"run_id"`
	StationID      string   `json:"station_id"`
	AffectedRoutes []string `json:"affected_routes"`
	WorkSummary    string   `json:"work_summary"`
	Applicant      string   `json:"applicant"`
}

type VerifyIsolationRequest struct {
	Actor          string  `json:"actor"`
	IsolationEpoch int64   `json:"isolation_epoch"`
	PressureValue  float64 `json:"pressure_value"`
	Detail         string  `json:"detail"`
}

type AuthorizeMaintenanceRequest struct {
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
}

type RevokeMaintenanceRequest struct {
	Actor  string `json:"actor"`
	Detail string `json:"detail"`
}

type ReturnMaintenanceRequest struct {
	Actor         string  `json:"actor"`
	Detail        string  `json:"detail"`
	PressureValue float64 `json:"pressure_value"`
}

func requireActor(actor, field string) error {
	if strings.TrimSpace(actor) == "" {
		return fmt.Errorf("%s is required", field)
	}
	return nil
}

func (r *Runtime) ApplyMaintenancePermit(request ApplyMaintenancePermitRequest) (model.MaintenancePermitView, error) {
	if strings.TrimSpace(request.RunID) == "" {
		return model.MaintenancePermitView{}, errors.New("run_id is required")
	}
	if _, ok := r.Runs.Get(request.RunID); !ok {
		return model.MaintenancePermitView{}, fmt.Errorf("run %s not found", request.RunID)
	}
	if err := requireActor(request.Applicant, "applicant"); err != nil {
		return model.MaintenancePermitView{}, err
	}
	control := r.ensureControl()
	if _, ok := control.Stations[request.StationID]; !ok {
		return model.MaintenancePermitView{}, fmt.Errorf("unknown station %s", request.StationID)
	}
	routes, err := normalizeAffectedRoutes(control, request.AffectedRoutes)
	if err != nil {
		return model.MaintenancePermitView{}, err
	}
	if strings.TrimSpace(request.WorkSummary) == "" {
		return model.MaintenancePermitView{}, errors.New("work_summary is required")
	}
	now := time.Now().UTC()
	permit := r.Maintenance.Apply(maintenance.Application{
		ID:             model.NewRevision(),
		RunID:          request.RunID,
		StationID:      request.StationID,
		AffectedRoutes: routes,
		WorkSummary:    strings.TrimSpace(request.WorkSummary),
		Applicant:      strings.TrimSpace(request.Applicant),
		At:             now,
	})
	if err := r.storeMaintenancePermit(permit); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   permit.RunID,
		Kind:    "maintenance_permit",
		Payload: fmt.Sprintf("applied:%s:%s", permit.ID, permit.StationID),
		At:      now,
	}); err != nil {
		return model.MaintenancePermitView{}, err
	}
	return model.ViewOf(permit), nil
}

func normalizeAffectedRoutes(control *ControlState, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return nil, errors.New("affected_routes must list at least one route")
	}
	routes := make(map[string]struct{}, len(requested))
	for _, routeID := range requested {
		routeID = strings.TrimSpace(routeID)
		if routeID == "" {
			return nil, errors.New("affected_routes contains an empty route id")
		}
		if _, ok := control.Routes[routeID]; !ok {
			return nil, fmt.Errorf("unknown affected route %s", routeID)
		}
		routes[routeID] = struct{}{}
	}
	out := make([]string, 0, len(routes))
	for routeID := range routes {
		out = append(out, routeID)
	}
	sort.Strings(out)
	return out, nil
}

// ensureNoMaintenanceLockout rejects production operations on a station or
// route that an active maintenance permit has locked out.
func (r *Runtime) ensureNoMaintenanceLockout(stationID, routeID string) error {
	permit, blocked := maintenance.BlockingPermit(r.Maintenance, stationID, routeID)
	if !blocked {
		return nil
	}
	return maintenance.LockoutError{PermitID: permit.ID, StationID: stationID, RouteID: routeID, Status: permit.Status}
}

func (r *Runtime) VerifyMaintenanceIsolation(permitID string, request VerifyIsolationRequest) (model.MaintenancePermitView, error) {
	if err := requireActor(request.Actor, "actor"); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if request.IsolationEpoch <= 0 {
		return model.MaintenancePermitView{}, errors.New("isolation_epoch must be positive")
	}
	permit, ok := r.Maintenance.Get(permitID)
	if !ok {
		return model.MaintenancePermitView{}, maintenance.NotFoundError{ID: permitID}
	}
	control := r.ensureControl()
	valveState, ok := control.Valves[permit.StationID+"/main"]
	if !ok || valveState.Position != model.ValveClosed {
		return model.MaintenancePermitView{}, fmt.Errorf("isolation not verified: main valve at %s is not closed", permit.StationID)
	}
	pressureState := control.Pressures[permit.StationID]
	if pressureState.Value != 0 {
		return model.MaintenancePermitView{}, fmt.Errorf("isolation not verified: pressure at %s is %.2f, expected zero", permit.StationID, pressureState.Value)
	}
	// If the valve controller already carries an isolation proof for the run,
	// the claimed epoch must match the recorded proof.
	if proof := r.Valves.IsolationProof(permit.RunID, request.IsolationEpoch); !proof.Accepted {
		if recorded := r.Valves.IsolationEpoch(permit.RunID); recorded > 0 && recorded != request.IsolationEpoch {
			return model.MaintenancePermitView{}, fmt.Errorf("isolation epoch %d does not match recorded proof epoch %d", request.IsolationEpoch, recorded)
		}
	}
	now := time.Now().UTC()
	updated, err := r.Maintenance.VerifyIsolation(permitID, maintenance.Verification{
		Actor:          strings.TrimSpace(request.Actor),
		IsolationEpoch: request.IsolationEpoch,
		PressureValue:  pressureState.Value,
		Detail:         strings.TrimSpace(request.Detail),
		At:             now,
	})
	if err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.storeMaintenancePermit(updated); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   permit.RunID,
		Kind:    "maintenance_permit",
		Epoch:   request.IsolationEpoch,
		Payload: fmt.Sprintf("isolation_verified:%s:epoch=%d", permitID, request.IsolationEpoch),
		At:      now,
	}); err != nil {
		return model.MaintenancePermitView{}, err
	}
	return model.ViewOf(updated), nil
}

func (r *Runtime) AuthorizeMaintenancePermit(permitID string, request AuthorizeMaintenanceRequest) (model.MaintenancePermitView, error) {
	if err := requireActor(request.Actor, "actor"); err != nil {
		return model.MaintenancePermitView{}, err
	}
	permit, ok := r.Maintenance.Get(permitID)
	if !ok {
		return model.MaintenancePermitView{}, maintenance.NotFoundError{ID: permitID}
	}
	now := time.Now().UTC()
	updated, err := r.Maintenance.Authorize(permitID, maintenance.Authorization{
		Actor:  strings.TrimSpace(request.Actor),
		Detail: strings.TrimSpace(request.Detail),
		At:     now,
	})
	if err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.storeMaintenancePermit(updated); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   permit.RunID,
		Kind:    "maintenance_permit",
		Payload: fmt.Sprintf("authorized:%s", permitID),
		At:      now,
	}); err != nil {
		return model.MaintenancePermitView{}, err
	}
	return model.ViewOf(updated), nil
}

func (r *Runtime) RevokeMaintenancePermit(permitID string, request RevokeMaintenanceRequest) (model.MaintenancePermitView, error) {
	if err := requireActor(request.Actor, "actor"); err != nil {
		return model.MaintenancePermitView{}, err
	}
	permit, ok := r.Maintenance.Get(permitID)
	if !ok {
		return model.MaintenancePermitView{}, maintenance.NotFoundError{ID: permitID}
	}
	now := time.Now().UTC()
	updated, err := r.Maintenance.Revoke(permitID, maintenance.Revocation{
		Actor:  strings.TrimSpace(request.Actor),
		Detail: strings.TrimSpace(request.Detail),
		At:     now,
	})
	if err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.storeMaintenancePermit(updated); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   permit.RunID,
		Kind:    "maintenance_permit",
		Payload: fmt.Sprintf("revoked:%s", permitID),
		At:      now,
	}); err != nil {
		return model.MaintenancePermitView{}, err
	}
	return model.ViewOf(updated), nil
}

func (r *Runtime) ReturnMaintenancePermit(permitID string, request ReturnMaintenanceRequest) (model.MaintenancePermitView, error) {
	if err := requireActor(request.Actor, "actor"); err != nil {
		return model.MaintenancePermitView{}, err
	}
	permit, ok := r.Maintenance.Get(permitID)
	if !ok {
		return model.MaintenancePermitView{}, maintenance.NotFoundError{ID: permitID}
	}
	now := time.Now().UTC()
	updated, err := r.Maintenance.ReturnToService(permitID, maintenance.Return{
		Actor:         strings.TrimSpace(request.Actor),
		Detail:        strings.TrimSpace(request.Detail),
		PressureValue: request.PressureValue,
		At:            now,
	})
	if err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.storeMaintenancePermit(updated); err != nil {
		return model.MaintenancePermitView{}, err
	}
	if err := r.appendEvent(model.Event{
		RunID:   permit.RunID,
		Kind:    "maintenance_permit",
		Payload: fmt.Sprintf("returned:%s", permitID),
		At:      now,
	}); err != nil {
		return model.MaintenancePermitView{}, err
	}
	return model.ViewOf(updated), nil
}

func (r *Runtime) storeMaintenancePermit(permit model.MaintenancePermit) error {
	r.mu.Lock()
	if r.control == nil {
		r.control = defaultControlState()
	}
	r.control.MaintenancePermits[permit.ID] = permit
	r.control.Revision = model.NewRevision()
	r.mu.Unlock()
	return nil
}

func (r *Runtime) MaintenancePermit(permitID string) (model.MaintenancePermitView, error) {
	permit, ok := r.Maintenance.Get(permitID)
	if !ok {
		return model.MaintenancePermitView{}, maintenance.NotFoundError{ID: permitID}
	}
	return model.ViewOf(permit), nil
}

func (r *Runtime) MaintenancePermits() []model.MaintenancePermitView {
	permits := r.Maintenance.Snapshot()
	sort.Slice(permits, func(i, j int) bool {
		if permits[i].AppliedAt.Equal(permits[j].AppliedAt) {
			return permits[i].ID < permits[j].ID
		}
		return permits[i].AppliedAt.Before(permits[j].AppliedAt)
	})
	out := make([]model.MaintenancePermitView, 0, len(permits))
	for _, permit := range permits {
		out = append(out, model.ViewOf(permit))
	}
	return out
}
