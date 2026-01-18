# Plugin System

The Cascade Chat plugin system enables extensibility through external processes that communicate via JSON-RPC 2.0. Plugins can subscribe to events, provide UI metadata, and interact with the IRC client.

## Overview

Plugins are standalone executables that:
- Communicate via JSON-RPC 2.0 over stdin/stdout
- Subscribe to IRC and system events
- Provide UI metadata (colors, badges, icons)
- Can be written in any language
- Are discovered automatically from PATH or plugin directory

## Architecture

```
┌─────────────────┐
│  Cascade Chat   │
│                 │
│  ┌───────────┐  │
│  │EventBus   │  │
│  └─────┬─────┘  │
│        │        │
│  ┌─────▼─────┐  │
│  │  Plugin   │  │
│  │  Manager  │  │
│  └─────┬─────┘  │
└────────┼────────┘
         │ IPC (stdin/stdout)
         │ JSON-RPC 2.0
┌────────▼────────┐
│  Plugin Process │
│  (executable)   │
└─────────────────┘
```

## Plugin Discovery

Plugins are discovered from two locations:

1. **Plugin Directory**: `~/.cascade-chat/plugins/`
   - Can be a flat directory with executables
   - Can contain subdirectories with executables
   - Executables should be named `cascade-<plugin-name>`

2. **System PATH**: Any executable in PATH starting with `cascade-`

### Discovery Process

1. Scan plugin directory for executables
2. Scan PATH for `cascade-*` executables
3. Extract plugin name from filename (remove `cascade-` prefix)
4. Validate executable exists and is executable
5. Load plugin metadata from database (if available)

## Plugin Protocol

Plugins communicate using JSON-RPC 2.0 over stdin/stdout. Each message is a single JSON object on one line (newline-delimited).

### JSON-RPC Messages

#### Request (from Cascade to Plugin)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "version": "1.0",
    "capabilities": [],
    "config": {}
  }
}
```

#### Response (from Plugin to Cascade)

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "my-plugin",
    "version": "1.0.0",
    "description": "My plugin description",
    "author": "Plugin Author",
    "events": ["message.received", "user.joined"],
    "metadata_types": ["nickname_color"]
  }
}
```

#### Notification (no response expected)

```json
{
  "jsonrpc": "2.0",
  "method": "event",
  "params": {
    "type": "message.received",
    "data": {
      "networkId": 1,
      "channel": "#general",
      "user": "Alice",
      "message": "Hello!"
    }
  }
}
```

### Methods

#### `initialize` (Request)

Called when plugin is loaded. Plugin must respond with metadata.

**Parameters:**
- `version` (string): Protocol version
- `capabilities` ([]string): Server capabilities (typically empty)
- `config` (object): User configuration for the plugin

**Response:**
- `name` (string): Plugin name
- `version` (string): Plugin version
- `description` (string): Plugin description
- `author` (string): Plugin author
- `events` ([]string): Event types plugin subscribes to (use `["*"]` for all)
- `metadata_types` ([]string): Types of UI metadata plugin provides
- `config_schema` (object): JSON Schema for plugin configuration (optional)

**Example:**

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "name": "nickname-colors",
    "version": "1.0.0",
    "description": "Assigns consistent colors to nicknames",
    "author": "Cascade Chat",
    "events": ["*"],
    "metadata_types": ["nickname_color"]
  }
}
```

#### `event` (Notification)

Sent to plugin when subscribed events occur.

**Parameters:**
- `type` (string): Event type
- `data` (object): Event-specific data (see [Events Documentation](./events.md))

**Example:**

```json
{
  "jsonrpc": "2.0",
  "method": "event",
  "params": {
    "type": "message.received",
    "data": {
      "networkId": 1,
      "channel": "#general",
      "user": "Alice",
      "message": "Hello!",
      "timestamp": 1234567890
    }
  }
}
```

### Notifications from Plugin

#### `ui_metadata.set` (Notification)

Plugin sends this to set UI metadata (colors, badges, etc.).

**Parameters:**
- `type` (string): Metadata type (e.g., `"nickname_color"`)
- `key` (string): Metadata key (e.g., `"nickname:Alice"`)
- `value` (interface{}): Metadata value (e.g., `"#FF6B6B"`)
- `network_id` (int64, optional): Network-specific metadata
- `channel` (string, optional): Channel-specific metadata
- `priority` (int, optional): Priority (higher = more important, default: 0)

**Example:**

```json
{
  "jsonrpc": "2.0",
  "method": "ui_metadata.set",
  "params": {
    "type": "nickname_color",
    "key": "nickname:Alice",
    "value": "#FF6B6B",
    "network_id": 1,
    "priority": 0
  }
}
```

## Plugin Lifecycle

1. **Discovery**: Plugin discovered during startup or when enabled
2. **Validation**: Executable validated (exists, is executable)
3. **Initialization**: Plugin process started, `initialize` request sent
4. **Active**: Plugin receives events and can send metadata
5. **Unload**: Plugin process terminated, metadata cleared

### Loading a Plugin

```go
// Plugin manager automatically loads enabled plugins on startup
pm.DiscoverAndLoad()

// Or manually load a plugin
pm.LoadPlugin(pluginInfo)
```

### Unloading a Plugin

```go
// Unload and disable
pm.SetPluginEnabled("plugin-name", false, storage)

// Or just unload (keeps enabled state)
pm.UnloadPlugin("plugin-name")
```

## UI Metadata System

Plugins can provide UI metadata that enhances the display of IRC information.

### Metadata Types

- **`nickname_color`**: Color for nickname display (hex color string)
- **`nickname_badge`**: Badge/icon for nickname (future)
- **`nickname_icon`**: Icon for nickname (future)

### Metadata Scoping

Metadata can be scoped at three levels:

1. **Global**: Applies everywhere
   - No `network_id` or `channel` specified

2. **Network**: Applies to a specific network
   - `network_id` specified, no `channel`

3. **Channel**: Applies to a specific channel on a network
   - Both `network_id` and `channel` specified

### Metadata Priority

When multiple plugins set metadata for the same key:
- Higher priority wins
- On tie, newer metadata wins

### Setting Metadata

From a plugin:

```json
{
  "jsonrpc": "2.0",
  "method": "ui_metadata.set",
  "params": {
    "type": "nickname_color",
    "key": "nickname:Alice",
    "value": "#FF6B6B",
    "network_id": 1,
    "priority": 0
  }
}
```

### Retrieving Metadata

From the application:

```go
// Get single metadata value
color := pm.GetNicknameColor(networkID, "Alice")

// Get batch of metadata values
colors := pm.GetNicknameColorsBatch(networkID, []string{"Alice", "Bob"})
```

## Writing a Plugin

### Basic Structure

A plugin must:
1. Read JSON-RPC messages from stdin (line-delimited)
2. Write JSON-RPC messages to stdout (line-delimited)
3. Handle `initialize` request
4. Handle `event` notifications
5. Send `ui_metadata.set` notifications as needed

### Example Plugin (Go)

```go
package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"
)

type Request struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      interface{} `json:"id,omitempty"`
    Method  string      `json:"method"`
    Params  interface{} `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string      `json:"jsonrpc"`
    ID      interface{} `json:"id,omitempty"`
    Result  interface{} `json:"result,omitempty"`
    Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

type EventParams struct {
    Type string                 `json:"type"`
    Data map[string]interface{} `json:"data"`
}

func sendResponse(id interface{}, result interface{}) {
    resp := Response{
        JSONRPC: "2.0",
        ID:      id,
        Result:  result,
    }
    data, _ := json.Marshal(resp)
    os.Stdout.Write(append(data, '\n'))
}

func sendNotification(method string, params interface{}) {
    req := Request{
        JSONRPC: "2.0",
        Method:  method,
        Params:  params,
    }
    data, _ := json.Marshal(req)
    os.Stdout.Write(append(data, '\n'))
}

func main() {
    reader := bufio.NewReader(os.Stdin)
    
    for {
        line, err := reader.ReadString('\n')
        if err != nil {
            break
        }
        
        var req Request
        if err := json.Unmarshal([]byte(line), &req); err != nil {
            continue
        }
        
        switch req.Method {
        case "initialize":
            sendResponse(req.ID, map[string]interface{}{
                "name":          "my-plugin",
                "version":       "1.0.0",
                "description":   "My plugin",
                "author":        "Me",
                "events":        []string{"message.received"},
                "metadata_types": []string{"nickname_color"},
            })
            
        case "event":
            paramsBytes, _ := json.Marshal(req.Params)
            var eventParams EventParams
            json.Unmarshal(paramsBytes, &eventParams)
            
            if eventParams.Type == "message.received" {
                // Process message and set metadata
                if user, ok := eventParams.Data["user"].(string); ok {
                    sendNotification("ui_metadata.set", map[string]interface{}{
                        "type":  "nickname_color",
                        "key":   "nickname:" + user,
                        "value": "#FF6B6B",
                    })
                }
            }
        }
    }
}
```

### Example Plugin (Python)

```python
import json
import sys

def send_response(id, result):
    resp = {
        "jsonrpc": "2.0",
        "id": id,
        "result": result
    }
    print(json.dumps(resp), flush=True)

def send_notification(method, params):
    req = {
        "jsonrpc": "2.0",
        "method": method,
        "params": params
    }
    print(json.dumps(req), flush=True)

for line in sys.stdin:
    line = line.strip()
    if not line:
        continue
    
    req = json.loads(line)
    
    if req.get("method") == "initialize":
        send_response(req.get("id"), {
            "name": "my-plugin",
            "version": "1.0.0",
            "description": "My plugin",
            "author": "Me",
            "events": ["message.received"],
            "metadata_types": ["nickname_color"]
        })
    
    elif req.get("method") == "event":
        params = req.get("params", {})
        if params.get("type") == "message.received":
            data = params.get("data", {})
            user = data.get("user")
            if user:
                send_notification("ui_metadata.set", {
                    "type": "nickname_color",
                    "key": f"nickname:{user}",
                    "value": "#FF6B6B"
                })
```

## Plugin Configuration

Plugins can define a configuration schema using JSON Schema. The schema is stored in the database and used to generate configuration UI.

### Config Schema Example

```json
{
  "type": "object",
  "properties": {
    "color_palette": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "default": ["#FF6B6B", "#4ECDC4", "#45B7D1"]
    },
    "enable_badges": {
      "type": "boolean",
      "default": true
    }
  }
}
```

### Accessing Configuration

Configuration is passed to the plugin during initialization:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "initialize",
  "params": {
    "version": "1.0",
    "capabilities": [],
    "config": {
      "color_palette": ["#FF6B6B", "#4ECDC4"],
      "enable_badges": true
    }
  }
}
```

## Plugin Management

### Enabling/Disabling Plugins

Plugins can be enabled or disabled through the UI or API. The state is persisted in the database.

```go
// Enable a plugin
pm.SetPluginEnabled("plugin-name", true, storage)

// Disable a plugin
pm.SetPluginEnabled("plugin-name", false, storage)
```

### Listing Plugins

```go
plugins := pm.ListPlugins()
// Returns PluginInfo for all discovered plugins (loaded and unloaded)
```

## Best Practices

1. **Error Handling**: Always handle errors gracefully, don't crash
2. **Logging**: Use stderr for debug logs (they're captured by Cascade)
3. **Buffering**: Flush stdout after each message (or use line buffering)
4. **Event Filtering**: Subscribe only to events you need (avoid `["*"]` if possible)
5. **Metadata Keys**: Use consistent key formats (e.g., `"nickname:Alice"`)
6. **Configuration**: Provide sensible defaults in config schema
7. **Versioning**: Include version in plugin metadata
8. **Graceful Shutdown**: Exit cleanly when stdin closes (EOF)

## Troubleshooting

### Plugin Not Loading

- Check executable permissions: `chmod +x cascade-my-plugin`
- Verify plugin is in PATH or plugin directory
- Check plugin implements `initialize` correctly
- Review stderr output for errors

### Plugin Not Receiving Events

- Verify plugin subscribes to event type in `initialize` response
- Check event type spelling matches exactly
- Review plugin manager logs

### Metadata Not Appearing

- Verify `ui_metadata.set` notification format
- Check metadata key format matches retrieval pattern
- Verify network_id/channel scope matches query
- Check metadata priority isn't being overridden

### Plugin Crashes

- Review stderr output
- Ensure plugin handles all expected event types
- Check for JSON parsing errors
- Verify plugin exits cleanly on EOF

## Future Enhancements

- Plugin actions (send message, join channel, etc.)
- Plugin UI components
- Plugin-to-plugin communication
- Plugin sandboxing/security
- Plugin marketplace
- Hot reloading
- Plugin dependencies
