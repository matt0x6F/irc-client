package irc

import (
	"slices"
	"strings"
)

// defaultChanModes is the RFC1459-ish fallback used when a server never advertises
// CHANMODES in its ISUPPORT (005) reply. Classes are comma-separated A,B,C,D.
const defaultChanModes = "b,k,l,imnstp"

// ModeKind classifies a channel mode letter according to the CHANMODES ISUPPORT token.
// The class determines whether a mode consumes a parameter when applied.
type ModeKind int

const (
	// ModeKindFlag (CHANMODES type D) never takes a parameter, e.g. imnpst.
	ModeKindFlag ModeKind = iota
	// ModeKindList (type A) is a list mode that takes a parameter on both + and -, e.g. b (ban).
	ModeKindList
	// ModeKindParam (type B) always takes a parameter on both + and -, e.g. k (key).
	ModeKindParam
	// ModeKindSetParam (type C) takes a parameter only when set (+), e.g. l (limit).
	ModeKindSetParam
	// ModeKindPrefix is a membership mode from PREFIX (o, v, h, ...) and takes a nick on + and -.
	ModeKindPrefix
)

// ModeChange is a single resolved mode operation produced by ParseModeChanges.
type ModeChange struct {
	Add   bool     // true for '+', false for '-'
	Mode  rune     // the mode letter (e.g. 'o', 'b', 'k', 'm')
	Param string   // associated parameter (nick, key, limit, mask); empty when none applies
	Kind  ModeKind // classification of the mode letter
}

// ModeClassification is an immutable snapshot of a server's channel-mode grammar,
// derived from the CHANMODES and PREFIX ISUPPORT tokens. It is passed by value to
// ParseModeChanges so the parser never touches the client mutex.
type ModeClassification struct {
	List     map[rune]bool // type A
	Param    map[rune]bool // type B
	SetParam map[rune]bool // type C
	Flag     map[rune]bool // type D
	Prefix   map[rune]bool // membership modes from PREFIX (o, v, h, ...)
}

// kindOf returns the ModeKind for a mode letter. Unknown letters default to
// ModeKindFlag (no parameter) — the safe choice that keeps the parameter cursor
// aligned for the remainder of the mode string.
func (c ModeClassification) kindOf(m rune) ModeKind {
	switch {
	case c.Prefix[m]:
		return ModeKindPrefix
	case c.List[m]:
		return ModeKindList
	case c.Param[m]:
		return ModeKindParam
	case c.SetParam[m]:
		return ModeKindSetParam
	default:
		return ModeKindFlag
	}
}

// consumesParam reports whether a mode of the given kind, applied with the given
// operator, consumes a parameter from the argument list.
func consumesParam(k ModeKind, add bool) bool {
	switch k {
	case ModeKindList, ModeKindParam, ModeKindPrefix:
		return true // A, B, prefix: parameter on both + and -
	case ModeKindSetParam:
		return add // C: parameter only when setting
	default:
		return false // D: never
	}
}

// ParseModeChanges walks an IRC mode string (e.g. "+o-v+k") together with its
// parameter list, resolving each letter into a ModeChange with the correct +/-
// operator and parameter. A parameter is consumed only when both the mode's class
// requires one AND a parameter is actually available — so a bare "+b" (a ban-list
// query) leaves Param empty instead of stealing an unrelated argument.
func ParseModeChanges(modeStr string, params []string, cls ModeClassification) []ModeChange {
	var changes []ModeChange
	add := true
	pi := 0
	for _, r := range modeStr {
		switch r {
		case '+':
			add = true
		case '-':
			add = false
		default:
			kind := cls.kindOf(r)
			param := ""
			if consumesParam(kind, add) && pi < len(params) {
				param = params[pi]
				pi++
			}
			changes = append(changes, ModeChange{Add: add, Mode: r, Param: param, Kind: kind})
		}
	}
	return changes
}

// classifyChanModes splits a CHANMODES ISUPPORT value ("A,B,C,D", e.g. "beI,k,l,imnpst")
// into its four comma-separated classes. Missing classes yield empty sets.
func classifyChanModes(chanModes string) (a, b, c, d map[rune]bool) {
	a, b, c, d = map[rune]bool{}, map[rune]bool{}, map[rune]bool{}, map[rune]bool{}
	dst := []map[rune]bool{a, b, c, d}
	for i, group := range strings.SplitN(chanModes, ",", 4) {
		if i >= len(dst) {
			break
		}
		for _, r := range group {
			dst[i][r] = true
		}
	}
	return
}

// parseExtban parses an EXTBAN ISUPPORT value of the form "<prefix>,<types>"
// (e.g. "$,ajrxc" or "~,qjncrRa") into the single prefix rune and the set of
// supported extban type letters. A value whose prefix field is empty (",a")
// yields prefix 0. ok is false when the value is empty or has no comma.
//
// The ratified account-extban feature corresponds to the 'a' type: with prefix
// '$' it matches masks like "$a" (any logged-in user) or "$a:account".
func parseExtban(value string) (prefix rune, types map[rune]bool, ok bool) {
	pfx, list, found := strings.Cut(value, ",")
	if !found {
		return 0, nil, false
	}
	if r := []rune(pfx); len(r) > 0 {
		prefix = r[0]
	}
	types = map[rune]bool{}
	for _, r := range list {
		types[r] = true
	}
	return prefix, types, true
}

// sortedRunes renders a set of mode letters as a deterministic, sorted string
// (e.g. for exposing a CHANMODES class to the frontend).
func sortedRunes(m map[rune]bool) string {
	rs := make([]rune, 0, len(m))
	for r := range m {
		rs = append(rs, r)
	}
	slices.Sort(rs)
	return string(rs)
}

// ChannelModeState is the parsed, canonical set of channel-level modes: boolean
// flags (type D) plus parameterized modes (types B and C with their values). It
// deliberately excludes membership (prefix) modes and list (type A) modes, which
// are tracked per-user and on demand respectively.
type ChannelModeState struct {
	flags  map[rune]bool   // type D letters that are set
	params map[rune]string // type B/C letters mapped to their parameter
}

// parseChannelModeState parses a canonical mode string ("+knt secret" or "+nt")
// back into a ChannelModeState using the server's classification.
func parseChannelModeState(s string, cls ModeClassification) ChannelModeState {
	st := ChannelModeState{flags: map[rune]bool{}, params: map[rune]string{}}
	s = strings.TrimSpace(s)
	if s == "" {
		return st
	}
	fields := strings.Fields(s)
	modeStr := fields[0]
	if !strings.HasPrefix(modeStr, "+") && !strings.HasPrefix(modeStr, "-") {
		modeStr = "+" + modeStr
	}
	for _, ch := range ParseModeChanges(modeStr, fields[1:], cls) {
		switch ch.Kind {
		case ModeKindFlag:
			st.flags[ch.Mode] = true
		case ModeKindParam, ModeKindSetParam:
			st.params[ch.Mode] = ch.Param
		}
	}
	return st
}

// String serializes the state back into a canonical mode string mirroring the format
// of RPL_CHANNELMODEIS (324): "+<letters> <params...>" with letters sorted and each
// parameter appended in the same order. Returns "" when no modes are set.
func (st ChannelModeState) String() string {
	letters := make([]rune, 0, len(st.flags)+len(st.params))
	for m := range st.flags {
		letters = append(letters, m)
	}
	for m := range st.params {
		letters = append(letters, m)
	}
	if len(letters) == 0 {
		return ""
	}
	slices.Sort(letters)

	var b strings.Builder
	b.WriteByte('+')
	for _, m := range letters {
		b.WriteRune(m)
	}
	for _, m := range letters {
		if v, ok := st.params[m]; ok && v != "" {
			b.WriteByte(' ')
			b.WriteString(v)
		}
	}
	return b.String()
}

// applyChannelModes folds channel-level mode changes into a prior canonical mode
// string and returns the new canonical string. Membership (prefix) and list (type A)
// changes are ignored — they are handled separately by the caller.
func applyChannelModes(prior string, changes []ModeChange, cls ModeClassification) string {
	st := parseChannelModeState(prior, cls)
	for _, ch := range changes {
		switch ch.Kind {
		case ModeKindFlag:
			if ch.Add {
				st.flags[ch.Mode] = true
			} else {
				delete(st.flags, ch.Mode)
			}
		case ModeKindParam, ModeKindSetParam:
			if ch.Add {
				st.params[ch.Mode] = ch.Param
			} else {
				delete(st.params, ch.Mode)
			}
		}
	}
	return st.String()
}
