package sasl

// Plain implements SASL PLAIN: an authzid (left empty via the leading NUL),
// authcid, and password separated by NUL bytes.
type Plain struct{ username, password string }

func NewPlain(username, password string) Mechanism { return &Plain{username, password} }

func (p *Plain) Name() string { return "PLAIN" }

func (p *Plain) Respond(_ []byte) ([]byte, error) {
	return []byte("\x00" + p.username + "\x00" + p.password), nil
}
