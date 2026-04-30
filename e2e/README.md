# nonchalant — End-to-End Tests (Playwright)

Browser-driven E2E tests that exercise the full pipeline:

```
ffmpeg (publisher) → RTMP ingest → stream bus → WebSocket-FLV → flv.js → <video>
```

The Go integration tests in `internal/itest` already verify the byte-level
contracts (FLV signature, RTMP handshake, etc.). These Playwright tests go one
step further and assert that a real browser can actually **decode and play**
the live stream.

## Prerequisites

- Go ≥ 1.25 (to build the server)
- `ffmpeg` on `PATH`
- Node.js ≥ 20

## Setup

```bash
make build                    # builds bin/nonchalant
cd e2e
npm install
npx playwright install chromium
```

## Run

```bash
npm test                      # all tests, headless
npm run test:headed           # watch the browser
npm run test:debug            # Playwright Inspector
npm run report                # open last HTML report
```

## What the suite covers

| Spec                       | Scope                                                         |
| -------------------------- | ------------------------------------------------------------- |
| `tests/api.spec.ts`        | `/healthz`, `/api/server`, `/api/streams` (with live publisher) |
| `tests/httpflv.spec.ts`    | HTTP-FLV endpoint emits a valid FLV byte stream               |
| `tests/wsflv.spec.ts`      | flv.js decodes live WS-FLV in Chromium and `<video>` advances |

Each test gets a fresh nonchalant process on ephemeral ports plus an
`ffmpeg`-driven publisher (`assets/nonchalant-test.mp4`, looped). All processes
are torn down on teardown.

## Layout

```
e2e/
├── playwright.config.ts
├── package.json
├── fixtures/
│   └── server.ts              # spawns server + publisher, exposes URL helpers
├── pages/
│   └── wsflv-player.html      # flv.js player page used by wsflv.spec.ts
└── tests/
    ├── api.spec.ts
    ├── httpflv.spec.ts
    └── wsflv.spec.ts
```
