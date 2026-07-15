package dcc

import (
	"encoding/binary"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type Command string

const (
	CommandSend   Command = "SEND"
	CommandResume Command = "RESUME"
	CommandAccept Command = "ACCEPT"
	CommandChat   Command = "CHAT"
)

// Offer is one parsed CTCP DCC control message. Position is used by RESUME and
// ACCEPT; Token identifies passive/reverse negotiations.
type Offer struct {
	Command  Command
	Filename string
	Address  string
	Port     int
	Size     int64
	Position int64
	Token    string
	Passive  bool
}

func Parse(payload string) (Offer, error) {
	fields, err := splitFields(strings.TrimSpace(payload))
	if err != nil {
		return Offer{}, err
	}
	if len(fields) < 1 {
		return Offer{}, fmt.Errorf("empty DCC command")
	}

	cmd := Command(strings.ToUpper(fields[0]))
	switch cmd {
	case CommandSend:
		return parseSendFields(fields)
	case CommandResume, CommandAccept:
		if len(fields) < 4 || len(fields) > 5 {
			return Offer{}, fmt.Errorf("DCC %s expects filename, port, position, and optional token", cmd)
		}
		filename, err := safeFilename(fields[1])
		if err != nil {
			return Offer{}, err
		}
		port, err := parsePort(fields[2], true)
		if err != nil {
			return Offer{}, err
		}
		position, err := strconv.ParseInt(fields[3], 10, 64)
		if err != nil || position < 0 {
			return Offer{}, fmt.Errorf("invalid DCC resume position")
		}
		o := Offer{Command: cmd, Filename: filename, Port: port, Position: position, Passive: port == 0}
		if len(fields) == 5 {
			o.Token = fields[4]
		}
		return o, nil
	case CommandChat:
		return Offer{Command: cmd}, nil
	default:
		return Offer{}, fmt.Errorf("unsupported DCC command %q", fields[0])
	}
}

func parseSendFields(fields []string) (Offer, error) {
	if len(fields) < 5 {
		return Offer{}, fmt.Errorf("DCC SEND expects filename, address, port, size, and optional token")
	}
	// Prefer the protocol's quoted/single-field form. If it does not validate,
	// fall back to the historic unquoted-spaces form used by some older clients,
	// parsing its numeric tail from the right.
	if len(fields) <= 6 {
		token := ""
		if len(fields) == 6 {
			token = fields[5]
		}
		if offer, err := makeSendOffer(fields[1], fields[2], fields[3], fields[4], token); err == nil {
			return offer, nil
		}
	}
	if len(fields) >= 6 {
		// Passive/reverse form has a token after the size.
		start := len(fields) - 4
		if start > 1 {
			if offer, err := makeSendOffer(strings.Join(fields[1:start], " "), fields[start], fields[start+1], fields[start+2], fields[start+3]); err == nil {
				return offer, nil
			}
		}
		// Classic form ends at the size.
		start = len(fields) - 3
		if start > 1 {
			if offer, err := makeSendOffer(strings.Join(fields[1:start], " "), fields[start], fields[start+1], fields[start+2], ""); err == nil {
				return offer, nil
			}
		}
	}
	return Offer{}, fmt.Errorf("invalid DCC SEND offer")
}

func makeSendOffer(filenameRaw, address, portRaw, sizeRaw, token string) (Offer, error) {
	filename, err := safeFilename(filenameRaw)
	if err != nil {
		return Offer{}, err
	}
	port, err := parsePort(portRaw, true)
	if err != nil {
		return Offer{}, err
	}
	size, err := strconv.ParseInt(sizeRaw, 10, 64)
	if err != nil || size < 0 {
		return Offer{}, fmt.Errorf("invalid DCC file size")
	}
	offer := Offer{Command: CommandSend, Filename: filename, Address: address, Port: port, Size: size, Token: token, Passive: port == 0}
	if offer.Passive {
		if token == "" || address != "0" {
			return Offer{}, fmt.Errorf("invalid passive DCC SEND")
		}
	} else if _, err := ParseAddress(address); err != nil {
		return Offer{}, err
	}
	return offer, nil
}

func ParseAddress(raw string) (net.IP, error) {
	if n, err := strconv.ParseUint(raw, 10, 32); err == nil {
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(n))
		ip := net.IP(b)
		return validateRemoteIP(ip)
	}
	ip := net.ParseIP(strings.Trim(raw, "[]"))
	if ip == nil {
		return nil, fmt.Errorf("invalid DCC address")
	}
	return validateRemoteIP(ip)
}

func validateRemoteIP(ip net.IP) (net.IP, error) {
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return nil, fmt.Errorf("unsafe DCC address")
	}
	return ip, nil
}

func FormatAddress(raw string) (string, error) {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return "", fmt.Errorf("invalid advertised address")
	}
	if v4 := ip.To4(); v4 != nil {
		return strconv.FormatUint(uint64(binary.BigEndian.Uint32(v4)), 10), nil
	}
	return ip.String(), nil
}

func FormatSend(filename, address string, port int, size int64, token string) string {
	parts := []string{"SEND", quoteFilename(filename), address, strconv.Itoa(port), strconv.FormatInt(size, 10)}
	if token != "" {
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}

func FormatResume(command Command, filename string, port int, position int64, token string) string {
	parts := []string{string(command), quoteFilename(filename), strconv.Itoa(port), strconv.FormatInt(position, 10)}
	if token != "" {
		parts = append(parts, token)
	}
	return strings.Join(parts, " ")
}

func safeFilename(raw string) (string, error) {
	if raw == "" || strings.IndexByte(raw, 0) >= 0 || strings.IndexFunc(raw, unicode.IsControl) >= 0 || filepath.IsAbs(raw) || filepath.Base(raw) != raw || raw == "." || raw == ".." {
		return "", fmt.Errorf("unsafe DCC filename")
	}
	if strings.ContainsAny(raw, `/\\`) {
		return "", fmt.Errorf("unsafe DCC filename")
	}
	return raw, nil
}

func quoteFilename(name string) string {
	name = filepath.Base(name)
	if strings.IndexFunc(name, unicode.IsSpace) < 0 && !strings.Contains(name, `"`) {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `\"`) + `"`
}

func parsePort(raw string, allowZero bool) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 || n > 65535 || (!allowZero && n == 0) {
		return 0, fmt.Errorf("invalid DCC port")
	}
	return n, nil
}

func splitFields(s string) ([]string, error) {
	var fields []string
	var b strings.Builder
	inQuote, escaped, have := false, false, false
	flush := func() {
		if have {
			fields = append(fields, b.String())
			b.Reset()
			have = false
		}
	}
	for _, r := range s {
		if escaped {
			b.WriteRune(r)
			escaped, have = false, true
			continue
		}
		if inQuote && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote, have = !inQuote, true
			continue
		}
		if unicode.IsSpace(r) && !inQuote {
			flush()
			continue
		}
		b.WriteRune(r)
		have = true
	}
	if escaped || inQuote {
		return nil, fmt.Errorf("unterminated quoted DCC argument")
	}
	flush()
	return fields, nil
}
