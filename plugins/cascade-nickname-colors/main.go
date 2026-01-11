package main

import (
	"bufio"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
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
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

type InitializeParams struct {
	Version     string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type EventParams struct {
	Type string                 `json:"type"`
	Data map[string]interface{} `json:"data"`
}

// Color palette - nice IRC colors
var colors = []string{
	"#FF6B6B", // Red
	"#4ECDC4", // Teal
	"#45B7D1", // Blue
	"#FFA07A", // Light Salmon
	"#98D8C8", // Mint
	"#F7DC6F", // Yellow
	"#BB8FCE", // Purple
	"#85C1E2", // Sky Blue
	"#F8B739", // Orange
	"#52BE80", // Green
	"#EC7063", // Coral
	"#5DADE2", // Light Blue
	"#F1948A", // Pink
	"#73C6B6", // Turquoise
	"#F4D03F", // Gold
	"#AF7AC5", // Lavender
}

// getColorForNickname returns a consistent color for a nickname
func getColorForNickname(nickname string) string {
	// Create a hash of the nickname
	hash := md5.Sum([]byte(strings.ToLower(nickname)))
	// Use first byte to select color
	index := int(hash[0]) % len(colors)
	return colors[index]
}

// getMapKeys returns all keys from a map (for debugging)
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// sendNotification sends a JSON-RPC notification (no response expected)
func sendNotification(method string, params interface{}) error {
	req := Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	
	// Write directly to stdout with newline
	// Note: Sync() doesn't work on pipes, so we just write and let the OS buffer handle it
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

// sendResponse sends a JSON-RPC response
func sendResponse(id interface{}, result interface{}) error {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	
	data, err := json.Marshal(resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[nickname-colors] Error marshaling response: %v\n", err)
		return err
	}
	
	fmt.Fprintf(os.Stderr, "[nickname-colors] Sending response: %s\n", string(data))
	
	// Write directly to stdout with newline
	// Note: Sync() doesn't work on pipes, so we just write and let the OS buffer handle it
	_, err = os.Stdout.Write(append(data, '\n'))
	if err != nil {
		fmt.Fprintf(os.Stderr, "[nickname-colors] Error writing to stdout: %v\n", err)
		return err
	}
	
	fmt.Fprintf(os.Stderr, "[nickname-colors] Successfully wrote response to stdout\n")
	return nil
}

// sendError sends a JSON-RPC error response
func sendError(id interface{}, code int, message string) error {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	
	// Write directly to stdout with newline
	// Note: Sync() doesn't work on pipes, so we just write and let the OS buffer handle it
	_, err = os.Stdout.Write(append(data, '\n'))
	return err
}

// setNicknameColor sets the color for a nickname
func setNicknameColor(nickname string, networkID interface{}) {
	key := fmt.Sprintf("nickname:%s", nickname)
	color := getColorForNickname(nickname)
	
	params := map[string]interface{}{
		"type":  "nickname_color",
		"key":   key,
		"value": color,
	}
	
	// Add network_id if available
	if networkID != nil {
		var id int64
		switch v := networkID.(type) {
		case float64:
			id = int64(v)
		case int64:
			id = v
		case int:
			id = int64(v)
		default:
			fmt.Fprintf(os.Stderr, "[nickname-colors] Warning: unexpected networkID type: %T, value: %v\n", networkID, networkID)
		}
		if id != 0 {
			params["network_id"] = id
		}
	}
	
	fmt.Fprintf(os.Stderr, "[nickname-colors] Setting color for %s: %s (networkID: %v)\n", nickname, color, networkID)
	if err := sendNotification("ui_metadata.set", params); err != nil {
		fmt.Fprintf(os.Stderr, "[nickname-colors] Error sending color metadata: %v\n", err)
	} else {
		fmt.Fprintf(os.Stderr, "[nickname-colors] Successfully sent color for %s\n", nickname)
	}
}

// handleEvent processes IRC events
func handleEvent(params EventParams) {
	fmt.Fprintf(os.Stderr, "[nickname-colors] Received event: %s\n", params.Type)
	fmt.Fprintf(os.Stderr, "[nickname-colors] Event data keys: %v\n", getMapKeys(params.Data))
	fmt.Fprintf(os.Stderr, "[nickname-colors] Full event data: %+v\n", params.Data)
	
	// Extract networkID from event data (can be networkId or network_id)
	var networkID interface{}
	if id, ok := params.Data["networkId"]; ok {
		networkID = id
		fmt.Fprintf(os.Stderr, "[nickname-colors] Found networkId: %v (type: %T)\n", id, id)
	} else if id, ok := params.Data["network_id"]; ok {
		networkID = id
		fmt.Fprintf(os.Stderr, "[nickname-colors] Found network_id: %v (type: %T)\n", id, id)
	} else {
		fmt.Fprintf(os.Stderr, "[nickname-colors] WARNING: No networkId or network_id in event data!\n")
	}
	
	switch params.Type {
	case "message.received":
		// Extract nickname from message
		if user, ok := params.Data["user"].(string); ok && user != "*" {
			fmt.Fprintf(os.Stderr, "[nickname-colors] Processing message from user: %s\n", user)
			setNicknameColor(user, networkID)
		}
	case "user.joined":
		// Extract nickname from join event (can be "user" or "nickname" field)
		var nickname string
		if n, ok := params.Data["nickname"].(string); ok {
			nickname = n
		} else if u, ok := params.Data["user"].(string); ok {
			nickname = u
		}
		if nickname != "" {
			fmt.Fprintf(os.Stderr, "[nickname-colors] Processing user joined: %s\n", nickname)
			setNicknameColor(nickname, networkID)
		}
	case "channel.names.complete":
		// Process all users in the channel when NAMES list is complete
		fmt.Fprintf(os.Stderr, "[nickname-colors] Received channel.names.complete event\n")
		if usersRaw, ok := params.Data["users"]; ok {
			fmt.Fprintf(os.Stderr, "[nickname-colors] Found users field, type: %T, value: %v\n", usersRaw, usersRaw)
			// Users can be []interface{} when unmarshaled from JSON
			if usersArray, ok := usersRaw.([]interface{}); ok {
				fmt.Fprintf(os.Stderr, "[nickname-colors] Processing %d users from channel.names.complete\n", len(usersArray))
				for _, userRaw := range usersArray {
					if user, ok := userRaw.(string); ok && user != "" {
						fmt.Fprintf(os.Stderr, "[nickname-colors] Setting color for user from names list: %s\n", user)
						setNicknameColor(user, networkID)
					}
				}
			} else if usersArray, ok := usersRaw.([]string); ok {
				// Direct []string type (less common but possible)
				fmt.Fprintf(os.Stderr, "[nickname-colors] Processing %d users from channel.names.complete ([]string)\n", len(usersArray))
				for _, user := range usersArray {
					if user != "" {
						fmt.Fprintf(os.Stderr, "[nickname-colors] Setting color for user from names list: %s\n", user)
						setNicknameColor(user, networkID)
					}
				}
			} else {
				fmt.Fprintf(os.Stderr, "[nickname-colors] WARNING: users field is not []interface{} or []string, type: %T\n", usersRaw)
			}
		} else {
			fmt.Fprintf(os.Stderr, "[nickname-colors] WARNING: No users field in channel.names.complete event data\n")
		}
	case "user.nick":
		// Handle nickname changes
		if oldNick, ok := params.Data["old_nickname"].(string); ok {
			setNicknameColor(oldNick, networkID)
		}
		if newNick, ok := params.Data["nickname"].(string); ok {
			setNicknameColor(newNick, networkID)
		}
	}
}

func main() {
	// Write directly to stderr (unbuffered) to ensure it's sent immediately
	// This is critical when stderr is redirected to a pipe
	// Note: Don't call Sync() on pipes - it can block if the pipe buffer is full
	os.Stderr.Write([]byte("[nickname-colors] Plugin starting, waiting for input...\n"))
	
	// Use a buffered reader instead of Scanner to avoid blocking issues
	// Set a reasonable buffer size to avoid blocking
	reader := bufio.NewReaderSize(os.Stdin, 4096)
	
	// Set up a signal handler to exit gracefully
	// Note: We can't easily handle signals in this simple plugin, but we can
	// ensure we exit quickly when stdin closes
	
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
		if err == io.EOF {
			fmt.Fprintf(os.Stderr, "[nickname-colors] EOF reached, exiting\n")
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "[nickname-colors] Error reading from stdin: %v\n", err)
		os.Exit(1)
		}
		
		// Remove trailing newline
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			continue
		}
		
		// Write directly to stderr for immediate output
		// Don't call Sync() on pipes - it can block
		os.Stderr.Write([]byte(fmt.Sprintf("[nickname-colors] Received line from stdin: %s\n", line)))
		
		var req Request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			os.Stderr.Write([]byte(fmt.Sprintf("[nickname-colors] Error parsing request: %v\n", err)))
			continue
		}
		
		os.Stderr.Write([]byte(fmt.Sprintf("[nickname-colors] Parsed request: Method=%s, ID=%v\n", req.Method, req.ID)))
		
		// Handle initialize request
		if req.Method == "initialize" {
			os.Stderr.Write([]byte(fmt.Sprintf("[nickname-colors] Received initialize request, ID: %v\n", req.ID)))
			// Parse params
			paramsBytes, _ := json.Marshal(req.Params)
			var initParams InitializeParams
			json.Unmarshal(paramsBytes, &initParams)
			
			os.Stderr.Write([]byte("[nickname-colors] Sending initialize response\n"))
			// Send success response
			if err := sendResponse(req.ID, map[string]interface{}{
				"initialized": true,
			}); err != nil {
				os.Stderr.Write([]byte(fmt.Sprintf("[nickname-colors] Error sending initialize response: %v\n", err)))
			} else {
				os.Stderr.Write([]byte("[nickname-colors] Successfully sent initialize response\n"))
			}
			continue
		}
		
		// Handle event notifications
		if req.Method == "event" && req.ID == nil {
			// Parse event params
			paramsBytes, _ := json.Marshal(req.Params)
			var eventParams EventParams
			if err := json.Unmarshal(paramsBytes, &eventParams); err == nil {
				handleEvent(eventParams)
			} else {
				fmt.Fprintf(os.Stderr, "[nickname-colors] Error parsing event params: %v, raw: %s\n", err, string(paramsBytes))
			}
			continue
		}
		
		// Unknown method
		if req.ID != nil {
			sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
		}
	}
}
