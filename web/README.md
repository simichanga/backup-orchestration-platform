# BOP ops console

The browser UI for BOP's HTTP API - see [docs/05-operations.md](../docs/05-operations.md#http-api-and-web-ui)
for what it's for and how it's served in production. This directory is
the source; the build output feeds `internal/webui`'s `//go:embed`
directive directly (`vite.config.ts` writes to `../internal/webui/dist`),
so `bop controller` can serve the whole thing itself with no separate
deploy or Node.js runtime required.

Stack: React + TypeScript + Vite, react-router for client-side routing,
plain CSS Modules (no Tailwind/component library) so the design stays
deliberate rather than templated. No backend framework opinions apply
here - this only ever talks to BOP's own `/v1/*` REST API, same-origin.

## Design

Dark-first "vault ledger" concept: dense, information-forward tables
rather than card grids, IBM Plex Sans/Mono for UI chrome and data, and a
Fraunces display face used sparingly for headings. The signature element
is the "seal" ring (`src/components/Seal.tsx`) shown on a job's detail
page - its fill tier is derived directly from which of BOP's real
verification events (`ArtifactCreated` / `RepositoryVerificationCompleted`
/ `RestoreVerificationCompleted`) have actually fired for that job, not
decorative. Fonts are self-hosted (`@fontsource`, latin subset only) since
this can run on an internal/airgapped network - nothing loads from a CDN.

## Developing

```bash
npm install
npm run dev
```

The dev server proxies `/v1` to `http://127.0.0.1:9091` (see
`vite.config.ts`) - run a real `bop controller` with `api.enabled: true`
alongside it rather than mocking the API. Auth is a bearer token pasted
into the connect screen and held in `sessionStorage` for that tab only -
see `src/state/auth.tsx` and `src/api/client.ts`.

## Building

```bash
npm run build
```

Writes to `../internal/webui/dist`. From the repo root, `make build-web`
does the same (`npm ci && npm run build`); `make build` runs it
automatically before compiling the Go binary. **Commit the result** after
changing anything under `web/` - `internal/webui/dist` is checked in
deliberately (same discipline as any generated/vendored file) so
`go build`/`go install` work standalone with no Node.js installed. CI
rebuilds the frontend and fails if the committed `dist` doesn't match.
