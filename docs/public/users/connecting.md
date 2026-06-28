# Connecting to a network

Networks are configured in **Settings → Networks**. Add as many as you like;
each is saved locally and can reconnect automatically on launch.

## Add a network

Open **Settings → Networks → Add network** and fill in:

| Field | Required | Notes |
|---|---|---|
| **Name** | Yes | A label for the network (e.g. *Libera.Chat*). |
| **Server address** | Yes | Hostname or IP. You can add multiple servers and reorder them with the up/down arrows; they're tried in order. |
| **Port** | Yes | Defaults to `6667`. Use `6697` for TLS on most networks. |
| **TLS** | — | A per-server toggle. Enable it for encrypted connections. |
| **Nickname** | Yes | Your primary nick. |
| **Username** | No | The `user` part of your identity; defaults from the nick if blank. |
| **Realname** | No | Free-text "real name" / gecos field. |
| **Password** | No | A server (PASS) password, if the network requires one. |
| **Auto-connect** | — | Connect to this network automatically when Cascade starts. |

!!! tip "Multiple servers per network"
    Each network can hold an ordered list of servers, each with its own
    address, port, and TLS setting. Cascade tries them top to bottom, so you
    can list a primary server with TLS-enabled fallbacks.

## SASL authentication

SASL is the modern way to authenticate to a network account (and is required by
some networks before you can join channels). Enable **SASL** in the network form
and pick a **mechanism**:

=== "PLAIN"

    Username + password. The simplest option.

    !!! warning "Use TLS with PLAIN"
        PLAIN sends your credentials without encrypting them at the SASL layer.
        Only use it over a **TLS** connection (the form shows this warning too).

=== "SCRAM-SHA-256 / SCRAM-SHA-512"

    Username + password, but the password is never sent over the wire. A
    challenge-response handshake proves you know it. Prefer these over PLAIN
    when the network supports them.

=== "EXTERNAL"

    Certificate-based (CertFP). You authenticate with a **client
    certificate** instead of a password. Provide the certificate path (optional
    field) and make sure the connection uses TLS.

The underlying IRC client supports all four mechanisms: PLAIN, EXTERNAL,
SCRAM-SHA-256, and SCRAM-SHA-512.

## Nicknames and collisions

If your chosen nickname is already in use when you connect, Cascade handles it
for you. There's no separate "alternate nick" list to configure:

- While connecting, it automatically tries variations of your nick so the
  connection still succeeds. You'll see a one-time notice such as
  *"Nick `yournick` is in use — trying alternatives…"*.
- Once connected under a different nick, Cascade keeps trying to reclaim your
  preferred nick in the background and tells you so:
  *"Connected as `yournick_`. Cascade will keep trying to reclaim `yournick`
  automatically."* When the preferred nick frees up, it switches you back.
- If you run [`/nick`](commands.md) yourself and it fails, Cascade surfaces the
  exact reason (e.g. *"Couldn't change nick to `yournick` — it's already in
  use."*).

!!! note
    Nickname fallback is automatic and owned by the IRC layer. You configure a
    single preferred nickname; Cascade does the rest.
