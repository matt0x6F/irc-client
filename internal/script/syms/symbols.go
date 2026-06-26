package syms

import "github.com/traefik/yaegi/interp"

// Symbols is the Yaegi symbol table for the cascade API. Generated extract
// files populate it in their init(). This is the ENTIRE surface a v1 script
// sees — no stdlib is injected, so the absence is the capability sandbox.
var Symbols = interp.Exports{}
