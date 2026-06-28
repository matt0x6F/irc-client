module github.com/matt0x6f/irc-client

go 1.25.0

require github.com/matt0x6f/irc-client/cascade v1.1.0

require (
	github.com/ergochat/irc-go v0.4.0
	github.com/fsnotify/fsnotify v1.10.1
	github.com/jmoiron/sqlx v1.4.0
	github.com/mattn/go-sqlite3 v1.14.33
	github.com/rs/zerolog v1.34.0
	github.com/traefik/yaegi v0.16.1
	github.com/wailsapp/wails/v3 v3.0.0-alpha2.103
	github.com/zalando/go-keyring v0.2.6
	golang.org/x/crypto v0.53.0
	golang.org/x/mod v0.35.0
	golang.org/x/net v0.56.0
	gopkg.in/natefinch/lumberjack.v2 v2.2.1
)

require (
	al.essio.dev/pkg/shellescape v1.6.0 // indirect
	git.sr.ht/~jackmordaunt/go-toast/v2 v2.0.3 // indirect
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/danieljoos/wincred v1.2.3 // indirect
	github.com/ebitengine/purego v0.9.1 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/wailsapp/wails/webview2 v1.0.24 // indirect
	golang.org/x/sys v0.46.0 // indirect
)

replace github.com/matt0x6f/irc-client/cascade => ./cascade

// replace github.com/wailsapp/wails/v2 v2.10.2 => /Users/matt/go/pkg/mod
