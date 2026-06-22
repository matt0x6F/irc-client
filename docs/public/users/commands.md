# Commands & shortcuts

Type a `/` at the start of the message box to run a command. Anything that
isn't a command is sent as a normal message to the current channel or query.

## The `/help` command

`/help` is handled entirely inside Cascade — it never touches the server.

- **`/help`** — opens a searchable dialog listing every command, grouped by
  category (Client, Server, CTCP, and any commands added by plugins). You can
  switch this to print the list straight into the current buffer in
  **Settings**.
- **`/help <command>`** — prints usage for a single command, by name or alias.
  For example, `/help join` shows `/join #channel [key] — Join a channel`.

## Autocomplete

**Command completion** — start typing `/` and a popup appears below the input
listing matching commands with their usage and category:

- **↑ / ↓** to move through the suggestions
- **Enter** or **Tab** to accept the highlighted command
- **Esc** to dismiss the popup

Once you type a space after the command, the popup is replaced by a one-line
usage hint reminding you of the arguments.

**Nickname completion** — in a channel, type the start of someone's nick and
press **Tab**. Cascade completes it in place (adding a trailing `:` in
channels), and pressing **Tab** again cycles through other matches. There's no
popup for nick completion — it's pure inline text.

## Command reference

Many commands have shorter aliases, shown in parentheses.

### Channels & messaging

| Command | Usage | Description |
|---|---|---|
| `/join` (`/j`) | `#channel [key]` | Join a channel (with an optional key). |
| `/part` (`/leave`) | `#channel [reason]` | Leave a channel. |
| `/msg` (`/m`, `/privmsg`) | `target message` | Message a user or channel. |
| `/query` (`/q`) | `nickname [message]` | Open a private conversation. |
| `/me` (`/action`) | `[target] action text` | Send an action ("* you wave"). |
| `/notice` | `target message` | Send a notice. |
| `/topic` | `#channel [new topic]` | View or set the channel topic. |
| `/names` | `[#channel]` | List the users in a channel. |
| `/close` | `#channel \| nickname` | Close the current channel or query. |

### Identity & status

| Command | Usage | Description |
|---|---|---|
| `/nick` | `newnick` | Change your nickname. |
| `/away` | `[message]` | Set or clear your away status. |
| `/whois` | `nickname` | Look up information about a user. |
| `/whowas` | `nickname` | Look up a user who has left. |
| `/quit` | `[reason]` | Disconnect from the server. |

### Moderation

| Command | Usage | Description |
|---|---|---|
| `/mode` | `target modes [args]` | View or change modes. |
| `/op` (`/hop`) / `/deop` (`/dehop`) | `#channel nickname` | Grant or remove operator status. |
| `/voice` (`/v`) / `/devoice` (`/dev`) | `#channel nickname` | Grant or remove voice. |
| `/kick` | `#channel nickname [reason]` | Kick a user from a channel. |
| `/ban` / `/unban` | `#channel mask` | Add or remove a channel ban. |
| `/invite` | `nickname #channel` | Invite a user to a channel. |

### CTCP & raw

| Command | Usage | Description |
|---|---|---|
| `/ctcp` | `target command [args]` | Send a raw CTCP request. |
| `/ping` | `target` | CTCP-ping a user. |
| `/time` | `target` | Ask for a user's local time. |
| `/version` | `target` | Ask for a user's client version. |
| `/clientinfo` | `target` | Ask which CTCP commands a user supports. |
| `/list` | `[args]` | List channels on the network. |
| `/quote` (`/raw`) | `command [args]` | Send a raw IRC line to the server. |

!!! note
    Plugins can register their own commands. Those appear under a **Plugin**
    category in the `/help` dialog. See [Using plugins](plugins.md).

## Keyboard shortcuts

On macOS use **⌘**; on Windows and Linux use **Ctrl**.

### Global

| Shortcut | Action |
|---|---|
| **⌘/Ctrl + K** | Open message search |
| **⌘/Ctrl + ,** | Open Settings |
| **⌘/Ctrl + /** | Toggle the keyboard-shortcuts overlay |
| **⌘/Ctrl + B** | Toggle the left sidebar (networks & channels) |
| **⌘/Ctrl + Shift + B** | Toggle the right sidebar (users & pinned messages) |
| **⌘/Ctrl + Shift + N** | Jump keyboard focus into the network/channel tree |
| **Esc** | Close any open dialog |

### In the message box

| Shortcut | Action |
|---|---|
| **⌘/Ctrl + B** | Wrap the selected text in `*bold*` |
| **⌘/Ctrl + I** | Wrap the selected text in `_italic_` |
| **⌘/Ctrl + U** | Wrap the selected text in `__underline__` |
| **↑ / ↓** | Scroll back and forward through your input history |
| **Tab** | Nickname completion (see above) |

!!! tip "⌘/Ctrl + B does two things"
    With the message box focused and text selected, **⌘/Ctrl + B** formats that
    text as bold. Otherwise it toggles the left sidebar.
