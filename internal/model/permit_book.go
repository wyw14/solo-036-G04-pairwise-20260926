package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type MaintenancePermitStatus string

const (
	PermitApplied           MaintenancePermitStatus = "applied"
	PermitIsolationVerified MaintenancePermitStatus = "isolation_verified"
	PermitAuthorized        MaintenancePermitStatus = "authorized"
	PermitRevoked           MaintenancePermitStatus = "revoked"
	PermitReturned          MaintenancePermitStatus = "returned"
)

type MaintenancePermitEntryKind string

const (
	PermitEntryApplied           MaintenancePermitEntryKind = "applied"
	PermitEntryIsolationVerified MaintenancePermitEntryKind = "isolation_verified"
	PermitEntryAuthorized        MaintenancePermitEntryKind = "authorized"
	PermitEntryRevoked           MaintenancePermitEntryKind = "revoked"
	PermitEntryReturned          MaintenancePermitEntryKind = "returned"
)

// Operations that a maintenance permit can place out of service.
const (
	BlockedMaintenanceWork  = "maintenance_work"
	BlockedValveOperation   = "valve_operation"
	BlockedLaunch           = "launch"
	BlockedReceive          = "receive"
	BlockedPressurization   = "pressurization"
	BlockedRouteReservation = "route_reservation"
)

type MaintenancePermitEntry struct {
	Kind           MaintenancePermitEntryKind `json:"kind"`
	Actor          string                     `json:"actor"`
	Detail         string                     `json:"detail,omitempty"`
	IsolationEpoch int64                      `json:"isolation_epoch,omitempty"`
	At             time.Time                  `json:"at"`
}

type MaintenancePermit struct {
	ID             string                   `json:"id"`
	RunID          string                   `json:"run_id"`
	StationID      string                   `json:"station_id"`
	AffectedRoutes []string                 `json:"affected_routes"`
	WorkSummary    string                   `json:"work_summary"`
	Status         MaintenancePermitStatus  `json:"status"`
	IsolationEpoch int64                    `json:"isolation_epoch,omitempty"`
	PressureValue  float64                  `json:"pressure_value,omitempty"`
	AppliedAt      time.Time                `json:"applied_at"`
	UpdatedAt      time.Time                `json:"updated_at"`
	Entries        []MaintenancePermitEntry `json:"entries"`
}

type BlockedOperation struct {
	Operation string `json:"operation"`
	Label     string `json:"label"`
	Reason    string `json:"reason"`
}

// MaintenancePermitView is the API projection of a permit, augmented with the
// operations that its current status places out of service.
type MaintenancePermitView struct {
	MaintenancePermit
	BlockedOperations []BlockedOperation `json:"blocked_operations"`
}

func ViewOf(permit MaintenancePermit) MaintenancePermitView {
	return MaintenancePermitView{MaintenancePermit: permit, BlockedOperations: permit.BlockedOperations()}
}

func ValidMaintenanceTransition(from, to MaintenancePermitStatus) bool {
	switch from {
	case PermitApplied:
		return to == PermitIsolationVerified || to == PermitRevoked
	case PermitIsolationVerified:
		return to == PermitAuthorized || to == PermitRevoked
	case PermitAuthorized:
		// Work closure (revocation) must be recorded before production hand-back.
		return to == PermitRevoked
	case PermitRevoked:
		return to == PermitReturned
	}
	return false
}

// Closed reports whether the permit has been returned to service and no longer
// restricts anything.
func (p MaintenancePermit) Closed() bool { return p.Status == PermitReturned }

// ProductionBlocked reports whether normal production operations are frozen by
// the permit. A mere application does not freeze production; verified isolation
// and every status afterwards does, until the permit is formally returned.
func (p MaintenancePermit) ProductionBlocked() bool {
	switch p.Status {
	case PermitIsolationVerified, PermitAuthorized, PermitRevoked:
		return true
	}
	return false
}

// Blocks reports whether the permit currently freezes the given station or
// route for production operations.
func (p MaintenancePermit) Blocks(stationID, routeID string) bool {
	if !p.ProductionBlocked() {
		return false
	}
	if stationID != "" && p.StationID == stationID {
		return true
	}
	if routeID != "" {
		for _, affected := range p.AffectedRoutes {
			if affected == routeID {
				return true
			}
		}
	}
	return false
}

// BlockedOperations enumerates, for the permit status, every operation that
// cannot currently proceed and why. The list is part of the permit record so
// maintenance and control crews read the same prohibition table.
func (p MaintenancePermit) BlockedOperations() []BlockedOperation {
	id := p.ID
	production := []struct {
		operation string
		label     string
	}{
		{BlockedValveOperation, "Valve operation at the affected station"},
		{BlockedLaunch, "Pig launch / launcher operation"},
		{BlockedReceive, "Pig receive / receiver operation"},
		{BlockedPressurization, "Pressurization and repressurization"},
		{BlockedRouteReservation, "Reservation of affected routes"},
	}
	productionReason := func() string {
		switch p.Status {
		case PermitIsolationVerified:
			return fmt.Sprintf("permit %s has verified isolation; production stays locked out until work is authorized and returned", id)
		case PermitAuthorized:
			return fmt.Sprintf("permit %s authorizes maintenance; production equipment is locked and tagged out", id)
		case PermitRevoked:
			return fmt.Sprintf("permit %s has been revoked; production stays locked out pending return-to-work confirmation", id)
		}
		return ""
	}
	maintenanceEntry := func(reason string) BlockedOperation {
		return BlockedOperation{Operation: BlockedMaintenanceWork, Label: "Hands-on maintenance work", Reason: reason}
	}
	switch p.Status {
	case PermitApplied:
		return []BlockedOperation{maintenanceEntry(fmt.Sprintf("permit %s is pending isolation verification and authorization", id))}
	case PermitIsolationVerified:
		out := []BlockedOperation{maintenanceEntry(fmt.Sprintf("permit %s isolation is verified but work is not yet authorized", id))}
		reason := productionReason()
		for _, op := range production {
			out = append(out, BlockedOperation{Operation: op.operation, Label: op.label, Reason: reason})
		}
		return out
	case PermitAuthorized:
		out := make([]BlockedOperation, 0, len(production))
		reason := productionReason()
		for _, op := range production {
			out = append(out, BlockedOperation{Operation: op.operation, Label: op.label, Reason: reason})
		}
		return out
	case PermitRevoked:
		revocation := "permit was revoked"
		for _, entry := range p.Entries {
			if entry.Kind == PermitEntryRevoked && strings.TrimSpace(entry.Detail) != "" {
				revocation = "permit was revoked: " + entry.Detail
				break
			}
		}
		out := []BlockedOperation{maintenanceEntry(revocation)}
		reason := productionReason()
		for _, op := range production {
			out = append(out, BlockedOperation{Operation: op.operation, Label: op.label, Reason: reason})
		}
		return out
	case PermitReturned:
		return []BlockedOperation{}
	}
	return nil
}

func (p MaintenancePermit) Validate(stations map[string]Station, routes map[string]bool) error {
	if strings.TrimSpace(p.ID) == "" {
		return errors.New("maintenance permit id is required")
	}
	if strings.TrimSpace(p.RunID) == "" {
		return errors.New("maintenance permit must reference an operation")
	}
	if _, ok := stations[p.StationID]; !ok {
		return fmt.Errorf("maintenance permit references unknown station %s", p.StationID)
	}
	if len(p.AffectedRoutes) == 0 {
		return errors.New("maintenance permit must list affected routes")
	}
	for _, routeID := range p.AffectedRoutes {
		if !routes[routeID] {
			return fmt.Errorf("maintenance permit references unknown route %s", routeID)
		}
	}
	if strings.TrimSpace(p.WorkSummary) == "" {
		return errors.New("maintenance permit work summary is required")
	}
	switch p.Status {
	case PermitApplied, PermitIsolationVerified, PermitAuthorized, PermitRevoked, PermitReturned:
	default:
		return fmt.Errorf("invalid maintenance permit status %q", p.Status)
	}
	if p.AppliedAt.IsZero() || p.UpdatedAt.IsZero() {
		return errors.New("maintenance permit timestamps are required")
	}
	if len(p.Entries) == 0 {
		return errors.New("maintenance permit must record its application entry")
	}
	for index, entry := range p.Entries {
		if strings.TrimSpace(entry.Actor) == "" {
			return fmt.Errorf("maintenance permit entry %d requires an actor", index)
		}
		if entry.At.IsZero() {
			return fmt.Errorf("maintenance permit entry %d requires a timestamp", index)
		}
		switch entry.Kind {
		case PermitEntryApplied, PermitEntryIsolationVerified, PermitEntryAuthorized, PermitEntryRevoked, PermitEntryReturned:
		default:
			return fmt.Errorf("maintenance permit entry %d has invalid kind %q", index, entry.Kind)
		}
		if entry.Kind == PermitEntryIsolationVerified && entry.IsolationEpoch <= 0 {
			return fmt.Errorf("maintenance permit entry %d must carry the isolation epoch", index)
		}
	}
	if p.Entries[0].Kind != PermitEntryApplied {
		return errors.New("maintenance permit history must start with the application entry")
	}
	last := p.Entries[len(p.Entries)-1]
	if MaintenancePermitStatus(last.Kind) != p.Status {
		return fmt.Errorf("maintenance permit status %s does not match its latest recorded step %s", p.Status, last.Kind)
	}
	if p.Status == PermitIsolationVerified && p.IsolationEpoch <= 0 {
		return errors.New("isolation-verified permit must record the verified epoch")
	}
	return nil
}
