package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// JSON-RPC structures
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
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type InitializeParams struct {
	Version      string                 `json:"version"`
	Capabilities []string               `json:"capabilities"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

type EventParams struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// Plugin state
type PluginState struct {
	config map[string]interface{}
}

var state = &PluginState{
	config: make(map[string]interface{}),
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			continue
		}

		var resp Response
		resp.JSONRPC = "2.0"
		resp.ID = req.ID

		switch req.Method {
		case "initialize":
			resp.Result = handleInitialize(req.Params)
		default:
			// Unknown method, ignore
			continue
		}

		// Send response
		if data, err := json.Marshal(resp); err == nil {
			writer.Write(data)
			writer.WriteString("\n")
			writer.Flush()
		}
	}

	if err := scanner.Err(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error reading stdin: %v\n", err)
	}
}

func handleInitialize(params interface{}) map[string]interface{} {
	// Parse params
	paramsBytes, _ := json.Marshal(params)
	var initParams InitializeParams
	json.Unmarshal(paramsBytes, &initParams)

	// Store config
	if initParams.Config != nil {
		state.config = initParams.Config
	}

	// Set defaults if not provided
	if state.config["separator"] == nil {
		state.config["separator"] = ":"
	}
	if state.config["trigger"] == nil {
		state.config["trigger"] = "@"
	}
	if state.config["tab_completion"] == nil {
		state.config["tab_completion"] = true
	}

	return map[string]interface{}{
		"name":        "nick-completer",
		"version":     "1.0.0",
		"description": "Provides nickname completion with configurable trigger and separator",
		"author":      "Cascade Chat",
		"events":      []string{"*"},
		"config_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"separator": map[string]interface{}{
					"type":        "string",
					"enum":        []string{":", "-", " "},
					"default":     ":",
					"description": "Separator to use after nickname",
				},
				"trigger": map[string]interface{}{
					"type":        "string",
					"enum":        []string{"@", "none"},
					"default":     "@",
					"description": "Trigger character before nickname (or none)",
				},
				"tab_completion": map[string]interface{}{
					"type":        "boolean",
					"default":     true,
					"description": "Enable Tab key for completion",
				},
			},
		},
	}
}
