# Cascade Chat - Technical Documentation

## Project Overview

A modern, multi-platform IRC client built with Wails (Go + React), featuring:
- IRC protocol handling via `ergochat/irc-go`
- IPC-based plugin system (kubectl-style)
- ShadCN UI components with Tailwind CSS v4
- SQLite persistence with optimized write batching
- Event-driven architecture

## Architecture

### High-Level Structure

```
┌─────────────────────────────────────────┐
│         Frontend (React/TS)              │
│  - ShadCN Components                     │
│  - Tailwind CSS v4                       │
│  - Wails Bindings                        │
└─────────────────┬───────────────────────┘
                  │ Wails IPC
┌─────────────────▼───────────────────────┐
│         Backend (Go)                     │
│  ┌──────────┐  ┌──────────┐  ┌────────┐│
│  │ IRC Core │  │  Plugin   │  │Storage ││
│  │ (irc-go) │  │  Manager  │  │(SQLite)││
│  └────┬─────┘  └─────┬─────┘  └───┬────┘│
│       │              │             │     │
│       └──────┬───────┴──────┬──────┘     │
│              │              │            │
│       ┌──────▼──────┐  ┌────▼─────┐     │
│       │ Event Bus   │  │ Database │     │
│       └──────┬──────┘  └──────────┘     │
└──────────────┼──────────────────────────┘
               │ IPC (stdin/stdout)
┌──────────────▼──────────────────────────┐
│         Plugin Processes                │
│  (Any language, JSON-RPC protocol)       │
└──────────────────────────────────────────┘
```

### Core Components

#### 1. IRC Core (`internal/irc/`)
- **Purpose**: Manages IRC server connections and protocol handling
- **Key Files**:
  - `client.go`: Main IRC client implementation
  - `events.go`: Event type constants
- **Patterns**:
  - Uses `ircevent.Connection` from `ergochat/irc-go`
  - Emits events to EventBus for all IRC activities
  - Thread-safe with mutex protection

#### 2. Event Bus (`internal/events/`)
- **Purpose**: Central event routing system
- **Pattern**: Publisher-subscriber pattern
- **Features**:
  - Supports wildcard subscriptions (`*`)
  - Thread-safe with RWMutex
  - Async event delivery (goroutines)
  - Extensible for UI events (future)

#### 3. Storage Layer (`internal/storage/`)
- **Purpose**: Persistent data storage
- **Database**: SQLite with WAL mode
- **Code Generation**: SQLC for type-safe database queries
- **Key Features**:
  - Batch write buffering (configurable size and flush interval)
  - Efficient indexed queries
  - Migration system
  - Type-safe queries via SQLC
- **Patterns**:
  - Background flush goroutine
  - Channel-based write buffer
  - SQLC-generated queries for type safety
  - Batch inserts use sqlx for performance

#### 4. Plugin System (`internal/plugin/`)
- **Purpose**: Extensible plugin architecture
- **Discovery**: PATH-based + dedicated directory
- **IPC Protocol**: JSON-RPC 2.0 over stdin/stdout
- **Key Files**:
  - `manager.go`: Plugin lifecycle management
  - `discovery.go`: Plugin discovery
  - `ipc.go`: Inter-process communication
  - `protocol.go`: JSON-RPC structures
- **Patterns**:
  - Plugin processes run as separate executables
  - Bidirectional communication (future: UI events)
  - Event subscription model

#### 5. Frontend (`frontend/src/`)
- **Framework**: React 19 + TypeScript
- **UI Library**: ShadCN components
- **Styling**: Tailwind CSS v4
- **Structure**:
  ```
  src/
  ├── components/     # React components
  ├── types/          # TypeScript definitions
  └── App.tsx         # Main application
  ```

## Standards & Conventions

### Go Code Standards

1. **Package Organization**:
   - `internal/` for private packages
   - One package per directory
   - Clear separation of concerns

2. **Naming Conventions**:
   - Exported types: PascalCase
   - Unexported: camelCase
   - Interfaces: Descriptive names ending in `-er` when appropriate

3. **Error Handling**:
   - Always return errors, never panic (except in `main`)
   - Use `fmt.Errorf` with `%w` for error wrapping
   - Log errors at appropriate levels

4. **Concurrency**:
   - Use mutexes for shared state protection
   - Prefer channels for communication
   - Document goroutine lifetimes

5. **Testing**:
   - Unit tests in `*_test.go` files
   - Table-driven tests preferred
   - Mock external dependencies

### TypeScript/React Standards

1. **Component Structure**:
   - Functional components with hooks
   - Props interfaces defined in component file
   - Type safety with TypeScript

2. **State Management**:
   - Local state with `useState`
   - Effects with `useEffect` and proper cleanup
   - Wails bindings for backend communication

3. **Styling**:
   - Tailwind utility classes
   - ShadCN component variants
   - CSS variables for theming

4. **File Organization**:
   - One component per file
   - Types in separate `types/` directory
   - Shared utilities in `lib/`

## Patterns

### Event-Driven Architecture

All IRC events flow through the EventBus:
```
IRC Event → EventBus → [Subscribers]
                      ├─ Plugin Manager → Plugins
                      ├─ Storage (future)
                      └─ Frontend (future)
```

### Plugin Communication

1. **Discovery**: Scan PATH and plugin directory
2. **Initialization**: JSON-RPC `initialize` request
3. **Subscription**: Plugin declares event interests
4. **Runtime**: Events sent as JSON-RPC notifications
5. **Actions**: Plugins send action requests via JSON-RPC

### Database Write Optimization

1. Messages buffered in channel (configurable size)
2. Background goroutine flushes periodically
3. Batch inserts using sqlx NamedExec (kept for performance)
4. Single inserts use SQLC-generated queries
5. WAL mode for concurrent reads during writes

### Frontend-Backend Communication

- Wails automatically generates TypeScript bindings
- Go methods exposed via `Bind` in `main.go`
- Type-safe communication through generated types

## Workflows

### Development Workflow

1. **Setup**:
   ```bash
   # Install dependencies
   go mod download
   cd frontend && npm install
   ```

2. **Development**:
   ```bash
   # Run in dev mode (hot reload)
   wails dev
   ```

3. **Building**:
   ```bash
   # Build for current platform
   wails build
   
   # Build for specific platform
   wails build -platform darwin/amd64
   ```

4. **Testing**:
   ```bash
   # Run Go tests
   go test ./...
   
   # Run frontend tests (if configured)
   cd frontend && npm test
   ```

5. **Database Code Generation**:
   ```bash
   # Generate SQLC code after changing queries or schema
   sqlc generate
   
   # Validate queries
   sqlc vet
   
   # Check for uncommitted generated code changes
   sqlc diff
   ```

### Plugin Development

1. **Create Plugin Executable**:
   - Name: `cascade-<plugin-name>`
   - Location: `~/.cascade-chat/plugins/` or `$PATH`
   - Optional: `plugin.json` metadata file

2. **Plugin Protocol**:
   - Read JSON-RPC from stdin
   - Write JSON-RPC to stdout
   - Implement `initialize` method
   - Handle `event` notifications

3. **Example Plugin Structure**:
   ```
   my-plugin/
   ├── plugin.json      # Metadata (optional)
   └── cascade-my-plugin  # Executable
   ```

### Database Migrations

1. **Add Migration**:
   - Add SQL to `migrations.go`
   - Update `Migrate()` function
   - Update `schema.sql` to match new schema
   - Test migration on fresh database

2. **Schema Changes**:
   - Update `schema.sql` to reflect new schema
   - Add migration SQL to `migrations.go`
   - Regenerate SQLC code: `sqlc generate`
   - Update queries in `queries/*.sql` if needed

### SQLC Code Generation

The project uses [SQLC](https://sqlc.dev) to generate type-safe Go code from SQL queries.

1. **Query Files**:
   - SQL queries are in `internal/storage/queries/*.sql`
   - Each query must have a `-- name: QueryName :one|:many|:exec` annotation
   - Use `:one` for single row results, `:many` for multiple rows, `:exec` for updates/deletes

2. **Schema File**:
   - Schema is defined in `internal/storage/schema.sql`
   - Keep this in sync with migrations in `migrations.go`
   - Schema is the source of truth for SQLC code generation

3. **Generating Code**:
   ```bash
   sqlc generate
   ```
   - Generated code is in `internal/storage/generated/`
   - Never edit generated code directly
   - Regenerate after changing queries or schema

4. **Writing Queries**:
   - Use `?` for parameters (SQLite style)
   - For ON CONFLICT clauses, use `excluded.column` syntax to avoid parameter duplication
   - Complex queries with JOINs and subqueries are supported
   - Case-insensitive comparisons use `LOWER()` - SQLC will generate parameter names like `LOWER`

5. **Type Conversions**:
   - SQLC generates types using `sql.NullString`, `sql.NullInt64`, etc. for nullable fields
   - Application types use pointers (`*string`, `*int64`) for nullable fields
   - Conversion helpers in `convert.go` handle type conversions
   - Port fields: SQLC uses `int64`, application uses `int` - conversions handle this

6. **Special Cases**:
   - **Batch Inserts**: Keep using sqlx for batch message inserts (performance)
   - **JSON Fields**: Configured in `sqlc.yaml` to use `json.RawMessage`
   - **Complex Queries**: GetLastOpenPane, GetPrivateMessages work with SQLC

### Frontend Development

1. **Add Component**:
   - Create in `frontend/src/components/`
   - Use ShadCN components when possible
   - Follow TypeScript types

2. **Add Wails Binding**:
   - Add method to `app.go`
   - Rebuild: `wails generate module`
   - Use generated TypeScript types

## Configuration

### Database
- **Location**: `~/.cascade-chat/cascade-chat.db`
- **Mode**: WAL (Write-Ahead Logging)
- **Buffer**: 100 messages, 5 second flush interval

### Plugins
- **Directory**: `~/.cascade-chat/plugins/`
- **PATH**: System PATH for `cascade-*` executables
- **Metadata**: Optional `plugin.json` in plugin directory

### Frontend
- **Build Tool**: Vite
- **Framework**: React 19
- **Styling**: Tailwind CSS v4
- **Components**: ShadCN UI

## Future Considerations

### Plugin UI Components
- Architecture supports prepared component rendering
- Event bus extensible for UI events
- Bidirectional IPC ready for UI interactions

### Performance Optimizations
- Message pagination for large channels
- Virtual scrolling for message lists
- Database query optimization

### Features
- IRCv3 capability negotiation
- SASL authentication
- DCC file transfer
- Encrypted message storage
- Full-text search

## Troubleshooting

### Common Issues

1. **Plugin Not Loading**:
   - Check executable permissions
   - Verify plugin is in PATH or plugin directory
   - Check plugin implements JSON-RPC protocol

2. **Database Locked**:
   - Ensure WAL mode is enabled
   - Check for multiple connections
   - Verify single connection pool setting

3. **Frontend Not Updating**:
   - Rebuild Wails bindings: `wails generate module`
   - Clear frontend cache
   - Check browser console for errors

4. **IRC Connection Issues**:
   - Verify server address and port
   - Check TLS settings
   - Review connection logs

## Contributing

### Code Review Checklist
- [ ] Follows Go/TypeScript standards
- [ ] Includes error handling
- [ ] Thread-safe (if applicable)
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] No breaking changes (or documented)

### Commit Messages
- Use conventional commits format
- Prefix with component: `irc:`, `plugin:`, `ui:`, etc.
- Describe what and why, not how

## Resources

- [Wails Documentation](https://wails.io/docs)
- [irc-go Library](https://github.com/ergochat/irc-go)
- [ShadCN UI](https://ui.shadcn.com)
- [Tailwind CSS v4](https://tailwindcss.com)
- [JSON-RPC 2.0 Spec](https://www.jsonrpc.org/specification)

