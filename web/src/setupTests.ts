import '@testing-library/jest-dom/vitest'
import { afterEach } from 'vitest'
import { cleanup } from '@testing-library/react'

// globals: true isn't set, so RTL's own auto-cleanup (which hooks a global
// afterEach) never registers - without this, unmounted renderHook/render
// results (and the listeners their effects subscribe, e.g. AuthProvider's
// onUnauthorized) leak into the next test in the same file.
afterEach(cleanup)
