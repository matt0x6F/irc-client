package irc

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"strconv"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
	"golang.org/x/crypto/pbkdf2"
)

// SCRAMState tracks the state of SCRAM authentication
type SCRAMState struct {
	mechanism    string
	username     string
	password     string
	clientNonce  string
	serverNonce  string
	salt         string
	iterations   int
	saltedPassword []byte
	clientKey    []byte
	storedKey    []byte
	serverKey    []byte
	clientProof  string
	serverSignature string
}

// handleSCRAMAuth handles SCRAM-SHA-256 and SCRAM-SHA-512 authentication
func (c *IRCClient) handleSCRAMAuth(response string) {
	// Initialize or get existing SCRAM state
	var state *SCRAMState
	if c.scramState == nil {
		state = &SCRAMState{
			mechanism:   c.saslMechanism,
			username:    c.saslUsername,
			password:    c.saslPassword,
			clientNonce: generateClientNonce(),
		}
		c.scramState = state
	} else {
		state = c.scramState
	}

	if response == "+" {
		// First message: send client-first-message
		clientFirstMessageBare := fmt.Sprintf("n=%s,r=%s", state.username, state.clientNonce)
		gs2Header := "n,," // No channel binding, no authorization identity
		clientFirstMessage := gs2Header + clientFirstMessageBare
		
		encoded := base64.StdEncoding.EncodeToString([]byte(clientFirstMessage))
		c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", encoded))
	} else if response == "*" {
		// Abort
		c.mu.Lock()
		c.saslInProgress = false
		c.scramState = nil
		c.mu.Unlock()
		c.conn.SendRaw("CAP END")
	} else {
		// Server response: decode and process
		decoded, err := base64.StdEncoding.DecodeString(response)
		if err != nil {
			c.abortSASL("Failed to decode server response")
			return
		}

		serverMessage := string(decoded)
		
		// Parse server-first-message: r=...,s=...,i=...
		params := parseSCRAMParams(serverMessage)
		
		serverNonce, ok := params["r"]
		if !ok || !strings.HasPrefix(serverNonce, state.clientNonce) {
			c.abortSASL("Invalid server nonce")
			return
		}
		state.serverNonce = serverNonce

		salt, ok := params["s"]
		if !ok {
			c.abortSASL("Missing salt")
			return
		}
		state.salt = salt

		iterationsStr, ok := params["i"]
		if !ok {
			c.abortSASL("Missing iterations")
			return
		}
		iterations, err := strconv.Atoi(iterationsStr)
		if err != nil {
			c.abortSASL("Invalid iterations")
			return
		}
		state.iterations = iterations

		// Compute salted password
		saltBytes, err := base64.StdEncoding.DecodeString(state.salt)
		if err != nil {
			c.abortSASL("Invalid salt encoding")
			return
		}

		var h func() hash.Hash
		if state.mechanism == "SCRAM-SHA-256" {
			h = sha256.New
		} else if state.mechanism == "SCRAM-SHA-512" {
			h = sha512.New
		} else {
			c.abortSASL("Unsupported SCRAM mechanism")
			return
		}

		state.saltedPassword = pbkdf2.Key([]byte(state.password), saltBytes, state.iterations, h().Size(), h)

		// Compute client key
		state.clientKey = computeHMAC(state.saltedPassword, "Client Key", h)
		
		// Compute stored key
		state.storedKey = computeHash(state.clientKey, h)

		// Compute server key
		state.serverKey = computeHMAC(state.saltedPassword, "Server Key", h)

		// Build client-final-message
		clientFirstMessageBare := fmt.Sprintf("n=%s,r=%s", state.username, state.clientNonce)
		serverFirstMessage := serverMessage
		clientFinalMessageWithoutProof := fmt.Sprintf("c=%s,r=%s", base64.StdEncoding.EncodeToString([]byte("n,,")), state.serverNonce)
		
		authMessage := clientFirstMessageBare + "," + serverFirstMessage + "," + clientFinalMessageWithoutProof
		
		// Compute client signature
		clientSignature := computeHMAC(state.storedKey, authMessage, h)
		
		// Compute client proof
		clientProof := xorBytes(state.clientKey, clientSignature)
		state.clientProof = base64.StdEncoding.EncodeToString(clientProof)

		// Send client-final-message
		clientFinalMessage := clientFinalMessageWithoutProof + ",p=" + state.clientProof
		encoded := base64.StdEncoding.EncodeToString([]byte(clientFinalMessage))
		c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", encoded))
	}
}

// verifySCRAMServerSignature verifies the server's signature in the final message
func (c *IRCClient) verifySCRAMServerSignature(response string) bool {
	if c.scramState == nil {
		return false
	}

	decoded, err := base64.StdEncoding.DecodeString(response)
	if err != nil {
		return false
	}

	serverMessage := string(decoded)
	params := parseSCRAMParams(serverMessage)

	serverSignature, ok := params["v"]
	if !ok {
		return false
	}

	// Compute expected server signature
	clientFirstMessageBare := fmt.Sprintf("n=%s,r=%s", c.scramState.username, c.scramState.clientNonce)
	serverFirstMessage := fmt.Sprintf("r=%s,s=%s,i=%s", c.scramState.serverNonce, c.scramState.salt, strconv.Itoa(c.scramState.iterations))
	clientFinalMessageWithoutProof := fmt.Sprintf("c=%s,r=%s", base64.StdEncoding.EncodeToString([]byte("n,,")), c.scramState.serverNonce)
	authMessage := clientFirstMessageBare + "," + serverFirstMessage + "," + clientFinalMessageWithoutProof

	var h func() hash.Hash
	if c.scramState.mechanism == "SCRAM-SHA-256" {
		h = sha256.New
	} else {
		h = sha512.New
	}

	expectedSignature := base64.StdEncoding.EncodeToString(computeHMAC(c.scramState.serverKey, authMessage, h))
	
	return serverSignature == expectedSignature
}

// Helper functions

func generateClientNonce() string {
	// Simple nonce generation - in production, use crypto/rand
	return fmt.Sprintf("%x", time.Now().UnixNano())
}

func parseSCRAMParams(message string) map[string]string {
	params := make(map[string]string)
	parts := strings.Split(message, ",")
	for _, part := range parts {
		if len(part) >= 3 && part[1] == '=' {
			key := part[0:1]
			value := part[2:]
			params[key] = value
		}
	}
	return params
}

func computeHMAC(key []byte, data string, h func() hash.Hash) []byte {
	mac := hmac.New(h, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func computeHash(data []byte, h func() hash.Hash) []byte {
	hasher := h()
	hasher.Write(data)
	return hasher.Sum(nil)
}

func xorBytes(a, b []byte) []byte {
	if len(a) != len(b) {
		return nil
	}
	result := make([]byte, len(a))
	for i := range a {
		result[i] = a[i] ^ b[i]
	}
	return result
}

func (c *IRCClient) abortSASL(reason string) {
	c.mu.Lock()
	c.saslInProgress = false
	c.scramState = nil
	c.mu.Unlock()
	
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("SASL authentication aborted: %s", reason),
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	c.storage.WriteMessage(statusMsg)
	
	c.conn.SendRaw("AUTHENTICATE *")
	c.conn.SendRaw("CAP END")
	
	c.eventBus.Emit(events.Event{
		Type:      EventSASLAborted,
		Data:      map[string]interface{}{"network": c.network.Address, "reason": reason},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

