// Ban-mask classification for the channel mode editor. IRCv3 EXTBAN (ratified,
// incl. account-extban) lets ban masks carry a typed prefix — e.g. with
// EXTBAN=$,a... the server accepts "$a" (any logged-in user) or "$a:account".
// describeBan turns a raw mask into a human label so the ban list reads as
// "account: alice" rather than the opaque "$a:alice".

export type BanKind = 'mask' | 'account' | 'extban';

export interface BanDescription {
  kind: BanKind;
  label: string;
  mask: string; // the original mask, unchanged (used as the removal key)
}

// describeBan classifies a ban mask given the server's advertised EXTBAN prefix
// (e.g. "$"; empty when the server advertised none, in which case every mask is
// treated literally). The account type 'a' is the ratified account-extban.
export function describeBan(mask: string, extbanPrefix: string): BanDescription {
  if (extbanPrefix && mask.startsWith(extbanPrefix)) {
    const typeChar = mask[extbanPrefix.length];

    if (typeChar === 'a') {
      const rest = mask.slice(extbanPrefix.length + 1);
      if (rest === '') {
        return { kind: 'account', label: 'any logged-in account', mask };
      }
      if (rest.startsWith(':')) {
        return { kind: 'account', label: `account: ${rest.slice(1)}`, mask };
      }
    }

    // Some other extban type (realname, channel, etc.) — surface it as an extban
    // without pretending to understand its argument.
    if (typeChar !== undefined) {
      return { kind: 'extban', label: `extban: ${mask}`, mask };
    }
  }

  return { kind: 'mask', label: mask, mask };
}
