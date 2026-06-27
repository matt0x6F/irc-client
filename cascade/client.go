package cascade

// Client is the script's proactive handle, passed to Setup(c). Its callbacks are
// bound by the host; scripts never construct it.
type Client struct {
	sayFn   func(networkName, target, message string)
	everyFn func(interval string, fn func())
	afterFn func(delay string, fn func())
}

// NewClient is the host-side constructor.
func NewClient(say func(networkName, target, message string), every func(interval string, fn func()), after func(delay string, fn func())) *Client {
	return &Client{sayFn: say, everyFn: every, afterFn: after}
}

// Network returns a handle to a configured network by name. Actions resolve the
// network at call time, so a handle to a disconnected/unknown network simply
// no-ops (the host logs it).
func (c *Client) Network(name string) Network { return Network{name: name, sayFn: c.sayFn} }

// Every schedules fn to run repeatedly on the given interval (e.g. "5m", "30s").
func (c *Client) Every(interval string, fn func()) {
	if c.everyFn != nil {
		c.everyFn(interval, fn)
	}
}

// After schedules fn to run once after the given delay.
func (c *Client) After(delay string, fn func()) {
	if c.afterFn != nil {
		c.afterFn(delay, fn)
	}
}

// Network is a handle to one configured network.
type Network struct {
	name  string
	sayFn func(networkName, target, message string)
}

// Say sends a PRIVMSG to target on this network. Fire-and-forget.
func (n Network) Say(target, message string) {
	if n.sayFn != nil {
		n.sayFn(n.name, target, message)
	}
}
