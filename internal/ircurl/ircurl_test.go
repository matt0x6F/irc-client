package ircurl

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *Link
		wantErr bool
	}{
		{
			name: "channel join, default port",
			raw:  "irc://irc.libera.chat/cascade",
			want: &Link{Scheme: "irc", Host: "irc.libera.chat", Port: 6667, TLS: false,
				Targets: []Target{{Name: "#cascade"}}},
		},
		{
			name: "tls with explicit port and # channel",
			raw:  "ircs://irc.libera.chat:7000/#cascade",
			want: &Link{Scheme: "ircs", Host: "irc.libera.chat", Port: 7000, TLS: true,
				Targets: []Target{{Name: "#cascade"}}},
		},
		{
			name: "channel key via query",
			raw:  "irc://example.org/#secret?key=hunter2",
			want: &Link{Scheme: "irc", Host: "example.org", Port: 6667,
				Targets: []Target{{Name: "#secret", Key: "hunter2"}}},
		},
		{
			name: "nick target opens query, not normalized",
			raw:  "irc://example.org/alice,isnick",
			want: &Link{Scheme: "irc", Host: "example.org", Port: 6667,
				Targets: []Target{{Name: "alice", IsNick: true}}},
		},
		{
			name: "multiple channel targets",
			raw:  "irc://example.org/foo,bar",
			want: &Link{Scheme: "irc", Host: "example.org", Port: 6667,
				Targets: []Target{{Name: "#foo"}, {Name: "#bar"}}},
		},
		{
			name: "host only, no target",
			raw:  "ircs://example.org",
			want: &Link{Scheme: "ircs", Host: "example.org", Port: 6697, TLS: true, Targets: nil},
		},
		{name: "bad scheme", raw: "http://example.org/x", wantErr: true},
		{name: "no host", raw: "irc:///#chan", wantErr: true},
		{name: "target with space", raw: "irc://example.org/%23a%20b", wantErr: true},
		{
			name: "fragment with multi-param query, key not first",
			raw:  "irc://example.org/#chan?foo=bar&key=hunter2",
			want: &Link{Scheme: "irc", Host: "example.org", Port: 6667,
				Targets: []Target{{Name: "#chan", Key: "hunter2"}}},
		},
		{
			name: "path form with key",
			raw:  "irc://example.org/secret?key=pw",
			want: &Link{Scheme: "irc", Host: "example.org", Port: 6667,
				Targets: []Target{{Name: "#secret", Key: "pw"}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Scheme != tt.want.Scheme || got.Host != tt.want.Host ||
				got.Port != tt.want.Port || got.TLS != tt.want.TLS {
				t.Fatalf("header mismatch: got %+v want %+v", got, tt.want)
			}
			if len(got.Targets) != len(tt.want.Targets) {
				t.Fatalf("targets len: got %v want %v", got.Targets, tt.want.Targets)
			}
			for i := range got.Targets {
				if got.Targets[i] != tt.want.Targets[i] {
					t.Fatalf("target %d: got %+v want %+v", i, got.Targets[i], tt.want.Targets[i])
				}
			}
		})
	}
}
