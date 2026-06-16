# Cascade Chat

A modern, multi-platform IRC client built with Wails, featuring plugin support, ShadCN UI, and efficient message storage.

## Features

- **Multi-platform**: Windows, macOS, and Linux support
- **Plugin System**: IPC-based plugins (kubectl-style) for extensibility
- **Modern UI**: ShadCN components with Tailwind CSS v4
- **Efficient Storage**: SQLite with optimized batch writes
- **Event-Driven**: Extensible event bus architecture

## Install (macOS)

Download the latest `Cascade-<version>-universal.dmg` from the
[Releases page](https://github.com/matt0x6F/irc-client/releases). The build is a
universal binary, running natively on both Apple Silicon and Intel Macs.

1. Open the DMG and drag **Cascade** to your Applications folder.
2. The build is **not code-signed**, so the first launch needs one extra step to
   get past Gatekeeper:
   - **Right-click** `Cascade.app` → **Open** → **Open** in the dialog, or
   - run `xattr -dr com.apple.quarantine /Applications/cascade.app` in Terminal.

You only need to do this once; afterwards it launches normally.

## Quick Start

### Prerequisites

- Go 1.25+ (required by Wails v3)
- Node.js 20 (see `.nvmrc`; with nvm run `nvm use` so `task` can find npm)
- Task: `go install github.com/go-task/task/v3/cmd/task@latest` (or `brew install go-task`)
- Wails v3 CLI: `go install github.com/wailsapp/wails/v3/cmd/wails3@latest`

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
wails3 dev
```

### Building

Build for your current platform:
```bash
task build
```

Package a distributable `.app` bundle / installer:
```bash
task package
```

Build a macOS `.dmg` (output in `bin/`; `brew install create-dmg` for a nicer
layout, otherwise `hdiutil` is used):
```bash
task dmg            # current architecture
task dmg-universal  # arm64 + amd64 universal
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
cascade/
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

## Testing with Local IRC Server

A Docker Compose setup is included for running a local Ergo IRC server for testing:

1. **Generate TLS certificates** (one-time setup):
   ```bash
   ./docker/ergo/generate-certs.sh
   ```

2. **Start the test server**:
   ```bash
   docker-compose up -d
   ```

3. **Connect in Cascade Chat**:
   - Address: `localhost`
   - Port: `6667` (plaintext) or `6697` (TLS)
   - Enable TLS if using port 6697

See [docker/ergo/README.md](docker/ergo/README.md) for more details.

## Documentation

- [Technical Documentation](agents.md) - Architecture, patterns, and workflows
- [Wails Documentation](https://wails.io/docs)
- [irc-go Library](https://github.com/ergochat/irc-go)

## License

BSD 3-Clause License
