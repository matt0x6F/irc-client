# Testing the Plugin System

## Understanding the Architecture

The frontend changes we made are the **Plugin API infrastructure** - they provide the mechanism for plugins to enhance the UI. Think of it like this:

- **Frontend changes** = The "plugin API" that allows plugins to provide UI metadata
- **Plugin executable** = The actual plugin that provides the colors dynamically

Once the infrastructure is in place, **any plugin can provide colors without further frontend changes**. The plugin system is extensible - you could add plugins for badges, icons, tooltips, etc., all using the same infrastructure.

## How to Test

1. **Check if plugin is loaded:**
   - Open the application
   - Check the console/logs for plugin loading messages
   - The plugin should be discovered from `~/.cascade-chat/plugins/cascade-nickname-colors`

2. **Check plugin output:**
   - The plugin writes debug messages to stderr
   - Look for messages like `[nickname-colors] Received event:` and `[nickname-colors] Setting color for:`

3. **Check backend logs:**
   - Look for `Received ui_metadata.set notification` messages
   - Look for `Stored metadata` messages

4. **Check frontend console:**
   - Look for `[useNicknameColors] Fetched colors:` messages
   - Check if colors are being retrieved from the backend

5. **Trigger events:**
   - Join a channel (triggers `user.joined` event)
   - Send/receive messages (triggers `message.received` event)
   - The plugin should receive these events and set colors

## Troubleshooting

If colors aren't showing:

1. **Plugin not loading?**
   - Check if `~/.cascade-chat/plugins/cascade-nickname-colors` exists and is executable
   - Check application logs for plugin discovery/loading errors

2. **Plugin not receiving events?**
   - Check if plugin.json has `"events": ["*"]`
   - Check backend logs for event routing

3. **Plugin not sending metadata?**
   - Check plugin stderr output for debug messages
   - Verify the plugin is receiving events and processing them

4. **Metadata not reaching frontend?**
   - Check backend logs for `Received ui_metadata.set notification`
   - Check if `metadata-updated` events are being emitted
   - Check frontend console for `[useNicknameColors] Fetched colors:`
