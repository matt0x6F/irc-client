// Compatibility shim — re-exports the v3-generated service bindings under the
// import path the app already uses (`wailsjs/go/main/App`). The v3 generator
// keeps each method's original name, so existing `import { SendCommand } from
// '…/wailsjs/go/main/App'` call sites resolve unchanged. The real bindings live
// under frontend/bindings/ and are regenerated with `wails3 generate bindings`.
export * from '../../../bindings/github.com/matt0x6f/irc-client/app'
