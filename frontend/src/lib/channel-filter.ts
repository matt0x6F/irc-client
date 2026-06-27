// Client-side channel-list filtering for the Browse Channels modal.
//
// The list we filter is already downloaded, and each row carries only what
// RPL_LIST provides: channel name, user count, and topic. So we support the
// ELIST-style criteria that map onto that data — user-count bounds (>N, <N) and
// name/topic text — and deliberately not the time-based ones (C>, T>), which
// would need timestamps RPL_LIST never sends.

export interface ChannelFilterTarget {
  channel: string;
  users: number;
  topic: string;
}

export interface ParsedChannelFilter {
  minUsers?: number; // ">N": strictly more than N users
  maxUsers?: number; // "<N": strictly fewer than N users
  terms: string[]; // substring terms matched against name or topic (lowercased)
}

// parseChannelFilter splits a whitespace-separated filter into predicates. Tokens
// of the form >N / <N set user-count bounds; every other token is a text term
// (surrounding glob stars are stripped so "*linux*" behaves like "linux").
export function parseChannelFilter(raw: string): ParsedChannelFilter {
  const result: ParsedChannelFilter = { terms: [] };
  for (const token of raw.trim().split(/\s+/).filter(Boolean)) {
    const gt = /^>(\d+)$/.exec(token);
    if (gt) {
      result.minUsers = Number(gt[1]);
      continue;
    }
    const lt = /^<(\d+)$/.exec(token);
    if (lt) {
      result.maxUsers = Number(lt[1]);
      continue;
    }
    const term = token.replace(/\*/g, '').toLowerCase();
    if (term) result.terms.push(term);
  }
  return result;
}

// matchesChannelFilter reports whether a channel satisfies every parsed criterion
// (bounds and text terms are conjunctive, mirroring server-side ELIST semantics).
export function matchesChannelFilter(
  ch: ChannelFilterTarget,
  filter: ParsedChannelFilter
): boolean {
  if (filter.minUsers !== undefined && !(ch.users > filter.minUsers)) return false;
  if (filter.maxUsers !== undefined && !(ch.users < filter.maxUsers)) return false;

  if (filter.terms.length > 0) {
    const name = ch.channel.toLowerCase();
    const topic = ch.topic.toLowerCase();
    for (const term of filter.terms) {
      if (!name.includes(term) && !topic.includes(term)) return false;
    }
  }
  return true;
}
