# API Reference

Everything a script can use lives in the `cascade` package.

## Handlers

All handlers are optional.

```go
func Setup(c *cascade.Client)
func OnText(e cascade.TextEvent)
func OnNotice(e cascade.NoticeEvent)
func OnJoin(e cascade.JoinEvent)
func OnPart(e cascade.PartEvent)
func OnQuit(e cascade.QuitEvent)
func OnKick(e cascade.KickEvent)
func OnNick(e cascade.NickEvent)
func OnUserStatus(e cascade.UserStatusEvent)
```

## Message events

`TextEvent` represents an inbound `PRIVMSG`:

```go
type TextEvent struct {
    Nick, Channel, Message string
    Self, Account, Network string
    MsgID string
    Time Time
    Action bool
}
func (e TextEvent) IsDM() bool
func (e TextEvent) HasPrefix(s string) bool
func (e TextEvent) Arg(n int) string
func (e TextEvent) Reply(message string)
func (e TextEvent) IsHighlight() bool
```

`NoticeEvent` represents an inbound `NOTICE` and has the same fields except `Action`:

```go
func (e NoticeEvent) IsDM() bool
func (e NoticeEvent) Reply(message string)
func (e NoticeEvent) IsHighlight() bool
```

`IsDM` uses the connected server's `CHANTYPES`; it works with nonstandard channel prefixes. `Time` is server-time aware when the server supplied a timestamp. `MsgID` and `Account` are empty when unavailable.

## Membership and identity events

```go
type JoinEvent struct {
    Nick, Channel string
    Self, Account, Network, Host, Realname string
    Time Time
}
func (e JoinEvent) Reply(message string)

type PartEvent struct {
    Nick, Channel, Reason string
    Self, Account, Network, Host, Realname string
    Time Time
}
func (e PartEvent) Reply(message string)

type QuitEvent struct {
    Nick, Reason string
    Self, Account, Network, Host, Realname string
    Time Time
}

type KickEvent struct {
    Nick, By, Channel, Reason string
    Self, Account, Network string
    Time Time
}
func (e KickEvent) Reply(message string)

type NickEvent struct {
    OldNick, NewNick string
    Self, Account, Network string
    Time Time
}
```

Only events with a single natural channel destination expose `Reply`.

## User status

```go
type UserStatus struct {
    Known bool
    Away bool
    AwayMessage string
    Account string
    Host string
    Realname string
}

type UserStatusEvent struct {
    Network, Self, Nick string
    Status UserStatus
    Time Time
}
func (e UserStatusEvent) IsSelf() bool
```

Status is session-local and query-only: looking up a user never sends `WHOIS`. `Known` means Cascade has a current-session metadata snapshot. It does not mean the user is online. An unknown user and a known present user can both have `Away == false`, so check `Known` whenever that distinction matters.

`OnUserStatus` receives full current snapshots for away, account, host, and real-name changes. Self-away changes arrive only after the IRC server acknowledges them.

## Client, network, and user handles

```go
func (c *Client) Network(name string) Network
func (c *Client) Every(interval string, fn func())
func (c *Client) After(delay string, fn func())

func (n Network) Name() string
func (n Network) IsConnected() bool
func (n Network) Nick() string
func (n Network) IsMe(nick string) bool
func (n Network) Self() User
func (n Network) User(nick string) User
func (n Network) Say(target, message string)
func (n Network) Notice(target, message string)
func (n Network) Action(target, message string)
func (n Network) Join(channel string)
func (n Network) JoinWithKey(channel, key string)
func (n Network) Part(channel, reason string)
func (n Network) ChangeNick(nick string)
func (n Network) SetAway(message string)
func (n Network) ClearAway()

func (u User) Nick() string
func (u User) IsSelf() bool
func (u User) Known() bool
func (u User) IsAway() bool
func (u User) Status() UserStatus
func (u User) Say(message string)
func (u User) Notice(message string)
```

Network names resolve at call time, so handles kept in timer closures survive reconnects. Queries return zero values and actions no-op when the network is unknown or disconnected. Actions are fire-and-forget; script methods do not return transport errors.

`IsMe` and `User.IsSelf` honor the server's IRC case mapping. `User.IsAway` is a convenience for `User.Status().Away`; check `Known()` first when unknown matters.

## Time

```go
func (t Time) Unix() int64
func (t Time) IsZero() bool
func (t Time) Before(other Time) bool
func (t Time) After(other Time) bool
func (t Time) Clock() string
```

`Clock` returns local time as `HH:MM`.

## Internal constructors

The package exports `New...` constructors and client binding options for Cascade's host runtime and SDK tests. Scripts receive host-built clients and events and do not need to call these constructors.
