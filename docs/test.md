# Smoke Test Notes

Minimal post-change checks. Run from the repo root.

## Build

```sh
make build
```

Expect exit 0 and a binary. Compile errors fail the smoke run.

## Unit tests

```sh
go test ./...
```

Expect exit 0. Investigate any package marked `FAIL`; skips are fine.

## Service health

Start the service locally on a port other than 7789 (owned by the supervisor),
then:

```sh
curl -fsS http://127.0.0.1:<port>/healthz
```

Expect HTTP 200. A connection refused means the process died on startup — check
the service log.

## Web console

Open the console in a browser. Expect the project list to render from the API
with no console errors. A blank page or 404 usually means the embedded `web/dist`
asset is stale; rebuild it.

## Notes

- These checks are non-destructive: no data files are rewritten.
- Failures here are blockers for delivery; report the failing command and its
  output.
