package ircevent

import (
	"bytes"
	"errors"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircutils"
)

type saslResult struct {
	Failed bool
	Err    error
}

func sliceContains(str string, list []string) bool {
	for _, x := range list {
		if x == str {
			return true
		}
	}
	return false
}

func (irc *Connection) submitSASLResult(r saslResult) {
	select {
	case irc.saslChan <- r:
	default:
	}
}

func (irc *Connection) composeSaslPlainResponse() []byte {
	var buf bytes.Buffer
	buf.WriteString(irc.SASLLogin) // optional authzid, included for compatibility
	buf.WriteByte('\x00')
	buf.WriteString(irc.SASLLogin) // authcid
	buf.WriteByte('\x00')
	buf.WriteString(irc.SASLPassword) // passwd
	return buf.Bytes()
}

// isRegistered reports whether registration has completed (RPL_ENDOFMOTD /
// ERR_NOMOTD seen). The SASL-abort numerics below are only meaningful during the
// pre-registration SASL exchange; after registration the same numerics (notably
// RPL_LOGGEDOUT) are benign account-state changes and must not tear down a healthy
// connection.
func (irc *Connection) isRegistered() bool {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.registered
}

func (irc *Connection) setupSASLCallbacks() {
	irc.AddCallback("AUTHENTICATE", func(e ircmsg.Message) {
		if irc.SASLMechanism != nil {
			if irc.saslBuffer == nil {
				irc.saslBuffer = ircutils.NewSASLBuffer(0)
			}
			chunk := "+"
			if len(e.Params) > 0 {
				chunk = e.Params[0]
			}
			done, challenge, err := irc.saslBuffer.Add(chunk)
			if err != nil {
				irc.Send("AUTHENTICATE", "*")
				irc.submitSASLResult(saslResult{true, err})
				return
			}
			if !done {
				return // more challenge chunks to come
			}
			resp, err := irc.SASLMechanism.Respond(challenge)
			if err != nil {
				irc.Send("AUTHENTICATE", "*")
				irc.submitSASLResult(saslResult{true, err})
				return
			}
			for _, out := range ircutils.EncodeSASLResponse(resp) {
				irc.Send("AUTHENTICATE", out)
			}
			return
		}
		// built-in PLAIN/EXTERNAL (unchanged)
		switch irc.SASLMech {
		case "PLAIN":
			for _, resp := range ircutils.EncodeSASLResponse(irc.composeSaslPlainResponse()) {
				irc.Send("AUTHENTICATE", resp)
			}
		case "EXTERNAL":
			irc.Send("AUTHENTICATE", "+")
		default:
			// impossible, nothing to do
		}
	})

	irc.AddCallback(RPL_LOGGEDOUT, func(e ircmsg.Message) {
		// Post-registration, RPL_LOGGEDOUT is a normal event (e.g. a user-issued
		// NickServ REGAIN/LOGOUT, or a services-side account re-bind), not a SASL
		// failure — ignore it rather than quitting the connection.
		if irc.isRegistered() {
			return
		}
		irc.SendRaw("CAP END")
		irc.SendRaw("QUIT")
		irc.submitSASLResult(saslResult{true, errors.New(e.Params[1])})
	})

	irc.AddCallback(ERR_NICKLOCKED, func(e ircmsg.Message) {
		if irc.isRegistered() {
			return // only a SASL failure during the pre-registration exchange
		}
		irc.SendRaw("CAP END")
		irc.SendRaw("QUIT")
		irc.submitSASLResult(saslResult{true, errors.New(e.Params[1])})
	})

	irc.AddCallback(RPL_SASLSUCCESS, func(e ircmsg.Message) {
		irc.submitSASLResult(saslResult{false, nil})
	})

	irc.AddCallback(ERR_SASLFAIL, func(e ircmsg.Message) {
		if irc.isRegistered() {
			return // only a SASL failure during the pre-registration exchange
		}
		irc.SendRaw("CAP END")
		irc.SendRaw("QUIT")
		irc.submitSASLResult(saslResult{true, errors.New(e.Params[1])})
	})

	// this could potentially happen with auto-login via certfp?
	irc.AddCallback(ERR_SASLALREADY, func(e ircmsg.Message) {
		irc.submitSASLResult(saslResult{false, nil})
	})
}
