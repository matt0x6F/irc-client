package sasl

// External implements SASL EXTERNAL: identity comes from the TLS client
// certificate, so the response is empty (the fork encodes it as "AUTHENTICATE +").
type External struct{}

func NewExternal() Mechanism { return &External{} }

func (e *External) Name() string { return "EXTERNAL" }

func (e *External) Respond(_ []byte) ([]byte, error) { return []byte{}, nil }
