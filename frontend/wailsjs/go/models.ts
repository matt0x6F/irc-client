// Compatibility shim — maps the v2 `main` / `storage` model namespaces onto the
// v3-generated model modules. The v3 model classes retain a static `createFrom`,
// so existing call sites like `main.NetworkConfig.createFrom(…)` and
// `storage.Message.createFrom(…)` keep working unchanged.
export * as main from '../../bindings/github.com/matt0x6f/irc-client/models'
export * as storage from '../../bindings/github.com/matt0x6f/irc-client/internal/storage/models'
