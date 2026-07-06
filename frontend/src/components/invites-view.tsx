import { useState } from 'react';
import { useNetworkStore } from '../stores/network';
import { DismissInvitesFrom, IgnoreInviteSender, DismissInvite } from '../../wailsjs/go/main/App';
import type { main } from '../../wailsjs/go/models';
import { relativeTime } from '../lib/activity-inbox';

type InviteView = main.InviteView;

export const COLLAPSE_THRESHOLD = 5;

export interface SenderGroup {
  inviter: string;
  channels: InviteView[];
  trusted: boolean;
  collapsed: boolean;
}

// groupBySender collapses invites into one group per inviter. A group is
// collapsed when its channel count is at or above the volume threshold —
// independent of trust, and non-destructively (channels are present, just hidden).
export function groupBySender(invites: InviteView[]): SenderGroup[] {
  const bySender = new Map<string, InviteView[]>();
  for (const i of invites) {
    const arr = bySender.get(i.inviter) ?? [];
    arr.push(i);
    bySender.set(i.inviter, arr);
  }
  return [...bySender.entries()].map(([inviter, channels]) => ({
    inviter,
    channels,
    trusted: channels.some((c) => c.trusted),
    collapsed: channels.length >= COLLAPSE_THRESHOLD,
  }));
}

export function InvitesView({ networkId }: { networkId: number }) {
  const invites = useNetworkStore((s) => s.invitesByNetwork[networkId] ?? []);
  const openOrJoinChannel = useNetworkStore((s) => s.openOrJoinChannel);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  const groups = groupBySender(invites);

  if (groups.length === 0) {
    return <div className="p-4 text-sm text-muted-foreground">No pending invites.</div>;
  }

  return (
    <div className="flex flex-col gap-2 p-3">
      {groups.map((g) => {
        const isExpanded = !g.collapsed || expanded[g.inviter];
        return (
          <div key={g.inviter} className="rounded-md border border-border bg-card/30 p-2">
            {/* Group header — always visible */}
            <div className="flex items-center justify-between gap-2">
              <div className="text-sm">
                <span className="font-semibold">{g.inviter}</span>{' '}
                {/* Collapsed summary: count only, no channel names (anti-harassment) */}
                {g.collapsed && !expanded[g.inviter]
                  ? <span className="text-muted-foreground">invited you to {g.channels.length} channels</span>
                  : <span className="text-muted-foreground">invited you</span>}
                {g.trusted && (
                  <span className="ml-2 text-xs text-primary font-medium">buddy</span>
                )}
              </div>
              <div className="flex items-center gap-2">
                {g.collapsed && (
                  <button
                    className="text-xs text-muted-foreground underline hover:text-foreground transition-colors"
                    onClick={() => setExpanded((e) => ({ ...e, [g.inviter]: !e[g.inviter] }))}
                  >
                    {expanded[g.inviter] ? 'Collapse' : 'Show channels'}
                  </button>
                )}
                <button
                  className="text-xs text-muted-foreground underline hover:text-foreground transition-colors"
                  onClick={() => void DismissInvitesFrom(networkId, g.inviter)}
                >
                  Dismiss all
                </button>
                <button
                  className="text-xs text-destructive underline hover:opacity-80 transition-opacity"
                  onClick={() => void IgnoreInviteSender(networkId, g.inviter)}
                >
                  Ignore sender
                </button>
              </div>
            </div>

            {/* Per-channel list — only shown when expanded */}
            {isExpanded && (
              <ul className="mt-2 flex flex-col gap-1">
                {g.channels.map((c) => (
                  <li key={c.channel} className="flex items-center justify-between text-sm">
                    <span>
                      {c.channel}{' '}
                      <span className="text-xs text-muted-foreground">{relativeTime(c.receivedAt)}</span>
                    </span>
                    <span className="flex gap-2">
                      <button
                        className="text-xs text-primary underline hover:opacity-80 transition-opacity"
                        onClick={() => {
                          void openOrJoinChannel(networkId, c.channel);
                          void DismissInvite(networkId, c.inviter, c.channel);
                        }}
                      >
                        Join
                      </button>
                      <button
                        className="text-xs text-muted-foreground underline hover:text-foreground transition-colors"
                        onClick={() => void DismissInvite(networkId, c.inviter, c.channel)}
                      >
                        Dismiss
                      </button>
                    </span>
                  </li>
                ))}
              </ul>
            )}
          </div>
        );
      })}
    </div>
  );
}
