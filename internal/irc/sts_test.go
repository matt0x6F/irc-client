package irc

import "testing"

func TestParseSTS(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		wantPort     int
		wantDuration int64
	}{
		{"duration and port", "duration=2592000,port=6697", 6697, 2592000},
		{"port before duration", "port=6697,duration=300", 6697, 300},
		{"port only (plaintext bootstrap)", "port=6697", 6697, 0},
		{"duration only", "duration=2592000", 0, 2592000},
		{"duration zero removes policy", "duration=0,port=6697", 6697, 0},
		{"preload flag ignored", "duration=2592000,port=6697,preload", 6697, 2592000},
		{"unknown token ignored", "foo=bar,port=6697", 6697, 0},
		{"empty value", "", 0, 0},
		{"garbage port", "port=notanumber,duration=300", 0, 300},
		{"garbage duration", "duration=abc,port=6697", 6697, 0},
		{"negative port ignored", "port=-1", 0, 0},
		{"negative duration ignored", "duration=-5", 0, 0},
		{"whitespace tolerated", " port = 6697 , duration = 300 ", 6697, 300},
		{"uppercase keys", "PORT=6697,DURATION=300", 6697, 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseSTS(tt.value)
			if !got.Present {
				t.Errorf("parseSTS(%q).Present = false, want true", tt.value)
			}
			if got.Port != tt.wantPort {
				t.Errorf("parseSTS(%q).Port = %d, want %d", tt.value, got.Port, tt.wantPort)
			}
			if got.Duration != tt.wantDuration {
				t.Errorf("parseSTS(%q).Duration = %d, want %d", tt.value, got.Duration, tt.wantDuration)
			}
		})
	}
}

func TestIsIPLiteral(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"irc.libera.chat", false},
		{"localhost", false},
		{"example.com", false},
		{"127.0.0.1", true},
		{"192.168.1.10", true},
		{"::1", true},
		{"2001:db8::1", true},
		{" 10.0.0.1 ", true},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsIPLiteral(tt.host); got != tt.want {
			t.Errorf("IsIPLiteral(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}
