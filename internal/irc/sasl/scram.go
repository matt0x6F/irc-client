package sasl

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"errors"
	"fmt"
	"hash"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// Scram implements SCRAM-SHA-256 and SCRAM-SHA-512 (RFC 5802) over IRC SASL.
type Scram struct {
	mech     string
	username string
	password string
	nonce    string           // client nonce
	h        func() hash.Hash // hash constructor for the chosen mechanism

	step        int
	serverKey   []byte
	authMessage string
}

func NewScram(mech, username, password string) (Mechanism, error) {
	var h func() hash.Hash
	switch mech {
	case "SCRAM-SHA-256":
		h = sha256.New
	case "SCRAM-SHA-512":
		h = sha512.New
	default:
		return nil, fmt.Errorf("unsupported SCRAM mechanism %q", mech)
	}
	nonce, err := randomNonce()
	if err != nil {
		return nil, err
	}
	return &Scram{mech: mech, username: username, password: password, nonce: nonce, h: h}, nil
}

func (s *Scram) Name() string { return s.mech }

func (s *Scram) Respond(challenge []byte) ([]byte, error) {
	switch s.step {
	case 0: // server sent "+" (empty)
		s.step = 1
		clientFirstBare := "n=" + s.username + ",r=" + s.nonce
		s.authMessage = clientFirstBare
		return []byte("n,," + clientFirstBare), nil

	case 1: // server-first: r=,s=,i=
		s.step = 2
		p := parseParams(string(challenge))
		rnonce := p["r"]
		if !strings.HasPrefix(rnonce, s.nonce) {
			return nil, errors.New("scram: server nonce does not extend client nonce")
		}
		salt, err := base64.StdEncoding.DecodeString(p["s"])
		if err != nil {
			return nil, fmt.Errorf("scram: bad salt: %w", err)
		}
		iters, err := strconv.Atoi(p["i"])
		if err != nil {
			return nil, fmt.Errorf("scram: bad iteration count: %w", err)
		}
		salted := pbkdf2.Key([]byte(s.password), salt, iters, s.h().Size(), s.h)
		clientKey := hmacSum(salted, "Client Key", s.h)
		storedKey := hashSum(clientKey, s.h)
		s.serverKey = hmacSum(salted, "Server Key", s.h)

		gs2 := base64.StdEncoding.EncodeToString([]byte("n,,")) // "biws"
		clientFinalNoProof := "c=" + gs2 + ",r=" + rnonce
		s.authMessage = s.authMessage + "," + string(challenge) + "," + clientFinalNoProof
		clientSig := hmacSum(storedKey, s.authMessage, s.h)
		proof := xorBytes(clientKey, clientSig)
		return []byte(clientFinalNoProof + ",p=" + base64.StdEncoding.EncodeToString(proof)), nil

	case 2: // server-final: v=
		s.step = 3
		p := parseParams(string(challenge))
		got, err := base64.StdEncoding.DecodeString(p["v"])
		if err != nil {
			return nil, errors.New("scram: bad server signature encoding")
		}
		want := hmacSum(s.serverKey, s.authMessage, s.h)
		if !hmac.Equal(got, want) {
			return nil, errors.New("scram: server signature mismatch")
		}
		return []byte{}, nil // acknowledge; server replies 903

	default:
		return nil, errors.New("scram: unexpected extra challenge")
	}
}

func randomNonce() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("scram: nonce: %w", err)
	}
	// base64 without '=' padding or ',' — both are SCRAM delimiters
	return strings.NewReplacer("=", "", ",", "").Replace(base64.StdEncoding.EncodeToString(b)), nil
}

func parseParams(msg string) map[string]string {
	out := make(map[string]string)
	for _, part := range strings.Split(msg, ",") {
		if len(part) >= 2 && part[1] == '=' {
			out[part[0:1]] = part[2:]
		}
	}
	return out
}

func hmacSum(key []byte, data string, h func() hash.Hash) []byte {
	m := hmac.New(h, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func hashSum(data []byte, h func() hash.Hash) []byte {
	x := h()
	x.Write(data)
	return x.Sum(nil)
}

func xorBytes(a, b []byte) []byte {
	out := make([]byte, len(a))
	for i := range a {
		out[i] = a[i] ^ b[i]
	}
	return out
}
