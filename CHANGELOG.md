# uggly-client changelog

## Unreleased / 2026 surface work

### Streaming (live surfaces)

- Stream-safe Event inject: unary `GetPage` with `Event` + `query[_input]=1` does not cancel `GetPageStream`
- Server-paced streams: no client sleep when `streamDelayMs == 0`
- Stream frame coalesce: only the latest frame is painted
- Stream RPC uses cancel-only context (avoids mid-session deadline kills on long games)
- Skip keystroke re-parse after first stream frame bind
- Skip cookie jar update when response has empty `SetCookies`
- F8 screenshot, F9 pause/resume stream display

### Protocol v2 client support

- Expand tables, item lists, prompts, and layout nodes before draw (`expand.go`)
- Event and link query/event forwarding
- Form autofocus after draw
- DivScroll key action (client-side box scroll)
- `Meta.Hello` capability handshake (best-effort)
- String border/fill chars and `Style.attrs` via boxes conversion
- Feed listing pagination (n/p)

### Reliability / paint

- Dirty-region paint buffer (`paint_buffer.go`)
- TLS / session hardening notes (TLS 1.2+ where configured)
- Cookie host stamping for blank `Cookie.server`
- Headless mode: simulation screen, synthetic keys, screen capture (`--output`)

### Tests

- Unit coverage for cookies, paint buffer, boxes/scroll expand paths (where present)

### Not in this repo

Sample surface servers (platformer, docstream) live outside uggly-client and may be published separately.
