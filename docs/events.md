# Events System

The Cascade Chat events system provides a centralized, event-driven architecture for communication between components. All IRC activities, system events, and UI interactions flow through the EventBus.

## Overview

The events system uses a publisher-subscriber pattern where:
- **Publishers** emit events to the EventBus
- **Subscribers** register interest in specific event types
- **EventBus** routes events to all relevant subscribers asynchronously

## Architecture

```
┌─────────────┐
│ IRC Client  │───┐
└─────────────┘   │
                  │
┌─────────────┐   │    ┌──────────────┐
│   System    │───┼───▶│  EventBus    │
└─────────────┘   │    └──────┬───────┘
                  │           │
┌─────────────┐   │           │
│     UI      │───┘           │
└─────────────┘               │
                              ▼
                    ┌──────────────────┐
                    │   Subscribers    │
                    ├──────────────────┤
                    │ Plugin Manager   │
                    │ Storage (future) │
                    │ Frontend (future)│
                    └──────────────────┘
```

## Event Structure

All events follow this structure:

```go
type Event struct {
    Type      string                 // Event type identifier
    Data      map[string]interface{} // Event-specific data
    Timestamp time.Time              // When the event occurred
    Source    EventSource            // Source of the event
}
```

### Event Sources

- `irc`: Events from IRC protocol interactions
- `ui`: Events from user interface (future)
- `system`: Events from system components

## Event Types

### IRC Events

All IRC events are defined in `internal/irc/events.go`:

#### Connection Events

- **`connection.established`**: Emitted when successfully connected to an IRC server
  - `networkId` (int64): Network ID
  - `address` (string): Server address
  - `port` (int): Server port

- **`connection.lost`**: Emitted when connection to server is lost
  - `networkId` (int64): Network ID
  - `error` (string): Error message if available

#### Message Events

- **`message.received`**: Emitted when a message is received
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name (empty for PMs)
  - `user` (string): Sender nickname
  - `message` (string): Message content
  - `timestamp` (int64): Unix timestamp

- **`message.sent`**: Emitted when a message is sent
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name (empty for PMs)
  - `message` (string): Message content

#### User Events

- **`user.joined`**: Emitted when a user joins a channel
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name
  - `user` or `nickname` (string): User nickname

- **`user.parted`**: Emitted when a user leaves a channel
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name
  - `user` or `nickname` (string): User nickname
  - `reason` (string): Part reason (optional)

- **`user.quit`**: Emitted when a user quits the server
  - `networkId` (int64): Network ID
  - `user` or `nickname` (string): User nickname
  - `reason` (string): Quit reason

- **`user.kicked`**: Emitted when a user is kicked from a channel
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name
  - `user` or `nickname` (string): Kicked user
  - `kicker` (string): User who performed the kick
  - `reason` (string): Kick reason

- **`user.nick`**: Emitted when a user changes nickname
  - `networkId` (int64): Network ID
  - `old_nickname` (string): Previous nickname
  - `nickname` (string): New nickname

#### Channel Events

- **`channel.topic`**: Emitted when channel topic changes
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name
  - `topic` (string): New topic
  - `setter` (string): User who set the topic (optional)

- **`channel.mode`**: Emitted when channel modes change
  - `networkId` (int64): Network ID
  - `channel` (string): Channel name
  - `modes` (string): Mode string (e.g., "+o user")
  - `setter` (string): User who set the mode (optional)

- **`channels.changed`**: Emitted when channel list changes
  - `networkId` (int64): Network ID
  - `channels` ([]string): List of channel names

#### SASL Authentication Events

- **`sasl.started`**: Emitted when SASL authentication begins
  - `networkId` (int64): Network ID
  - `mechanism` (string): SASL mechanism (e.g., "PLAIN", "SCRAM-SHA-256")

- **`sasl.success`**: Emitted when SASL authentication succeeds
  - `networkId` (int64): Network ID

- **`sasl.failed`**: Emitted when SASL authentication fails
  - `networkId` (int64): Network ID
  - `error` (string): Error message

- **`sasl.aborted`**: Emitted when SASL authentication is aborted
  - `networkId` (int64): Network ID

#### Other Events

- **`whois.received`**: Emitted when WHOIS information is received
  - `networkId` (int64): Network ID
  - `whois` (WhoisInfo): Parsed WHOIS information (see `internal/irc/events.go`)

- **`error`**: Emitted when an error occurs
  - `networkId` (int64): Network ID (optional)
  - `error` (string): Error message
  - `type` (string): Error type (optional)

### System Events

- **`metadata.updated`**: Emitted when plugin metadata is updated
  - `type` (string): Metadata type (e.g., "nickname_color")
  - `key` (string): Metadata key
  - `value` (interface{}): Metadata value
  - `network_id` (int64): Network ID (optional)
  - `channel` (string): Channel name (optional)

### UI Events (Future)

- **`ui.pane.focused`**: Emitted when a pane gains focus
- **`ui.pane.blurred`**: Emitted when a pane loses focus

## Using the EventBus

### Subscribing to Events

```go
// Subscribe to a specific event type
eventBus.Subscribe("message.received", mySubscriber)

// Subscribe to all events (wildcard)
eventBus.Subscribe("*", mySubscriber)
```

### Implementing a Subscriber

Any type implementing the `Subscriber` interface can receive events:

```go
type MySubscriber struct{}

func (s *MySubscriber) OnEvent(event events.Event) {
    // Handle the event
    switch event.Type {
    case "message.received":
        // Process message
    }
}
```

### Emitting Events

```go
eventBus.Emit(events.Event{
    Type:      "message.received",
    Data: map[string]interface{}{
        "networkId": 1,
        "channel":   "#general",
        "user":      "Alice",
        "message":   "Hello!",
    },
    Timestamp: time.Now(),
    Source:    events.EventSourceIRC,
})
```

### Synchronous Events

For testing or when order matters, use `EmitSync`:

```go
eventBus.EmitSync(event)
```

**Note**: Synchronous emission blocks until all subscribers process the event. Use sparingly.

## Event Flow Example

1. **IRC Client** receives a PRIVMSG from the server
2. **IRC Client** emits `message.received` event to EventBus
3. **EventBus** routes event to all subscribers:
   - **Plugin Manager** forwards to plugins subscribed to `message.received`
   - **Storage** (future) saves message to database
   - **Frontend** (future) updates UI

## Best Practices

1. **Event Naming**: Use dot-separated hierarchical names (e.g., `user.joined`, `channel.mode`)
2. **Event Data**: Include all relevant context (networkId, channel, etc.)
3. **Async Processing**: Events are delivered asynchronously - don't block in `OnEvent`
4. **Error Handling**: Handle errors gracefully in subscribers
5. **Wildcard Subscriptions**: Use sparingly - prefer specific event types for performance

## Thread Safety

The EventBus is fully thread-safe:
- Subscriptions/unsubscriptions are protected by mutex
- Event emission copies subscriber lists to avoid blocking
- Each subscriber receives events in a separate goroutine

## Future Enhancements

- Event filtering by data fields
- Event replay for debugging
- Event metrics and monitoring
- Priority-based event delivery
- Event batching for high-frequency events
