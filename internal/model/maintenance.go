package model

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

type MaintenanceStatus string

const (
	MaintenanceApplied    MaintenanceStatus = "applied"
	MaintenanceIsolated   MaintenanceStatus = "isolated"
	MaintenanceAuthorized MaintenanceStatus = "authorized"
	MaintenanceRevoked    MaintenanceStatus = "revoked"
	MaintenanceRestored   MaintenanceStatus = "restored"
)

func (s MaintenanceStatus) Terminal() bool {
	return s == MaintenanceRevoked || s == MaintenanceRestored
}

func (s MaintenanceStatus) Valid() bool {
	switch s {
	case MaintenanceApplied, MaintenanceIsolated, MaintenanceAuthorized, MaintenanceRevoked, MaintenanceRestored:
		return true
	default:
		return false
	}
}

type MaintenanceTransition struct {
	Sequence int64             `json:"sequence"`
	Status   MaintenanceStatus `json:"status"`
	Actor    string            `json:"actor"`
	Note     string            `json:"note,omitempty"`
	Evidence string            `json:"evidence,omitempty"`
	At       time.Time         `json:"at"`
}

type MaintenanceBlock struct {
	Operation string `json:"operation"`
	Target    string `json:"target"`
	Reason    string `json:"reason"`
}

type MaintenancePermit struct {
	ID        string                  `json:"id"`
	WorkRef   string                  `json:"work_ref"`
	StationID string                  `json:"station_id"`
	RouteIDs  []string                `json:"route_ids"`
	Status    MaintenanceStatus       `json:"status"`
	History   []MaintenanceTransition `json:"history"`
	CreatedAt time.Time               `json:"created_at"`
	UpdatedAt time.Time               `json:"updated_at"`
}

func (p MaintenancePermit) Active() bool { return p.Status.Valid() && !p.Status.Terminal() }

func (p MaintenancePermit) CoversRoute(routeID string) bool {
	for _, id := range p.RouteIDs {
		if id == routeID {
			return true
		}
	}
	return false
}

func (p MaintenancePermit) BlockedOperations() []MaintenanceBlock {
	if !p.Active() {
		return nil
	}
	reason := fmt.Sprintf("maintenance permit %s for %q is %s", p.ID, p.WorkRef, p.Status)
	blocks := make([]MaintenanceBlock, 0, len(p.RouteIDs)+2)
	for _, routeID := range p.RouteIDs {
		blocks = append(blocks, MaintenanceBlock{Operation: "launch", Target: routeID, Reason: reason})
	}
	blocks = append(blocks,
		MaintenanceBlock{Operation: "pressurize", Target: p.StationID, Reason: reason},
		MaintenanceBlock{Operation: "valve-operation", Target: p.StationID, Reason: reason},
	)
	return blocks
}

func (p MaintenancePermit) Validate(stations map[string]Station, routes map[string]RouteState) error {
	if p.ID == "" || strings.TrimSpace(p.WorkRef) == "" {
		return errors.New("maintenance permit identity is incomplete")
	}
	if _, ok := stations[p.StationID]; !ok {
		return fmt.Errorf("maintenance permit references unknown station %s", p.StationID)
	}
	if len(p.RouteIDs) == 0 {
		return errors.New("maintenance permit must affect at least one route")
	}
	seen := make(map[string]bool, len(p.RouteIDs))
	for _, routeID := range p.RouteIDs {
		if _, ok := routes[routeID]; !ok {
			return fmt.Errorf("maintenance permit references unknown route %s", routeID)
		}
		if seen[routeID] {
			return fmt.Errorf("maintenance permit lists route %s twice", routeID)
		}
		seen[routeID] = true
	}
	if !p.Status.Valid() {
		return fmt.Errorf("invalid maintenance status %q", p.Status)
	}
	if len(p.History) == 0 {
		return errors.New("maintenance permit history is empty")
	}
	for index, transition := range p.History {
		if transition.Sequence != int64(index+1) {
			return fmt.Errorf("maintenance history sequence gap at %d", transition.Sequence)
		}
		if !transition.Status.Valid() {
			return fmt.Errorf("maintenance history has invalid status %q", transition.Status)
		}
		if strings.TrimSpace(transition.Actor) == "" {
			return errors.New("maintenance history entry must name an actor")
		}
		if transition.At.IsZero() {
			return errors.New("maintenance history entry must be timestamped")
		}
	}
	last := p.History[len(p.History)-1]
	if last.Status != p.Status {
		return fmt.Errorf("maintenance status %s does not match last history entry %s", p.Status, last.Status)
	}
	if p.Status == MaintenanceRevoked && strings.TrimSpace(last.Note) == "" {
		return errors.New("revoked maintenance permit must record a reason")
	}
	return nil
}

func SortedMaintenance(permits []MaintenancePermit) []MaintenancePermit {
	out := append([]MaintenancePermit(nil), permits...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}
