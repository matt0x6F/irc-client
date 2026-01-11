package plugin

// JSON-RPC 2.0 protocol structures for plugin communication

// Request represents a JSON-RPC request
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Response represents a JSON-RPC response
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
}

// Error represents a JSON-RPC error
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// InitializeParams represents parameters for plugin initialization
type InitializeParams struct {
	Version     string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

// EventParams represents parameters for event notification
type EventParams struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// ActionParams represents parameters for plugin actions
type ActionParams struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// PluginInfo represents metadata about a plugin
type PluginInfo struct {
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description,omitempty"`
	Author      string   `json:"author,omitempty"`
	Events      []string `json:"events,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Path        string   `json:"path"`
	Enabled     bool     `json:"enabled"`
}

// MetadataSetParams represents parameters for ui_metadata.set notification
type MetadataSetParams struct {
	Type      string      `json:"type"`
	Key       string      `json:"key"`
	Value     interface{} `json:"value"`
	NetworkID *int64      `json:"network_id,omitempty"`
	Channel   *string     `json:"channel,omitempty"`
	Priority  int         `json:"priority,omitempty"`
}

