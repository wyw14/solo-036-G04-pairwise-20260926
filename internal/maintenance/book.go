package maintenance

import (
	"sort"
	"sync"
	"time"

	"github.com/wyw14/solo-036/internal/model"
)

// Book is the traceable maintenance permit register. It holds permits in
// application order; every status move appends an auditable entry.
type Book struct {
	mu      sync.RWMutex
	permits []model.MaintenancePermit
}

func NewBook() *Book { return &Book{permits: make([]model.MaintenancePermit, 0)} }

type Application struct {
	ID             string
	RunID          string
	StationID      string
	AffectedRoutes []string
	WorkSummary    string
	Applicant      string
	At             time.Time
}

func (b *Book) Apply(application Application) model.MaintenancePermit {
	b.mu.Lock()
	defer b.mu.Unlock()
	at := application.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	permit := model.MaintenancePermit{
		ID:             application.ID,
		RunID:          application.RunID,
		StationID:      application.StationID,
		AffectedRoutes: append([]string(nil), application.AffectedRoutes...),
		WorkSummary:    application.WorkSummary,
		Status:         model.PermitApplied,
		AppliedAt:      at,
		UpdatedAt:      at,
		Entries: []model.MaintenancePermitEntry{{
			Kind:   model.PermitEntryApplied,
			Actor:  application.Applicant,
			At:     at,
			Detail: application.WorkSummary,
		}},
	}
	sort.Strings(permit.AffectedRoutes)
	b.permits = append(b.permits, permit)
	return permit
}

func (b *Book) Get(id string) (model.MaintenancePermit, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, permit := range b.permits {
		if permit.ID == id {
			return permit, true
		}
	}
	return model.MaintenancePermit{}, false
}

// move applies a validated transition and records the entry.
func (b *Book) move(id string, entry model.MaintenancePermitEntry) (model.MaintenancePermit, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for index := range b.permits {
		permit := &b.permits[index]
		if permit.ID != id {
			continue
		}
		next := model.MaintenancePermitStatus(entry.Kind)
		if !model.ValidMaintenanceTransition(permit.Status, next) {
			return model.MaintenancePermit{}, InvalidTransitionError{From: permit.Status, To: next}
		}
		if entry.At.IsZero() {
			entry.At = time.Now().UTC()
		}
		permit.Status = next
		permit.UpdatedAt = entry.At
		permit.Entries = append(permit.Entries, entry)
		if entry.Kind == model.PermitEntryIsolationVerified {
			permit.IsolationEpoch = entry.IsolationEpoch
		}
		return *permit, nil
	}
	return model.MaintenancePermit{}, NotFoundError{ID: id}
}

type Verification struct {
	Actor          string
	IsolationEpoch int64
	PressureValue  float64
	Detail         string
	At             time.Time
}

func (b *Book) VerifyIsolation(id string, verification Verification) (model.MaintenancePermit, error) {
	if verification.IsolationEpoch <= 0 {
		return model.MaintenancePermit{}, InvalidTransitionError{From: model.PermitApplied, To: model.PermitIsolationVerified}
	}
	return b.move(id, model.MaintenancePermitEntry{
		Kind:           model.PermitEntryIsolationVerified,
		Actor:          verification.Actor,
		Detail:         verification.Detail,
		IsolationEpoch: verification.IsolationEpoch,
		At:             verification.At,
	})
}

type Authorization struct {
	Actor  string
	Detail string
	At     time.Time
}

func (b *Book) Authorize(id string, authorization Authorization) (model.MaintenancePermit, error) {
	return b.move(id, model.MaintenancePermitEntry{
		Kind:   model.PermitEntryAuthorized,
		Actor:  authorization.Actor,
		Detail: authorization.Detail,
		At:     authorization.At,
	})
}

type Revocation struct {
	Actor  string
	Detail string
	At     time.Time
}

func (b *Book) Revoke(id string, revocation Revocation) (model.MaintenancePermit, error) {
	return b.move(id, model.MaintenancePermitEntry{
		Kind:   model.PermitEntryRevoked,
		Actor:  revocation.Actor,
		Detail: revocation.Detail,
		At:     revocation.At,
	})
}

type Return struct {
	Actor         string
	Detail        string
	PressureValue float64
	At            time.Time
}

func (b *Book) ReturnToService(id string, handback Return) (model.MaintenancePermit, error) {
	permit, err := b.move(id, model.MaintenancePermitEntry{
		Kind:   model.PermitEntryReturned,
		Actor:  handback.Actor,
		Detail: handback.Detail,
		At:     handback.At,
	})
	if err != nil {
		return model.MaintenancePermit{}, err
	}
	at := handback.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	permit.UpdatedAt = at
	permit.PressureValue = handback.PressureValue
	b.mu.Lock()
	defer b.mu.Unlock()
	for index := range b.permits {
		if b.permits[index].ID == id {
			b.permits[index] = permit
			break
		}
	}
	return permit, nil
}

func (b *Book) Snapshot() []model.MaintenancePermit {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]model.MaintenancePermit, len(b.permits))
	copy(out, b.permits)
	return out
}

// Active returns permits that have not been formally returned to service,
// including applications and revoked permits awaiting return confirmation.
func (b *Book) Active() []model.MaintenancePermit {
	snapshot := b.Snapshot()
	out := make([]model.MaintenancePermit, 0, len(snapshot))
	for _, permit := range snapshot {
		if !permit.Closed() {
			out = append(out, permit)
		}
	}
	return out
}

func Restore(bk *Book, permits []model.MaintenancePermit) {
	bk.mu.Lock()
	defer bk.mu.Unlock()
	bk.permits = append(bk.permits[:0], permits...)
	sort.SliceStable(bk.permits, func(i, j int) bool { return bk.permits[i].AppliedAt.Before(bk.permits[j].AppliedAt) })
}
