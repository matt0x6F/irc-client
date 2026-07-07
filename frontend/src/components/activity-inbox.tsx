import { useMemo } from 'react';
import { Bell, AtSign, Tag, MessageSquare, Mail, Eye, X, UserX } from 'lucide-react';
import { useNetworkStore } from '../stores/network';
import { coalesceActivity, relativeTime, type ActivityGroup } from '../lib/activity-inbox';
import { IgnoreInviteSender } from '../../wailsjs/go/main/App';

// One glyph per source type, matched to the rest of the app's iconography
// (role icons in channel-info.tsx, buddy dots in monitor-list.tsx).
const SOURCE_ICON: Record<string, typeof Bell> = {
  highlight: AtSign,
  keyword: Tag,
  pm: MessageSquare,
  invite: Mail,
};

// "2 highlights" / "3 messages" / singular for count === 1 (no summary shown then).
function summaryLabel(g: ActivityGroup): string {
  const noun =
    g.sourceType === 'highlight' ? 'highlight'
    : g.sourceType === 'keyword' ? 'keyword hit'
    : g.sourceType === 'pm' ? 'message'
    : 'invite';
  return `${g.count} ${noun}${g.count === 1 ? '' : 's'}`;
}

function ActivityRow({ group }: { group: ActivityGroup }) {
  const activateActivityGroup = useNetworkStore((s) => s.activateActivityGroup);
  const markActivitySeenMany = useNetworkStore((s) => s.markActivitySeenMany);
  const dismissActivity = useNetworkStore((s) => s.dismissActivity);
  const openOrJoinChannel = useNetworkStore((s) => s.openOrJoinChannel);

  const Icon = SOURCE_ICON[group.sourceType] ?? Bell;
  const isInvite = group.sourceType === 'invite';
  const newest = group.items.reduce((a, b) => (a.timestamp >= b.timestamp ? a : b));

  const dismissGroup = () => {
    for (const i of group.items) void dismissActivity(i.id);
  };

  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`${group.target} — open`}
      onClick={() => void activateActivityGroup(group)}
      onKeyDown={(e) => {
        if (e.key === 'Enter' || e.key === ' ') void activateActivityGroup(group);
      }}
      className={`group flex flex-col gap-1 rounded-md px-3 py-2 cursor-pointer transition-colors border-l-2 ${
        group.hasUnseen
          ? 'bg-card shadow-sm border-primary'
          : 'bg-muted/30 border-transparent hover:bg-accent/50'
      }`}
    >
      <div className="flex items-center gap-2 min-w-0">
        <Icon className={`w-3.5 h-3.5 flex-shrink-0 ${group.hasUnseen ? 'text-primary' : 'text-muted-foreground'}`} />
        <span className={`font-semibold truncate ${group.hasUnseen ? 'text-foreground' : 'text-muted-foreground'}`}>
          {group.target}
        </span>
        {group.count > 1 && (
          <span className="text-xs text-muted-foreground flex-shrink-0">{summaryLabel(group)}</span>
        )}
        <span className="ml-auto text-xs text-muted-foreground flex-shrink-0">{relativeTime(group.latest)}</span>

        {/* Hover/inline actions */}
        <div className="flex items-center gap-1 flex-shrink-0 opacity-0 group-hover:opacity-100 focus-within:opacity-100 transition-opacity">
          {isInvite && (
            <>
              <button
                aria-label="Join"
                title="Join"
                className="text-xs text-primary underline hover:opacity-80 transition-opacity"
                onClick={(e) => {
                  e.stopPropagation();
                  void openOrJoinChannel(group.networkId, group.target);
                  dismissGroup();
                }}
              >
                Join
              </button>
              <button
                aria-label="Ignore sender"
                title="Ignore sender"
                className="text-muted-foreground hover:text-destructive transition-colors cursor-pointer"
                onClick={(e) => {
                  e.stopPropagation();
                  void IgnoreInviteSender(group.networkId, group.actor);
                }}
              >
                <UserX className="w-3.5 h-3.5" />
              </button>
            </>
          )}
          {group.hasUnseen && (
            <button
              aria-label="Mark seen"
              title="Mark seen"
              className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
              onClick={(e) => {
                e.stopPropagation();
                void markActivitySeenMany(group.items.filter((i) => !i.seen).map((i) => i.id));
              }}
            >
              <Eye className="w-3.5 h-3.5" />
            </button>
          )}
          <button
            aria-label="Dismiss"
            title="Dismiss"
            className="text-muted-foreground hover:text-foreground transition-colors cursor-pointer"
            onClick={(e) => {
              e.stopPropagation();
              dismissGroup();
            }}
          >
            <X className="w-3.5 h-3.5" />
          </button>
        </div>
      </div>

      <div className={`truncate text-xs pl-5 ${group.hasUnseen ? 'text-foreground/80' : 'text-muted-foreground'}`}>
        <span className="font-medium">{newest.actor}</span>
        {newest.preview ? `: ${newest.preview}` : null}
      </div>
    </div>
  );
}

export function ActivityInbox() {
  const items = useNetworkStore((s) => s.activityItems);
  const markAllActivitySeen = useNetworkStore((s) => s.markAllActivitySeen);
  const clearSeenActivity = useNetworkStore((s) => s.clearSeenActivity);
  const clearAllActivity = useNetworkStore((s) => s.clearAllActivity);

  const groups = useMemo(() => coalesceActivity(items), [items]);
  const unseenCount = groups.filter((g) => g.hasUnseen).length;

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 px-4 py-3 border-b border-border">
        <h2 className="font-semibold text-foreground">Activity</h2>
        {unseenCount > 0 && (
          <span className="text-xs font-semibold px-1.5 py-0.5 rounded-full bg-primary/20 text-primary">
            {unseenCount}
          </span>
        )}
      </div>

      {groups.length === 0 ? (
        <div className="flex-1 flex flex-col items-center justify-center gap-2 text-muted-foreground">
          <Bell className="w-8 h-8 opacity-50" />
          <div className="text-sm">You're all caught up.</div>
        </div>
      ) : (
        <div className="flex-1 overflow-y-auto flex flex-col gap-1.5 p-3">
          {groups.map((g) => (
            <ActivityRow key={g.key} group={g} />
          ))}
        </div>
      )}

      <div className="flex items-center gap-3 px-4 py-2 border-t border-border text-xs">
        <button
          className="text-muted-foreground underline hover:text-foreground transition-colors"
          onClick={() => void markAllActivitySeen()}
        >
          Mark all seen
        </button>
        <button
          className="text-muted-foreground underline hover:text-foreground transition-colors"
          onClick={() => void clearSeenActivity()}
        >
          Clear seen
        </button>
        <button
          className="text-muted-foreground underline hover:text-foreground transition-colors ml-auto"
          onClick={() => void clearAllActivity()}
        >
          Clear all
        </button>
      </div>
    </div>
  );
}
