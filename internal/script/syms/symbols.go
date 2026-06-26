package syms

import "github.com/traefik/yaegi/interp"

// Symbols is the Yaegi symbol table for the cascade API. Generated extract
// files populate it in their init(). This is the ENTIRE surface a v1 script
// sees — no stdlib is injected, so the absence is the capability sandbox.
var Symbols = interp.Exports{}

const (
	cascadeFullKey  = "github.com/matt0x6f/irc-client/internal/script/cascade/cascade"
	cascadeShortKey = "cascade/cascade"
)

// Table returns the cascade symbol table for injection via interp.Use. It
// aliases the short "cascade" import path to the entry yaegi extract generated
// under the full module path, so scripts can write `import "cascade"`. Computed
// at call time, so the generated init() has already populated the full key.
func Table() interp.Exports {
	if _, ok := Symbols[cascadeShortKey]; !ok {
		if full, ok := Symbols[cascadeFullKey]; ok {
			Symbols[cascadeShortKey] = full
		}
	}
	return Symbols
}
