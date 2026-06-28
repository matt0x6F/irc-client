import { storage } from '../../wailsjs/go/models'

// buildMsgidIndex builds a Map from msgid → message for fast parent lookup.
// Messages with an empty msgid are skipped (they cannot be referenced as parents).
export function buildMsgidIndex(messages: storage.Message[]): Map<string, storage.Message> {
  const idx = new Map<string, storage.Message>()
  for (const m of messages) {
    if (m.msgid) idx.set(m.msgid, m)
  }
  return idx
}

// resolveParent looks up the parent message for a reply tag synchronously.
// Returns null if replyMsgid is empty or not found in the index.
export function resolveParent(
  replyMsgid: string,
  index: Map<string, storage.Message>,
): storage.Message | null {
  if (!replyMsgid) return null
  return index.get(replyMsgid) ?? null
}

// quoteSnippet returns a single-line preview of a message's text, collapsed to
// at most max characters (default 80). Newlines and runs of whitespace are
// collapsed to a single space before truncation.
export function quoteSnippet(msg: storage.Message, max = 80): string {
  const oneLine = msg.message.replace(/\s+/g, ' ').trim()
  return oneLine.length > max ? oneLine.slice(0, max) : oneLine
}
