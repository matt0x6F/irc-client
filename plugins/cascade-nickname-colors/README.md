# Nickname Colors Plugin

A test plugin for Cascade Chat that assigns consistent colors to nicknames in both the sidebar and chat messages.

## Features

- Assigns a deterministic color to each nickname based on a hash of the nickname
- Colors are consistent across sidebar and chat views
- Same nickname always gets the same color
- Automatically colors nicknames when they:
  - Join a channel
  - Send messages
  - Change their nickname

## Installation

The plugin is already built and installed in `~/.cascade-chat/plugins/cascade-nickname-colors`.

## How It Works

1. The plugin listens for IRC events (`message.received`, `user.joined`, `user.nick`)
2. When it sees a nickname, it calculates a color using MD5 hash of the nickname
3. It sends a `ui_metadata.set` notification to the backend with the color
4. The backend stores the color in the metadata registry
5. The frontend retrieves and displays the colors

## Color Palette

The plugin uses a palette of 16 distinct colors that are easy to distinguish:
- Red, Teal, Blue, Light Salmon, Mint, Yellow, Purple, Sky Blue
- Orange, Green, Coral, Light Blue, Pink, Turquoise, Gold, Lavender

## Testing

1. Start the Cascade Chat application
2. Connect to an IRC network
3. Join a channel or send messages
4. Observe that nicknames in the sidebar and chat have consistent colors

## Building

To rebuild the plugin:

```bash
cd plugins/cascade-nickname-colors
go build -o ~/.cascade-chat/plugins/cascade-nickname-colors main.go
chmod +x ~/.cascade-chat/plugins/cascade-nickname-colors
```
