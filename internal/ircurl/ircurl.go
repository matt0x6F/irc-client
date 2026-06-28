// Package ircurl parses irc:// and ircs:// URLs into a normalized form so the
// app can act on deep links. It is intentionally dependency-free and pure.
package ircurl

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

var (
	ErrBadScheme = errors.New("ircurl: scheme must be irc or ircs")
	ErrNoHost    = errors.New("ircurl: missing host")
	ErrBadTarget = errors.New("ircurl: invalid target")
)

const maxTargetLen = 200

// Target is a single channel or nick referenced by an irc URL.
type Target struct {
	Name   string // channel (with prefix) or nick
	Key    string // channel key, if any
	IsNick bool   // true => open a query/PM rather than join
}

// Link is a parsed irc/ircs URL.
type Link struct {
	Scheme  string
	Host    string
	Port    int
	TLS     bool
	Targets []Target
}

// Parse parses an irc:// or ircs:// URL. Missing ports default to 6667 (irc) /
// 6697 (ircs). Channel targets without a prefix char get a leading '#', except
// nick targets (",isnick"/",isuser"), which are left as-is.
func Parse(raw string) (*Link, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("ircurl: parse: %w", err)
	}

	scheme := strings.ToLower(u.Scheme)
	tls := false
	defPort := 6667
	switch scheme {
	case "irc":
	case "ircs":
		tls, defPort = true, 6697
	default:
		return nil, ErrBadScheme
	}

	host := u.Hostname()
	if host == "" {
		return nil, ErrNoHost
	}
	port := defPort
	if p := u.Port(); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 || n > 65535 {
			return nil, fmt.Errorf("ircurl: bad port %q", p)
		}
		port = n
	}

	link := &Link{Scheme: scheme, Host: host, Port: port, TLS: tls}

	// Path holds the comma-separated target list; url.Parse already
	// percent-decoded it, so any encoded space/control char surfaces here.
	// Note: if the URL contains a fragment (e.g., /#cascade), url.Parse puts it
	// in the Fragment field instead of the Path field. We need to check both.
	pathTargets := strings.TrimPrefix(u.Path, "/")
	if pathTargets == "" && u.Fragment != "" {
		// The input likely had /#target or /#target?key=value
		// We need to extract the target from the fragment and parse any query within it
		fragment := u.Fragment
		if idx := strings.Index(fragment, "?"); idx >= 0 {
			pathTargets = fragment[:idx]
		} else {
			pathTargets = fragment
		}
	}
	if pathTargets == "" {
		return link, nil
	}
	queryKey := u.Query().Get("key")
	// If there was a fragment with ?key=value, extract it from there
	if queryKey == "" && u.Fragment != "" {
		if idx := strings.Index(u.Fragment, "?key="); idx >= 0 {
			queryKey = u.Fragment[idx+5:]
			if ampIdx := strings.Index(queryKey, "&"); ampIdx >= 0 {
				queryKey = queryKey[:ampIdx]
			}
		}
	}

	segs := strings.Split(pathTargets, ",")
	i := 0
	for i < len(segs) {
		name := segs[i]
		i++
		if name == "" {
			continue
		}
		isNick := false
		// Consume any following flag segments that modify this name.
		for i < len(segs) {
			switch strings.ToLower(segs[i]) {
			case "isnick", "isuser":
				isNick = true
				i++
				continue
			case "needkey", "needpass":
				i++
				continue
			}
			break
		}
		if strings.ContainsAny(name, " \t\r\n\x00") || len(name) > maxTargetLen {
			return nil, ErrBadTarget
		}
		t := Target{Name: name, IsNick: isNick}
		if !isNick && !strings.HasPrefix(name, "#") && !strings.HasPrefix(name, "&") {
			t.Name = "#" + name
		}
		if !isNick {
			t.Key = queryKey
		}
		link.Targets = append(link.Targets, t)
	}
	return link, nil
}
