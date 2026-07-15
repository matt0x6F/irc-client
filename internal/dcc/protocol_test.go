package dcc

import (
	"net"
	"testing"
)

func TestParseSend(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    Offer
		wantErr bool
	}{
		{"classic", `SEND file.zip 3405803781 5000 42`, Offer{Command: CommandSend, Filename: "file.zip", Address: "3405803781", Port: 5000, Size: 42}, false},
		{"quoted", `SEND "project assets.zip" 3405803781 5001 4294967297`, Offer{Command: CommandSend, Filename: "project assets.zip", Address: "3405803781", Port: 5001, Size: 4294967297}, false},
		{"unquoted spaces", `SEND project assets.zip 3405803781 5001 4294967297`, Offer{Command: CommandSend, Filename: "project assets.zip", Address: "3405803781", Port: 5001, Size: 4294967297}, false},
		{"passive", `SEND file.zip 0 0 42 token-1`, Offer{Command: CommandSend, Filename: "file.zip", Address: "0", Port: 0, Size: 42, Token: "token-1", Passive: true}, false},
		{"zero byte", `SEND empty.txt 3405803781 5000 0`, Offer{Command: CommandSend, Filename: "empty.txt", Address: "3405803781", Port: 5000, Size: 0}, false},
		{"ipv6", `SEND archive.tar 2001:db8::1 5000 99`, Offer{Command: CommandSend, Filename: "archive.tar", Address: "2001:db8::1", Port: 5000, Size: 99}, false},
		{"traversal", `SEND ../secret 3405803781 5000 42`, Offer{}, true},
		{"absolute path", `SEND /tmp/secret 3405803781 5000 42`, Offer{}, true},
		{"loopback", `SEND file.zip 2130706433 5000 42`, Offer{}, true},
		{"negative size", `SEND file.zip 3405803781 5000 -1`, Offer{}, true},
		{"missing fields", `SEND file.zip 3405803781`, Offer{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Parse(tc.payload)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Parse() error = %v", err)
			}
			if !tc.wantErr && got != tc.want {
				t.Fatalf("Parse() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAddressFormatting(t *testing.T) {
	got, err := FormatAddress("203.0.113.5")
	if err != nil || got != "3405803781" {
		t.Fatalf("FormatAddress = %q, %v", got, err)
	}
	ip, err := ParseAddress(got)
	if err != nil || !ip.Equal(net.ParseIP("203.0.113.5")) {
		t.Fatalf("ParseAddress = %v, %v", ip, err)
	}
}

func TestResumeRoundTrip(t *testing.T) {
	payload := FormatResume(CommandResume, "project assets.zip", 5000, 4294967296, "7")
	got, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != CommandResume || got.Filename != "project assets.zip" || got.Position != 4294967296 || got.Token != "7" {
		t.Fatalf("unexpected offer: %+v", got)
	}
}

func TestAcceptRoundTrip(t *testing.T) {
	payload := FormatResume(CommandAccept, "movie.mkv", 0, 5<<30, "99")
	got, err := Parse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != CommandAccept || got.Position != 5<<30 || got.Token != "99" {
		t.Fatalf("unexpected ACCEPT: %+v", got)
	}
}

func TestLegacyAcknowledgementRollover(t *testing.T) {
	if got := legacyAcknowledgement((int64(1) << 32) + 123); got != 123 {
		t.Fatalf("acknowledgement after 4GiB = %d, want 123", got)
	}
}
