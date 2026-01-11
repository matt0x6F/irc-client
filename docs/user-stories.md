# User Stories - Cascade IRC Client

This document contains user stories for features missing from the current implementation. Stories are organized by feature area and include acceptance criteria to guide development work.

## Table of Contents

1. [Core IRC Protocol Features](#core-irc-protocol-features)
2. [User Experience Features](#user-experience-features)
3. [Channel & Server Management](#channel--server-management)
4. [Connection Management](#connection-management)
5. [Message Management](#message-management)
6. [UI/UX Enhancements](#uiux-enhancements)
7. [Advanced Features](#advanced-features)
8. [Data Management](#data-management)
9. [Security Features](#security-features)

---

## Core IRC Protocol Features

### US-001: DCC File Transfer Support

**As a** user  
**I want** to send and receive files via DCC (Direct Client-to-Client)  
**So that** I can share files directly with other IRC users without external services

**Acceptance Criteria:**
- [ ] Support DCC SEND for sending files
- [ ] Support DCC GET for receiving files
- [ ] Display file transfer progress UI
- [ ] Support DCC RESUME for interrupted transfers
- [ ] Handle firewall/NAT scenarios (passive DCC)
- [ ] Show file transfer notifications
- [ ] Allow canceling active transfers
- [ ] Store received files in configurable directory
- [ ] Display file size and transfer speed

**Priority:** High  
**Estimated Effort:** Large

---

### US-002: DCC Chat Support

**As a** user  
**I want** to use DCC CHAT for direct encrypted chat  
**So that** I can have private conversations outside of IRC channels

**Acceptance Criteria:**
- [ ] Support initiating DCC CHAT connections
- [ ] Accept incoming DCC CHAT requests
- [ ] Display DCC chat in separate window/panel
- [ ] Show connection status for DCC chat
- [ ] Handle DCC chat disconnections gracefully

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-003: Extended CTCP Support

**As a** user  
**I want** to use CTCP commands beyond ACTION  
**So that** I can query user information and client details

**Acceptance Criteria:**
- [x] Support CTCP VERSION requests/responses
- [x] Support CTCP TIME requests/responses
- [x] Support CTCP PING requests/responses
- [x] Support CTCP CLIENTINFO requests/responses
- [x] Display CTCP responses in UI
- [x] Allow sending CTCP commands via UI or commands
- [x] Handle CTCP replies automatically

**Priority:** Low  
**Estimated Effort:** Small

---

### US-004: Private Message Windows

**As a** user  
**I want** dedicated windows/panels for private messages  
**So that** I can easily manage and view private conversations separately from channels

**Acceptance Criteria:**
- [x] Create separate UI for private messages
- [x] Show list of active private message conversations
- [x] Display private messages in dedicated view
- [x] Show unread indicators for private messages
- [x] Allow opening/closing private message windows
- [x] Support multiple simultaneous private message conversations
- [x] Display user info in private message header
- [x] Store private message history separately

**Priority:** High  
**Estimated Effort:** Medium

---

### US-005: WHOIS/WHOWAS Results Display

**As a** user  
**I want** to see parsed WHOIS/WHOWAS results in a user-friendly format  
**So that** I can view user information without reading raw IRC responses

**Acceptance Criteria:**
- [x] Parse WHOIS numeric responses (311, 312, 313, 317, 318, etc.)
- [x] Display user info panel with:
  - Nickname, username, hostmask
  - Real name
  - Server information
  - Channels user is in
  - Idle time
  - Account name (if available)
  - Sign-on time
- [ ] Parse WHOWAS responses similarly
- [x] Show user info panel on right-click context menu
- [x] Display user info in private message header
- [x] Cache recent WHOIS results

**Priority:** Medium  
**Estimated Effort:** Medium

---

## User Experience Features

### US-006: System Notifications

**As a** user  
**I want** to receive system notifications for important events  
**So that** I can be alerted when away from the application

**Acceptance Criteria:**
- [ ] Show desktop notifications for:
  - Private messages
  - Mentions (nickname in channel)
  - Highlighted keywords
  - Channel invitations
  - Connection status changes
- [ ] Allow configuring which events trigger notifications
- [ ] Support notification preferences per network/channel
- [ ] Respect system "Do Not Disturb" mode
- [ ] Show notification count in dock/taskbar
- [ ] Allow clicking notification to focus relevant window

**Priority:** High  
**Estimated Effort:** Medium

---

### US-007: Sound Notifications

**As a** user  
**I want** to hear sound alerts for important events  
**So that** I can be notified even when not looking at the screen

**Acceptance Criteria:**
- [ ] Play sounds for configurable events
- [ ] Allow custom sound files per event type
- [ ] Support volume control
- [ ] Allow muting sounds
- [ ] Provide default sounds for common events
- [ ] Support per-network/channel sound settings

**Priority:** Medium  
**Estimated Effort:** Small

---

### US-008: Highlighting and Keyword Notifications

**As a** user  
**I want** to highlight messages containing specific keywords  
**So that** I can quickly identify important messages

**Acceptance Criteria:**
- [ ] Allow configuring highlight keywords/patterns
- [ ] Support regex patterns for highlights
- [ ] Highlight matching messages visually (background color, bold, etc.)
- [ ] Trigger notifications for highlighted messages
- [ ] Support case-sensitive/insensitive matching
- [ ] Show highlight indicator in channel list
- [ ] Allow per-channel highlight rules
- [ ] Support wildcard patterns

**Priority:** High  
**Estimated Effort:** Medium

---

### US-009: Ignore List Management

**As a** user  
**I want** to ignore messages from specific users or hostmasks  
**So that** I can filter out unwanted messages

**Acceptance Criteria:**
- [ ] Add users to ignore list by nickname
- [ ] Add hostmasks to ignore list (user!ident@host)
- [ ] Support wildcard patterns in ignore masks
- [ ] Show ignore list in settings
- [ ] Allow temporary ignores (until disconnect)
- [ ] Allow permanent ignores (saved to database)
- [ ] Show ignored message count indicator
- [ ] Option to show ignored messages in muted/transparent form
- [ ] Support per-network ignore lists

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-010: Nickname Tab Completion

**As a** user  
**I want** to use tab completion for nicknames in the input field  
**So that** I can quickly mention users without typing full nicknames

**Acceptance Criteria:**
- [ ] Complete nicknames when pressing Tab
- [ ] Cycle through matching nicknames on repeated Tab presses
- [ ] Complete from current channel user list
- [ ] Support partial nickname matching
- [ ] Case-insensitive matching
- [ ] Show completion suggestions dropdown
- [ ] Complete with @ prefix for mentions (if supported by server)

**Priority:** Medium  
**Estimated Effort:** Small

---

### US-011: Command History

**As a** user  
**I want** to navigate through previously entered commands  
**So that** I can reuse commands without retyping

**Acceptance Criteria:**
- [ ] Store command history per input field
- [ ] Navigate history with Up/Down arrow keys
- [ ] Persist command history across sessions
- [ ] Support history search (Ctrl+R or similar)
- [ ] Limit history size (configurable)
- [ ] Clear history option in settings
- [ ] Separate history for channels vs status window

**Priority:** Low  
**Estimated Effort:** Small

---

### US-012: Away Message Support

**As a** user  
**I want** to set an away message  
**So that** others know when I'm not available

**Acceptance Criteria:**
- [ ] Set away message via command or UI
- [ ] Send AWAY command to server
- [ ] Display away status indicator
- [ ] Auto-reply to private messages with away message
- [ ] Support auto-away (set away after idle time)
- [ ] Show away status in user list
- [ ] Remove away status when returning
- [ ] Support per-network away messages

**Priority:** Medium  
**Estimated Effort:** Small

---

## Channel & Server Management

### US-013: Channel List Browsing

**As a** user  
**I want** to browse available channels on a server  
**So that** I can discover and join new channels

**Acceptance Criteria:**
- [ ] Send LIST command to server
- [ ] Parse LIST responses (322 numeric)
- [ ] Display channel list in searchable table/grid
- [ ] Show channel name, user count, topic
- [ ] Filter channels by name, user count, topic
- [ ] Sort channels by name, user count
- [ ] Join channel directly from list
- [ ] Refresh channel list
- [ ] Handle large channel lists efficiently
- [ ] Show channel modes in list

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-014: Ban List Management

**As a** user with channel operator privileges  
**I want** to view and manage channel ban lists  
**So that** I can moderate channels effectively

**Acceptance Criteria:**
- [ ] Display channel ban list (MODE +b responses)
- [ ] Show ban masks in list
- [ ] Add bans via UI (right-click user or manual entry)
- [ ] Remove bans via UI
- [ ] Support ban exception list (+e)
- [ ] Support invite exception list (+I)
- [ ] Support quiet list (+q)
- [ ] Display all lists in channel info panel
- [ ] Show who set each ban (if server supports)
- [ ] Support ban expiration (if server supports)

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-015: Channel Key Management

**As a** user  
**I want** the client to remember channel keys  
**So that** I can auto-join password-protected channels

**Acceptance Criteria:**
- [ ] Store channel keys securely (encrypted)
- [ ] Auto-use key when joining channel
- [ ] Prompt for key if not stored
- [ ] Update key if channel key changes
- [ ] Show key indicator in channel list
- [ ] Allow manual key entry/editing
- [ ] Support per-network key storage

**Priority:** Low  
**Estimated Effort:** Small

---

### US-016: Server List Export/Import

**As a** user  
**I want** to export and import network configurations  
**So that** I can backup settings or share server lists

**Acceptance Criteria:**
- [ ] Export network configurations to JSON/YAML
- [ ] Import network configurations from file
- [ ] Support partial imports (merge with existing)
- [ ] Validate imported configurations
- [ ] Handle password encryption in exports
- [ ] Support export/import from other IRC clients (compatibility)
- [ ] Show import preview before applying

**Priority:** Low  
**Estimated Effort:** Medium

---

## Connection Management

### US-017: Auto-Reconnect with Exponential Backoff

**As a** user  
**I want** the client to automatically reconnect when disconnected  
**So that** I don't have to manually reconnect after network issues

**Acceptance Criteria:**
- [ ] Detect disconnections automatically
- [ ] Attempt reconnection with exponential backoff
- [ ] Configurable max retry attempts
- [ ] Configurable initial retry delay
- [ ] Show reconnection status in UI
- [ ] Cancel reconnection attempts on user request
- [ ] Restore channel joins after reconnection
- [ ] Handle authentication (SASL) on reconnect
- [ ] Log reconnection attempts

**Priority:** High  
**Estimated Effort:** Medium

---

### US-018: SSL/TLS Certificate Management

**As a** user  
**I want** to manage SSL/TLS certificates for connections  
**So that** I can handle self-signed certificates and ensure security

**Acceptance Criteria:**
- [ ] Prompt user when encountering unknown certificates
- [ ] Allow accepting/rejecting certificates
- [ ] Store accepted certificates
- [ ] Support certificate pinning
- [ ] Show certificate details (issuer, expiration, etc.)
- [ ] Warn about expired certificates
- [ ] Support custom CA certificates
- [ ] Allow viewing/removing stored certificates
- [ ] Show security indicators in UI (lock icon, etc.)

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-019: Proxy Support

**As a** user  
**I want** to connect through a proxy server  
**So that** I can use IRC from restricted networks

**Acceptance Criteria:**
- [ ] Support SOCKS5 proxy
- [ ] Support HTTP proxy
- [ ] Configure proxy per network
- [ ] Support proxy authentication
- [ ] Test proxy connection before use
- [ ] Show proxy status in connection info
- [ ] Support system proxy settings

**Priority:** Low  
**Estimated Effort:** Medium

---

### US-020: Nickname Collision Handling

**As a** user  
**I want** the client to handle nickname collisions automatically  
**So that** I can connect even if my preferred nickname is taken

**Acceptance Criteria:**
- [ ] Detect nickname collision (433 ERR_NICKNAMEINUSE)
- [ ] Automatically append numbers to nickname
- [ ] Support configurable fallback nickname patterns
- [ ] Try multiple fallback nicknames
- [ ] Notify user of nickname change
- [ ] Allow manual nickname change after collision
- [ ] Store successful nickname for future use

**Priority:** Low  
**Estimated Effort:** Small

---

## Message Management

### US-021: Message Search

**As a** user  
**I want** to search through message history  
**So that** I can find previous conversations

**Acceptance Criteria:**
- [ ] Full-text search across messages
- [ ] Search within current channel/network
- [ ] Search across all channels/networks
- [ ] Filter by date range
- [ ] Filter by user
- [ ] Filter by message type
- [ ] Highlight search results
- [ ] Navigate between search results
- [ ] Support regex search
- [ ] Case-sensitive/insensitive options
- [ ] Show search result count

**Priority:** High  
**Estimated Effort:** Medium

---

### US-022: Message Logging and Export

**As a** user  
**I want** to export message logs  
**So that** I can archive conversations or use them externally

**Acceptance Criteria:**
- [ ] Export messages to plain text
- [ ] Export messages to HTML (formatted)
- [ ] Export messages to JSON
- [ ] Select date range for export
- [ ] Export per channel or all channels
- [ ] Include timestamps in exports
- [ ] Preserve IRC formatting in HTML export
- [ ] Support log rotation (auto-archive old logs)
- [ ] Configure logging per channel
- [ ] Choose log file location

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-023: Message Filtering

**As a** user  
**I want** to filter messages by type  
**So that** I can focus on relevant content

**Acceptance Criteria:**
- [ ] Filter by message type (join, part, quit, kick, etc.)
- [ ] Show/hide specific message types
- [ ] Create custom filter rules
- [ ] Apply filters per channel
- [ ] Save filter presets
- [ ] Show filtered message count
- [ ] Quick filter buttons in UI

**Priority:** Low  
**Estimated Effort:** Small

---

### US-024: Timestamp Customization

**As a** user  
**I want** to customize how timestamps are displayed  
**So that** I can view times in my preferred format

**Acceptance Criteria:**
- [ ] Configure timestamp format (12/24 hour, date format, etc.)
- [ ] Show/hide timestamps
- [ ] Support relative timestamps ("2 minutes ago")
- [ ] Support timezone display
- [ ] Apply timestamp settings globally or per-channel
- [ ] Preview timestamp format in settings

**Priority:** Low  
**Estimated Effort:** Small

---

## UI/UX Enhancements

### US-025: Customizable Themes

**As a** user  
**I want** to customize the application theme and colors  
**So that** I can personalize the interface

**Acceptance Criteria:**
- [ ] Support multiple color themes (light, dark, custom)
- [ ] Allow custom color schemes
- [ ] Customize font family and size
- [ ] Customize message colors
- [ ] Preview theme changes
- [ ] Export/import theme configurations
- [ ] Support system theme detection
- [ ] Per-network theme support (optional)

**Priority:** Low  
**Estimated Effort:** Medium

---

### US-026: Unread Message Indicators

**As a** user  
**I want** to see unread message counts and indicators  
**So that** I know which channels have new activity

**Acceptance Criteria:**
- [ ] Show unread count badge on channels
- [ ] Mark messages as read when channel is viewed
- [ ] Show activity indicators (partially implemented)
- [ ] Distinguish between unread and mentions
- [ ] Show total unread count in window title
- [ ] Support "mark all as read" action
- [ ] Configurable unread threshold

**Priority:** Medium  
**Estimated Effort:** Small

---

### US-027: Split View / Multiple Channels

**As a** user  
**I want** to view multiple channels simultaneously  
**So that** I can monitor multiple conversations at once

**Acceptance Criteria:**
- [ ] Split view horizontally or vertically
- [ ] Show multiple channels side-by-side
- [ ] Independent scrolling per pane
- [ ] Resizable panes
- [ ] Switch focus between panes
- [ ] Close individual panes
- [ ] Remember split configuration

**Priority:** Low  
**Estimated Effort:** Large

---

### US-028: Message Formatting Toolbar

**As a** user  
**I want** a toolbar for formatting messages  
**So that** I can easily apply IRC formatting codes

**Acceptance Criteria:**
- [ ] Toolbar buttons for bold, italic, underline, color
- [ ] Color picker for foreground/background
- [ ] Preview formatted text before sending
- [ ] Insert formatting codes at cursor position
- [ ] Toggle toolbar visibility
- [ ] Support keyboard shortcuts for formatting

**Priority:** Low  
**Estimated Effort:** Small

---

### US-029: User Info Panel

**As a** user  
**I want** to view detailed information about users  
**So that** I can see their status and activity

**Acceptance Criteria:**
- [ ] Display user info panel (WHOIS results)
- [ ] Show user's channels
- [ ] Show idle time
- [ ] Show account information
- [ ] Show hostmask
- [ ] Show sign-on time
- [ ] Open panel from user list or context menu
- [ ] Refresh user info
- [ ] Cache user info

**Priority:** Medium  
**Estimated Effort:** Small

---

### US-030: Server Statistics Display

**As a** user  
**I want** to see connection statistics  
**So that** I can monitor my IRC usage

**Acceptance Criteria:**
- [ ] Display connection time
- [ ] Show message counts (sent/received)
- [ ] Show channel count
- [ ] Show network/server information
- [ ] Display in status window or separate panel
- [ ] Reset statistics option
- [ ] Export statistics

**Priority:** Low  
**Estimated Effort:** Small

---

## Advanced Features

### US-031: Bouncer/ZNC Support

**As a** user  
**I want** to connect to IRC bouncers like ZNC  
**So that** I can access server-side history and use multiple clients

**Acceptance Criteria:**
- [ ] Support ZNC authentication
- [ ] Playback server-side message history
- [ ] Handle bouncer-specific features
- [ ] Support multiple client connections
- [ ] Display bouncer status
- [ ] Support ZNC modules (if applicable)

**Priority:** Low  
**Estimated Effort:** Large

---

### US-032: Scripting Support

**As a** user  
**I want** to create scripts for automation  
**So that** I can customize client behavior beyond plugins

**Acceptance Criteria:**
- [ ] Support alias commands
- [ ] Support trigger/action system
- [ ] Support scripting language (Lua, JavaScript, etc.)
- [ ] Allow scripts to access IRC events
- [ ] Allow scripts to send commands
- [ ] Script management UI
- [ ] Script debugging tools
- [ ] Example scripts library

**Priority:** Low  
**Estimated Effort:** Large

---

### US-033: Multiple Identity Support

**As a** user  
**I want** to use multiple nicknames/identities on the same network  
**So that** I can switch between different personas

**Acceptance Criteria:**
- [ ] Configure multiple identities per network
- [ ] Quick identity switching
- [ ] Store identity configurations
- [ ] Support different SASL credentials per identity
- [ ] Show current identity in UI
- [ ] Remember last used identity

**Priority:** Low  
**Estimated Effort:** Medium

---

## Data Management

### US-034: Settings Export/Import

**As a** user  
**I want** to export and import all settings  
**So that** I can backup configuration or sync across devices

**Acceptance Criteria:**
- [ ] Export all settings to file
- [ ] Import settings from file
- [ ] Selective import (choose which settings to import)
- [ ] Validate imported settings
- [ ] Backup settings automatically
- [ ] Show settings diff before import
- [ ] Support encrypted settings export

**Priority:** Medium  
**Estimated Effort:** Medium

---

### US-035: Database Maintenance

**As a** user  
**I want** to maintain the message database  
**So that** I can optimize performance and manage storage

**Acceptance Criteria:**
- [ ] Vacuum/optimize database
- [ ] Show database size
- [ ] Cleanup old messages (configurable retention)
- [ ] Archive old messages
- [ ] Rebuild database indexes
- [ ] Database integrity check
- [ ] Export database for backup
- [ ] Import database from backup

**Priority:** Low  
**Estimated Effort:** Medium

---

### US-036: Message Retention Policies

**As a** user  
**I want** to configure how long messages are stored  
**So that** I can manage database size

**Acceptance Criteria:**
- [ ] Configure message retention period
- [ ] Per-channel retention settings
- [ ] Auto-delete old messages
- [ ] Archive messages before deletion
- [ ] Show messages scheduled for deletion
- [ ] Manual cleanup option
- [ ] Retention policy exceptions

**Priority:** Low  
**Estimated Effort:** Medium

---

## Security Features

### US-037: Enhanced Secure Password Storage

**As a** user  
**I want** my passwords stored securely  
**So that** my credentials are protected

**Acceptance Criteria:**
- [ ] Use system keychain for password storage (partially implemented)
- [ ] Encrypt passwords in database
- [ ] Support master password for encryption
- [ ] Clear passwords from memory after use
- [ ] Audit password access
- [ ] Support password managers integration

**Priority:** Medium  
**Estimated Effort:** Small

---

### US-038: Connection Security Indicators

**As a** user  
**I want** to see security status of connections  
**So that** I know when connections are secure

**Acceptance Criteria:**
- [ ] Show TLS/SSL indicator (lock icon)
- [ ] Display encryption strength
- [ ] Warn about insecure connections
- [ ] Show certificate information
- [ ] Highlight security issues
- [ ] Security status in network list

**Priority:** Medium  
**Estimated Effort:** Small

---

## Story Prioritization Guide

### Priority Levels
- **High**: Core functionality that significantly improves user experience
- **Medium**: Important features that enhance usability
- **Low**: Nice-to-have features that can be deferred

### Effort Estimates
- **Small**: 1-3 days of work
- **Medium**: 1-2 weeks of work
- **Large**: 2+ weeks of work

## Implementation Notes

- Stories can be broken down into smaller tasks during sprint planning
- Some stories have dependencies (e.g., US-005 depends on US-004)
- Stories marked as "partially implemented" may need refinement
- Consider user feedback when prioritizing stories
- Technical debt stories should be created separately if needed

## Related Documentation

- [Technical Documentation](../agents.md) - Architecture and implementation details
- [README](../README.md) - Project overview and setup

