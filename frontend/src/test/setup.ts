import { vi } from 'vitest'
import '@testing-library/jest-dom/vitest'

// jsdom does not implement ResizeObserver, but components observe their scroll
// container with it (e.g. message-view re-pins to the bottom when the input area
// resizes the viewport). The real Wails WKWebView provides it natively; here a
// no-op stub keeps those effects from throwing on mount. jsdom does no layout, so
// the callback would never fire with meaningful sizes anyway.
if (typeof globalThis.ResizeObserver === 'undefined') {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  }
}

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
  return {
    Call: { ByID: call, ByName: call },
    CancellablePromise: Promise,
    // Create must implement Array/Map/Struct properly so model createFrom calls work
    // correctly when tests exercise component paths that render server lists.
    Create: {
      Any: (source: unknown) => source,
      ByteSlice: (source: unknown) => (source == null ? '' : source),
      Array: (element: (s: unknown) => unknown) =>
        (source: unknown[] | null) => {
          if (source === null) return []
          for (let i = 0; i < source.length; i++) source[i] = element(source[i])
          return source
        },
      Map: (key: unknown, value: (s: unknown) => unknown) =>
        (source: Record<string, unknown> | null) => {
          if (source === null) return {}
          for (const k in source) source[k] = value(source[k])
          return source
        },
      Nullable: (element: (s: unknown) => unknown) =>
        (source: unknown) => (source === null ? null : element(source)),
      Struct: (createField: Record<string, (s: unknown) => unknown>) =>
        (source: Record<string, unknown>) => {
          for (const name in createField) {
            if (name in source) source[name] = createField[name](source[name])
          }
          return source
        },
    },
    Events: { On: () => () => {}, Off: () => {}, Emit: () => {}, OnMultiple: () => () => {} },
  }
})
