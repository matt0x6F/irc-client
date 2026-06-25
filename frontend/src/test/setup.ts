import { vi } from 'vitest'
import '@testing-library/jest-dom/vitest'

// Neutralize the Wails transport globally for tests.
//
// The generated bindings (frontend/bindings/**) call `@wailsio/runtime`'s
// `Call.ByID`, which in a real app talks to the native bridge (fetch + a
// window-level promise registry). Under jsdom that async work can resolve AFTER
// the test worker tears its environment down, surfacing as an unhandled
// "ReferenceError: window is not defined" / "fetch failed". Vitest then
// misattributes that unhandled error to whichever test happened to be active,
// failing the run nondeterministically (e.g. it kept landing on
// network.help.test.ts even though that file never calls a binding directly).
//
// Most store/component tests already `vi.mock` the specific App methods they
// assert on; this stub only governs the *default* transport for the binding
// paths a test doesn't explicitly mock, making them inert instead of leaky.
// `Create` is imported by the generated models but unused there (the generator's
// "Unused imports" markers), so a passthrough is safe — verified by running the
// full vitest suite with this mock in place.
vi.mock('@wailsio/runtime', () => {
  const call = () => Promise.resolve(undefined)
  const passthrough = new Proxy({}, { get: () => (value: unknown) => value })
  return {
    Call: { ByID: call, ByName: call },
    CancellablePromise: Promise,
    Create: passthrough,
    Events: { On: () => () => {}, Off: () => {}, Emit: () => {}, OnMultiple: () => () => {} },
  }
})
