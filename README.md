PigFlow is a local HTTP control service for long-distance pipeline pigging operations.

## Maintenance permit register

Valve and pig send/receive operations already carry state and isolation proofs;
maintenance crews additionally get a traceable permit register covering
shutdown, isolation, and return-to-service authorization. Each permit links a
work reference, a station, and the affected routes, and records every step of
its lifecycle:

```
applied -> isolated -> authorized -> restored
   \         \           \
    \         \           \-> revoked
```

- **applied** — application registered; the affected routes and station are
  held immediately.
- **isolated** — isolation verified against live state: station main valve
  closed, station pressure at zero, and no run fenced to any affected route.
  The evidence is recorded in the permit history.
- **authorized** — maintenance work may proceed.
- **revoked** — permit cancelled (reason required, terminal).
- **restored** — line returned to service (terminal); holds lift.

While a permit is active (applied, isolated, or authorized), launches on its
affected routes are rejected, and the permit's `blocked_operations` plus the
`/api/interlocks` view state exactly which operations are currently not
permitted. Every transition is journaled (`maintenance` events) and persisted
in the control snapshot, so the register survives restarts with
`PIGFLOW_RECOVER=1`.

### Endpoints

```
POST   /api/maintenance                          apply: {work_ref, station_id, route_ids[], applicant, note}
GET    /api/maintenance[?station_id=&status=&active=]
GET    /api/maintenance/{permitID}
POST   /api/maintenance/{permitID}/isolation     verify isolation: {actor, note}
POST   /api/maintenance/{permitID}/authorization authorize work: {actor, note}
POST   /api/maintenance/{permitID}/revocation    revoke: {actor, reason}
POST   /api/maintenance/{permitID}/restoration   return to service: {actor, note}
```
