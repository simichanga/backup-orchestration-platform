import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// globals: true isn't set, so RTL's own auto-cleanup (which hooks a global
// afterEach) never registers - without this, unmounted renderHook/render
// results (and the listeners their effects subscribe, e.g. AuthProvider's
// onUnauthorized) leak into the next test in the same file.
afterEach(cleanup)

// jsdom doesn't implement window.matchMedia at all ("Not implemented" throw)
// - usePrefersReducedMotion (and anything that renders framer-motion via it,
// e.g. TriggerBackupModal) needs this to render at all. Defaults to "no
// preference" (matches: false); tests that care about the reduced-motion
// path override window.matchMedia themselves.
if (!window.matchMedia) {
  window.matchMedia = (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })
}
