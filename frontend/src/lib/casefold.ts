// CASEMAPPING-aware case folding, mirroring the backend's byte-oriented logic
// (internal/irc/spec.go: casefold). IRC predates Unicode, so nick/channel identity
// folds by ASCII rules plus — on rfc1459 networks — the quirk that []\~ are the
// lowercase of {}|^ (they were "the same letter" on the terminals RFC 1459
// targeted). Use this for every in-memory map keyed by a nick or channel, so the
// frontend keys entries the SAME way the backend does; plain toLowerCase() would
// split Nick[a] and nick{a} into different buckets on an rfc1459 server.
//
// The server's CASEMAPPING token is exposed via ServerCapabilitiesInfo and cached
// per network in the network store; callers pass the network's value. An
// absent/empty mapping is treated as rfc1459, the protocol default.

/** Folds `s` into its canonical identity key under the server's `mapping`. */
export function casefold(mapping: string, s: string): string {
  let foldBrackets = false;
  let foldTilde = false;
  switch ((mapping || '').toLowerCase()) {
    case 'ascii':
      // only A–Z
      break;
    case 'rfc1459-strict':
    case 'strict-rfc1459':
      foldBrackets = true;
      break;
    case 'rfc1459':
    case '':
      foldBrackets = true;
      foldTilde = true;
      break;
    default:
      // Unknown mapping (precis/utf8/…): conservative ASCII fold.
      break;
  }

  let out = '';
  for (let i = 0; i < s.length; i++) {
    const c = s.charCodeAt(i);
    if (c >= 0x41 && c <= 0x5a) {
      // A–Z -> a–z
      out += String.fromCharCode(c + 32);
    } else if (foldBrackets && c === 0x5b) {
      out += '{'; // [ -> {
    } else if (foldBrackets && c === 0x5d) {
      out += '}'; // ] -> }
    } else if (foldBrackets && c === 0x5c) {
      out += '|'; // \ -> |
    } else if (foldTilde && c === 0x7e) {
      out += '^'; // ~ -> ^
    } else {
      out += s[i];
    }
  }
  return out;
}
