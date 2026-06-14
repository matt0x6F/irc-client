package irc

import (
	"testing"
)

// testClassification mirrors a typical server: CHANMODES=beI,k,l,imnpst with PREFIX=(ov)@+.
func testClassification() ModeClassification {
	a, b, c, d := classifyChanModes("beI,k,l,imnpst")
	return ModeClassification{
		List:     a,
		Param:    b,
		SetParam: c,
		Flag:     d,
		Prefix:   map[rune]bool{'o': true, 'v': true},
	}
}

func TestParseModeChanges(t *testing.T) {
	cls := testClassification()

	tests := []struct {
		name    string
		modeStr string
		params  []string
		want    []ModeChange
	}{
		{
			name:    "prefix and param mix",
			modeStr: "+o-v+k",
			params:  []string{"nick1", "nick2", "secret"},
			want: []ModeChange{
				{Add: true, Mode: 'o', Param: "nick1", Kind: ModeKindPrefix},
				{Add: false, Mode: 'v', Param: "nick2", Kind: ModeKindPrefix},
				{Add: true, Mode: 'k', Param: "secret", Kind: ModeKindParam},
			},
		},
		{
			name:    "type C param only on set",
			modeStr: "+l-l",
			params:  []string{"50"},
			want: []ModeChange{
				{Add: true, Mode: 'l', Param: "50", Kind: ModeKindSetParam},
				{Add: false, Mode: 'l', Param: "", Kind: ModeKindSetParam},
			},
		},
		{
			name:    "flags never consume params",
			modeStr: "+mnt",
			params:  []string{"ignored"},
			want: []ModeChange{
				{Add: true, Mode: 'm', Param: "", Kind: ModeKindFlag},
				{Add: true, Mode: 'n', Param: "", Kind: ModeKindFlag},
				{Add: true, Mode: 't', Param: "", Kind: ModeKindFlag},
			},
		},
		{
			name:    "bare ban-list query consumes no param",
			modeStr: "+b",
			params:  nil,
			want: []ModeChange{
				{Add: true, Mode: 'b', Param: "", Kind: ModeKindList},
			},
		},
		{
			name:    "ban with mask on add and remove",
			modeStr: "+b-b",
			params:  []string{"a!*@*", "b!*@*"},
			want: []ModeChange{
				{Add: true, Mode: 'b', Param: "a!*@*", Kind: ModeKindList},
				{Add: false, Mode: 'b', Param: "b!*@*", Kind: ModeKindList},
			},
		},
		{
			name:    "unknown letter defaults to flag",
			modeStr: "+Z",
			params:  []string{"unused"},
			want: []ModeChange{
				{Add: true, Mode: 'Z', Param: "", Kind: ModeKindFlag},
			},
		},
		{
			name:    "missing params do not panic",
			modeStr: "+kl",
			params:  nil,
			want: []ModeChange{
				{Add: true, Mode: 'k', Param: "", Kind: ModeKindParam},
				{Add: true, Mode: 'l', Param: "", Kind: ModeKindSetParam},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseModeChanges(tt.modeStr, tt.params, cls)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d changes, want %d: %+v", len(got), len(tt.want), got)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("change %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestApplyChannelModesRoundTrip(t *testing.T) {
	cls := testClassification()

	tests := []struct {
		name    string
		prior   string
		modeStr string
		params  []string
		want    string
	}{
		{
			name:    "add flags to empty",
			prior:   "",
			modeStr: "+nt",
			want:    "+nt",
		},
		{
			name:    "add key keeps param in canonical string",
			prior:   "+nt",
			modeStr: "+k",
			params:  []string{"secret"},
			want:    "+knt secret",
		},
		{
			name:    "set limit then it serializes with value",
			prior:   "+knt secret",
			modeStr: "+l",
			params:  []string{"50"},
			want:    "+klnt secret 50",
		},
		{
			name:    "removing key drops its param",
			prior:   "+klnt secret 50",
			modeStr: "-k",
			want:    "+lnt 50",
		},
		{
			name:    "prefix and list changes do not affect channel string",
			prior:   "+nt",
			modeStr: "+o-b",
			params:  []string{"someone", "mask!*@*"},
			want:    "+nt",
		},
		{
			name:    "remove last flag yields empty",
			prior:   "+n",
			modeStr: "-n",
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changes := ParseModeChanges(tt.modeStr, tt.params, cls)
			got := applyChannelModes(tt.prior, changes, cls)
			if got != tt.want {
				t.Errorf("applyChannelModes(%q, %q) = %q, want %q", tt.prior, tt.modeStr, got, tt.want)
			}
		})
	}
}

func TestApplyUserPrefix(t *testing.T) {
	// PREFIX=(qaohv)~&@%+ : owner > admin > op > halfop > voice
	sc := &ServerCapabilities{
		PrefixString: "(qaohv)~&@%+",
		Prefix: map[rune]rune{
			'~': 'q', '&': 'a', '@': 'o', '%': 'h', '+': 'v',
		},
	}

	tests := []struct {
		name       string
		current    string
		modeLetter rune
		add        bool
		want       string
	}{
		{"op an empty user", "", 'o', true, "@"},
		{"voice keeps op first", "@", 'v', true, "@+"},
		{"add op to a voiced user sorts highest-first", "+", 'o', true, "@+"},
		{"deop leaves voice", "@+", 'o', false, "+"},
		{"removing absent mode is a no-op", "+", 'o', false, "+"},
		{"unknown mode letter is ignored", "@", 'Z', true, "@"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sc.applyUserPrefix(tt.current, tt.modeLetter, tt.add)
			if got != tt.want {
				t.Errorf("applyUserPrefix(%q, %q, %v) = %q, want %q", tt.current, tt.modeLetter, tt.add, got, tt.want)
			}
		})
	}
}

func TestChannelModeStateStringSorted(t *testing.T) {
	cls := testClassification()
	// Letters should come out sorted regardless of application order.
	got := applyChannelModes("", ParseModeChanges("+tnk", []string{"key"}, cls), cls)
	want := "+knt key"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
