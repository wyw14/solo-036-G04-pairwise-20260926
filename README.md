PigFlow is a local HTTP control service for long-distance pipeline pigging operations.

## Maintenance permit book

Maintenance work on a station or route is governed by a traceable permit that
links an operation (`run_id`), a station and the affected routes. Each move is
recorded with the acting person, timestamp and (for isolation) the verified
isolation epoch, and appended to the durable journal and control snapshot.

Permit lifecycle:

```
applied ──> isolation_verified ──> authorized ──> revoked ──> returned
   └──────────────> revoked ──────────────────────────────────^
```

| Status               | Maintenance work | Production (valves / launch / receive / pressurization / route reservation) |
|----------------------|------------------|------------------------------------------------------------------------------|
| `applied`            | blocked          | allowed                                                                      |
| `isolation_verified` | blocked          | blocked                                                                      |
| `authorized`         | allowed          | blocked                                                                      |
| `revoked`            | blocked          | blocked (awaiting return confirmation)                                       |
| `returned`           | closed           | released                                                                     |

The exact operations that cannot currently proceed, with reasons, are returned
on every permit as `blocked_operations`, surface on `GET /api/interlocks`, and
are enforced by the launch, receive and repressurization endpoints.

Endpoints:

- `POST   /api/maintenance-permits` — apply (run, station, affected routes)
- `GET    /api/maintenance-permits` — list the permit book
- `GET    /api/maintenance-permits/{permitID}` — permit with blocked operations
- `POST   /api/maintenance-permits/{permitID}/isolation-verification`
- `POST   /api/maintenance-permits/{permitID}/authorization`
- `POST   /api/maintenance-permits/{permitID}/revocation`
- `POST   /api/maintenance-permits/{permitID}/return` — return to service
