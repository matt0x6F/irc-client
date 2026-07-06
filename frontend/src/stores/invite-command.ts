// expandInvite normalizes "/invite <user>" against the active pane.
// - returns the expanded command string when the pane is a channel,
// - returns { error } when it can't resolve a channel (status / PM / activity pane),
// - returns null when the command isn't an /invite needing expansion.
export function expandInvite(
  command: string,
  selectedChannel: string | null
): string | { error: string } | null {
  const m = /^\/invite\s+(\S+)\s*(\S+)?\s*$/i.exec(command.trim());
  if (!m) return null;
  const user = m[1];
  const explicitChannel = m[2];
  if (explicitChannel) return command.trim(); // already has a channel — pass through unchanged
  const isChannelPane =
    !!selectedChannel &&
    selectedChannel !== 'status' &&
    selectedChannel !== 'activity' &&
    !selectedChannel.startsWith('pm:');
  if (!isChannelPane) {
    return { error: 'Open a channel, or use /invite <user> #channel' };
  }
  return `/invite ${user} ${selectedChannel}`;
}
