# API Reference

Everything a script can use lives in the `cascade` package. This is the complete surface.

---

## Handlers

Handlers are functions you define at the top level of your script. All are optional; Cascade calls whichever ones exist.

```go
func Setup(c *cascade.Client)        // runs once at load, reload, or enable
func OnText(e cascade.TextEvent)     // inbound channel or DM message (PRIVMSG)
func OnNotice(e cascade.NoticeEvent) // inbound NOTICE
func OnJoin(e cascade.JoinEvent)     // a user joined a channel
func OnPart(e cascade.PartEvent)     // a user left a channel
```

---

## `cascade.TextEvent`

Passed to `OnText` for every inbound `PRIVMSG`.

```go
type TextEvent struct {
    Nick    string // sender's nick
    Channel string // "#chan" for a channel message; your nick for a DM
    Message string // the full message text
}

func (e TextEvent) IsDM() bool             // true if the target is not a channel ("#" or "&")
func (e TextEvent) HasPrefix(s string) bool // true if Message starts with s
func (e TextEvent) Arg(n int) string        // n-th whitespace-separated field, 1-based; "" if out of range
func (e TextEvent) Reply(msg string)        // send msg to the channel, or back to the sender for a DM
```

---

## `cascade.NoticeEvent`

Passed to `OnNotice` for every inbound `NOTICE`.

```go
type NoticeEvent struct {
    Nick    string
    Channel string
    Message string
}

func (e NoticeEvent) IsDM() bool
func (e NoticeEvent) Reply(msg string)
```

!!! note
    `NoticeEvent` has no `HasPrefix` or `Arg` helpers (unlike `TextEvent`). Compare `e.Message` directly using built-in string functions.

---

## `cascade.JoinEvent`

Passed to `OnJoin` when any user joins a channel.

```go
type JoinEvent struct {
    Nick    string
    Channel string
}

func (e JoinEvent) Reply(msg string) // PRIVMSG to the channel
```

---

## `cascade.PartEvent`

Passed to `OnPart` when any user leaves a channel.

```go
type PartEvent struct {
    Nick    string
    Channel string
    Reason  string
}

func (e PartEvent) Reply(msg string) // PRIVMSG to the channel
```

---

## `cascade.Client`

Received by `Setup(c *cascade.Client)`. Provides proactive actions: scheduling timers and addressing networks.

```go
func (c *Client) Network(name string) Network       // handle for a configured network
func (c *Client) Every(interval string, fn func())  // repeat fn every interval (Go duration string, e.g. "5m")
func (c *Client) After(delay string, fn func())     // run fn once after delay
```

Invalid duration strings are logged and ignored. All timers stop automatically on script reload, disable, or unload.

---

## `cascade.Network`

Returned by `c.Network(name)`. Represents one configured IRC network.

```go
func (n Network) Say(target, message string) // PRIVMSG to target (channel or nick); no-op if the network is unknown or disconnected
```

---

## Internal constructors

The `cascade` package also exports `NewTextEvent`, `NewNoticeEvent`, `NewJoinEvent`, `NewPartEvent`, and `NewClient`. These are used by Cascade itself to build the values it hands to your handlers. Scripts do not call them.
