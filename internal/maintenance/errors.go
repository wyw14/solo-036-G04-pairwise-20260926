package maintenance

import (
	"fmt"

	"github.com/wyw14/solo-036/internal/model"
)

type NotFoundError struct{ ID string }

func (e NotFoundError) Error() string { return fmt.Sprintf("maintenance permit %s not found", e.ID) }

type InvalidTransitionError struct {
	From model.MaintenancePermitStatus
	To   model.MaintenancePermitStatus
}

func (e InvalidTransitionError) Error() string {
	return fmt.Sprintf("maintenance permit cannot move from %s to %s", e.From, e.To)
}

// LockoutError reports that an active permit forbids a production operation.
type LockoutError struct {
	PermitID  string
	StationID string
	RouteID   string
	Status    model.MaintenancePermitStatus
}

func (e LockoutError) Error() string {
	switch {
	case e.RouteID != "":
		return fmt.Sprintf("operation blocked by maintenance permit %s (status %s): route %s is locked out", e.PermitID, e.Status, e.RouteID)
	case e.StationID != "":
		return fmt.Sprintf("operation blocked by maintenance permit %s (status %s): station %s is locked out", e.PermitID, e.Status, e.StationID)
	default:
		return fmt.Sprintf("operation blocked by maintenance permit %s (status %s)", e.PermitID, e.Status)
	}
}

// BlockingPermit returns the first active permit that freezes the station or
// route for production operations. Permits in every status from verified
// isolation onwards freeze production until formally returned.
func BlockingPermit(book *Book, stationID, routeID string) (model.MaintenancePermit, bool) {
	for _, permit := range book.Active() {
		if permit.Blocks(stationID, routeID) {
			return permit, true
		}
	}
	return model.MaintenancePermit{}, false
}
