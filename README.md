# IRC Client

A modern, multi-platform IRC client built with Wails, featuring plugin support, ShadCN UI, and efficient message storage.

## Features

- **Multi-platform**: Windows, macOS, and Linux support
- **Plugin System**: IPC-based plugins (kubectl-style) for extensibility
- **Modern UI**: ShadCN components with Tailwind CSS v4
- **Efficient Storage**: SQLite with optimized batch writes
- **Event-Driven**: Extensible event bus architecture

## Quick Start

### Prerequisites

- Go 1.21+
- Node.js 18+
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### Installation

1. Clone the repository
2. Run setup:
   ```bash
   task setup
   ```

### Development

Run the development server with hot reload:
```bash
task dev
```

Or use Wails directly:
```bash
wails dev
```

### Building

Build for your current platform:
```bash
task build
```

Build for all platforms:
```bash
task build-all
```

## Taskfile Commands

This project uses [Task](https://taskfile.dev) for workflow automation. Common commands:

### Development
- `task dev` - Run development server
- `task build` - Build application
- `task check` - Run all checks (fmt, lint, test, type-check)

### Go Tasks
- `task go-test` - Run Go tests
- `task go-fmt` - Format Go code
- `task go-lint` - Run Go linters
- `task go-mod` - Tidy and verify modules

### Frontend Tasks
- `task frontend-install` - Install dependencies
- `task frontend-build` - Build for production
- `task frontend-type-check` - Type check TypeScript

### Database Tasks
- `task db-reset` - Reset database (WARNING: deletes all data)
- `task db-backup` - Backup database

### Plugin Tasks
- `task plugin-list` - List discovered plugins
- `task plugin-dir` - Create plugin directory

### Setup & Maintenance
- `task setup` - Initial project setup
- `task setup-dev` - Setup development environment
- `task clean` - Clean build artifacts
- `task release-check` - Run all checks before release

See all available tasks:
```bash
task --list
```

## Project Structure

```
irc-client/
├── app.go                 # Wails app entry with bindings
├── main.go                # Application entry point
├── internal/
│   ├── irc/              # IRC core implementation
│   ├── events/            # Event bus
│   ├── plugin/            # Plugin system
│   └── storage/           # Database layer
├── frontend/
│   └── src/
│       ├── components/    # React components
│       └── types/         # TypeScript definitions
└── plugins/               # Example plugins
```

## Documentation

- [Technical Documentation](agents.md) - Architecture, patterns, and workflows
- [Wails Documentation](https://wails.io/docs)
- [irc-go Library](https://github.com/ergochat/irc-go)

## License

BSD 3-Clause License
