// Compatibility shim — exposes the v2 Wails runtime API the app was written
// against, backed by the v3 `@wailsio/runtime` package. The long-term v3-idiomatic
// move is to import { Events, Browser } from '@wailsio/runtime' directly at each
// call site; this shim keeps the migration's surface area small.
//
// Semantics note: v2 delivered the emitted payload directly to EventsOn
// callbacks, whereas v3 wraps it in a WailsEvent. Since the Go side emits a
// single payload (CustomEvent.Data = data[0]), we unwrap `.data` here so every
// existing `EventsOn(name, (payload) => …)` call site keeps working unchanged.
import { Events, Browser } from '@wailsio/runtime'

export function EventsOn(eventName: string, callback: (...data: any[]) => void): () => void {
  return Events.On(eventName, (ev: any) => callback(ev?.data))
}

export function EventsEmit(eventName: string, data?: any): void {
  void (Events.Emit as (name: string, data?: any) => Promise<boolean>)(eventName, data)
}

export function BrowserOpenURL(url: string): void {
  void Browser.OpenURL(url)
}
