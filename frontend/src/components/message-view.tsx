import { useEffect, useLayoutEffect, useRef, useState, useMemo, useCallback } from 'react';
import { useVirtualizer } from '@tanstack/react-virtual';
import { storage } from '../../wailsjs/go/models';
import { indexByMsgid, indexById } from '../lib/message-index';
import { IRCFormattedText } from './irc-formatted-text';
import { UserContextMenu } from './user-context-menu';
import { useNicknameColors } from '../hooks/useNicknameColors';
import { useNetworkStore } from '../stores/network';
import { useUIStore } from '../stores/ui';
import { casefold } from '../lib/casefold';
import { useSettingsStore } from '../stores/settings';
import { SendCommand } from '../../wailsjs/go/main/App';
import { CornerUpLeft, Hash } from 'lucide-react';
import { buildMsgidIndex, resolveParent, quoteSnippet } from '../lib/reply';

interface MessageViewProps {
  networkId: number | null;
  selectedChannel?: string | null;
}

type ConsolidatedMessage = storage.Message & {
  _consolidated?: boolean;
  _originalMessages?: storage.Message[];
  _nicknames?: string[];
  _actionText?: string;
}

// Optimistic messages use `Date.now()` as a placeholder id (~1.7e12) until the real
// DB row (a small autoincrement id) is loaded. Don't offer pinning on those rows.
const OPTIMISTIC_ID_THRESHOLD = 1_000_000_000_000;

// A single shared time formatter (equivalent to toLocaleTimeString()'s default
// "medium" style), reused across every row instead of re-resolving the locale
// formatter per render.
const TIME_FORMAT = new Intl.DateTimeFormat(undefined, { timeStyle: 'medium' });

export function MessageView({ networkId, selectedChannel }: MessageViewProps) {
  // Subscribe to the high-churn messages array here rather than in App (the root),
  // so a new message re-renders only this component's subtree, not the whole app.
  const messages = useNetworkStore((s) => s.messages);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const prevChannelRef = useRef<string | null | undefined>(selectedChannel);
  // Whether the pane should stay pinned to the latest message. Armed on every
  // channel switch and while the user is at the bottom; released on a genuine
  // upward scroll. While set, the layout effect re-pins to the bottom on *every*
  // render — this is what makes a channel switch (and a fast incremental flood of
  // new messages) land at the latest message regardless of load/measure timing.
  // (Scroll position across a scroll-up history prepend is preserved separately, by
  // the element-anchored restore in maybeLoadOlder.)
  const stickToBottomRef = useRef(true);
  // Last observed scrollTop, to detect scroll *direction*. The pin is released only
  // on a real upward (user) scroll — never on the transient "not at bottom" readings
  // that our own scroll-to-bottom and content-height growth produce.
  const lastScrollTopRef = useRef(0);

  // Consolidate join/quit preference, sourced from the durable settings store.
  // Subscribing here makes the message view update live when the toggle changes
  // in Settings (the store writes through to the backend settings table).
  const consolidateEnabled = useSettingsStore((s) => s.consolidateJoinQuit);
  const unfurlsEnabled = useSettingsStore((s) => s.unfurlsEnabled);

  // Consolidate messages if enabled
  const processedMessages = useMemo(() => {
    if (!consolidateEnabled) {
      return messages;
    }

    const result: ConsolidatedMessage[] = [];
    let currentGroup: storage.Message[] = [];
    let currentType: string | null = null;

    const flushGroup = () => {
      if (currentGroup.length === 0) return;
      
      if (currentGroup.length === 1) {
        // Single message, no consolidation needed
        result.push(currentGroup[0] as ConsolidatedMessage);
      } else {
        // Create consolidated message
        const firstMsg = currentGroup[0];
        // Extract unique nicknames, preserving order (first occurrence)
        const allNicknames = currentGroup.map(msg => msg.user).filter(user => user && user !== '*');
        const seen = new Set<string>();
        const nicknames = allNicknames.filter(nick => {
          if (seen.has(nick)) {
            return false;
          }
          seen.add(nick);
          return true;
        });
        
        // Check if all messages are from the same user
        const allSameUser = nicknames.length > 0 && nicknames.every(nick => nick === nicknames[0]);
        
        let nicknameList = '';
        let action = '';
        let suffix = '';
        
        if (allSameUser) {
          // Same user multiple times: "user action n times"
          const user = nicknames[0];
          const count = currentGroup.length;
          
          nicknameList = user;
          
          // Determine action verb
          if (currentType === 'join') {
            action = 'joined';
            suffix = ` ${count} time${count > 1 ? 's' : ''}`;
          } else if (currentType === 'part') {
            action = 'left';
            suffix = ` ${count} time${count > 1 ? 's' : ''}`;
            // Check if there's a reason in the first message
            const firstMsgText = firstMsg.message;
            const reasonMatch = firstMsgText.match(/\((.+)\)$/);
            if (reasonMatch) {
              suffix += ` (${reasonMatch[1]})`;
            }
          } else if (currentType === 'quit') {
            action = 'quit';
            suffix = ` ${count} time${count > 1 ? 's' : ''}`;
            // Check if there's a reason in the first message
            const firstMsgText = firstMsg.message;
            const reasonMatch = firstMsgText.match(/\((.+)\)$/);
            if (reasonMatch) {
              suffix += ` (${reasonMatch[1]})`;
            }
          }
        } else {
          // Different users: "A, B, C action"
          if (nicknames.length === 1) {
            nicknameList = nicknames[0];
          } else if (nicknames.length === 2) {
            nicknameList = `${nicknames[0]} and ${nicknames[1]}`;
          } else {
            nicknameList = `${nicknames.slice(0, -1).join(', ')}, and ${nicknames[nicknames.length - 1]}`;
          }

          // Determine action verb and suffix
          if (currentType === 'join') {
            action = nicknames.length === 1 ? 'joins' : 'join';
            suffix = ' the channel';
          } else if (currentType === 'part') {
            action = nicknames.length === 1 ? 'left' : 'left';
            suffix = ' the channel';
            // Check if there's a reason in the first message
            const firstMsgText = firstMsg.message;
            const reasonMatch = firstMsgText.match(/\((.+)\)$/);
            if (reasonMatch) {
              suffix += ` (${reasonMatch[1]})`;
            }
          } else if (currentType === 'quit') {
            action = 'quit';
            // Check if there's a reason in the first message
            const firstMsgText = firstMsg.message;
            const reasonMatch = firstMsgText.match(/\((.+)\)$/);
            if (reasonMatch) {
              suffix = ` (${reasonMatch[1]})`;
            }
          }
        }

        const consolidated: ConsolidatedMessage = {
          ...firstMsg,
          message: `${nicknameList} ${action}${suffix}`,
          _consolidated: true,
          _originalMessages: [...currentGroup],
          _nicknames: allSameUser ? [nicknames[0]] : nicknames,
          _actionText: `${action}${suffix}`,
        } as ConsolidatedMessage;
        result.push(consolidated);
      }
      currentGroup = [];
      currentType = null;
    };

    for (const msg of messages) {
      const isSystemMsg = msg.message_type === 'join' || msg.message_type === 'part' || msg.message_type === 'quit';
      
      if (isSystemMsg && msg.message_type === currentType) {
        // Continue grouping
        currentGroup.push(msg);
      } else {
        // Break grouping - flush current group
        flushGroup();
        
        if (isSystemMsg) {
          // Start new group
          currentGroup.push(msg);
          currentType = msg.message_type;
        } else {
          // Regular message - add as-is
          result.push(msg as ConsolidatedMessage);
        }
      }
    }

    // Flush any remaining group
    flushGroup();

    return result;
  }, [messages, consolidateEnabled]);

  // Extract unique nicknames from processed messages (including consolidated ones)
  const uniqueNicknames = useMemo(() => {
    const nicks = new Set<string>();
    processedMessages.forEach(msg => {
      const consolidated = msg as ConsolidatedMessage;
      if (consolidated._consolidated && consolidated._nicknames) {
        consolidated._nicknames.forEach(nick => nicks.add(nick));
      } else if (msg.user && msg.user !== '*') {
        nicks.add(msg.user);
      }
    });
    return Array.from(nicks);
  }, [processedMessages]);

  // Get nickname colors - this will fetch colors and listen for updates
  const nicknameColors = useNicknameColors(networkId, uniqueNicknames);

  // Get the current user's nickname for mention highlighting. Prefer the live
  // server-assigned nick (kept current through /nick and collision reclaims) and
  // fall back to the configured nick before the live one is known.
  const currentNickname = useNetworkStore((state) => {
    if (networkId === null) return null;
    return (
      state.currentNick[networkId] ??
      state.networks.find((n) => n.id === networkId)?.nickname ??
      null
    );
  });

  // Pinned messages / jump-to-message state
  // Bot set for this network: badge messages whose author is a recognized bot.
  // Subscribing to the Set reference re-renders when addBot replaces it.
  const botSet = useNetworkStore((s) => (networkId !== null ? s.botNicks[networkId] : undefined));
  const caseMapping = useNetworkStore((s) => (networkId !== null ? s.caseMapping?.[networkId] : undefined)) ?? '';

  // Build a msgid→message index once per render for fast parent lookup.
  const msgidIndex = useMemo(() => buildMsgidIndex(messages), [messages]);

  // Row indices for jumping under virtualization (off-screen rows aren't in the
  // DOM, so we scroll the virtualizer to an index rather than querySelector-ing).
  const rowIndexByMsgid = useMemo(() => indexByMsgid(processedMessages), [processedMessages]);
  const rowIndexById = useMemo(() => indexById(processedMessages), [processedMessages]);

  // Flash state is state-driven (not imperative classList) because a flashed row
  // may be scrolled into existence by the virtualizer only after this fires.
  const [flashMsgid, setFlashMsgid] = useState<string | null>(null);
  const [flashPinId, setFlashPinId] = useState<number | null>(null);

  const rowVirtualizer = useVirtualizer({
    count: processedMessages.length,
    getScrollElement: () => scrollContainerRef.current,
    estimateSize: () => 44,
    overscan: 14,
    getItemKey: (i) => (processedMessages[i] as storage.Message).id,
    // Take FULL manual control of scroll position on content change. anchorTo:'end'
    // did NOT push scrollTop down for prepended rows (verified on real data: the view
    // snapped to the newly-loaded oldest message), and TanStack's built-in
    // size-change adjustment fights our own scroll-restore on prepend (double-
    // compensation → residual drift). So we disable the built-in adjustment and own
    // it: the layout-effect re-pin keeps us at the bottom, and maybeLoadOlder restores
    // the offset across a prepend. NOTE: the container must NOT set
    // `scroll-behavior: smooth`, or these instant scrolls animate and race measurement.
    // @ts-expect-error present in @tanstack/virtual-core 3.17.3 (runtime); react-virtual's bundled types lag.
    shouldAdjustScrollPositionOnItemSizeChange: () => false,
  });

  // Reply jump: scroll to a message row by msgid and flash it briefly. Returns
  // true if the row exists in the current buffer, false otherwise.
  const scrollToMsgid = useCallback((msgid: string): boolean => {
    const idx = rowIndexByMsgid.get(msgid);
    if (idx === undefined) return false;
    // Explicit smooth: the container no longer defaults to CSS smooth scrolling.
    rowVirtualizer.scrollToIndex(idx, { align: 'center', behavior: 'smooth' });
    setFlashMsgid(msgid);
    setTimeout(() => setFlashMsgid((cur) => (cur === msgid ? null : cur)), 1200);
    return true;
  }, [rowIndexByMsgid, rowVirtualizer]);

  const pendingScrollMsgid = useNetworkStore((s) => s.pendingScrollMsgid);
  const clearPendingScrollMsgid = useNetworkStore((s) => s.clearPendingScrollMsgid);
  const openParentMessage = useNetworkStore((s) => s.openParentMessage);

  // Consume pendingScrollMsgid: once the new buffer's messages load and the row
  // appears in the DOM, scroll to it and clear the pending id. Runs on every
  // messages update (after selectPane loads the new buffer).
  useEffect(() => {
    if (!pendingScrollMsgid) return;
    if (scrollToMsgid(pendingScrollMsgid)) {
      clearPendingScrollMsgid();
    }
  }, [pendingScrollMsgid, messages, scrollToMsgid, clearPendingScrollMsgid]);

  const jumpToReplyMsgid = useCallback(
    (replyMsgid: string) => {
      if (!replyMsgid) return;
      if (scrollToMsgid(replyMsgid)) return;
      // Parent not in the current buffer — switch to the buffer that contains it.
      if (networkId === null) return;
      void openParentMessage(networkId, replyMsgid);
    },
    [scrollToMsgid, openParentMessage, networkId],
  );

  const pinnedMessages = useNetworkStore((s) => s.pinnedMessages);
  const pinMessage = useNetworkStore((s) => s.pinMessage);
  const unpinMessage = useNetworkStore((s) => s.unpinMessage);
  const setReplyTarget = useNetworkStore((s) => s.setReplyTarget);
  const selectPane = useNetworkStore((s) => s.selectPane);
  const openQuery = useNetworkStore((s) => s.openQuery);
  // Right-click-a-nick context menu, anchored at the click position.
  const [userMenu, setUserMenu] = useState<{ x: number; y: number; nick: string } | null>(null);
  const viewMode = useNetworkStore((s) => s.viewMode);
  const anchoredMessageId = useNetworkStore((s) => s.anchoredMessageId);
  const clearAnchorFlash = useNetworkStore((s) => s.clearAnchorFlash);
  const returnToLive = useNetworkStore((s) => s.returnToLive);
  // Report whether the pane is following the bottom so the store knows whether to
  // replace the latest-100 window (following) or merge new messages into the buffer
  // (scrolled up), keeping the read position stable on a busy channel.
  const setAtBottom = useNetworkStore((s) => s.setAtBottom);
  const newSinceAnchor = useNetworkStore((s) => s.newSinceAnchor);
  const loadOlderMessages = useNetworkStore((s) => s.loadOlderMessages);
  const loadNewerMessages = useNetworkStore((s) => s.loadNewerMessages);
  const loadingHistory = useNetworkStore((s) => s.loadingHistory);

  // Pagination state (refs so they don't trigger re-renders).
  const loadingOlderRef = useRef(false);
  const loadingNewerRef = useRef(false);
  const reachedStartRef = useRef(false);

  const pinnedIds = useMemo(() => new Set(pinnedMessages.map((p) => p.id)), [pinnedMessages]);

  // Check if a message text contains the user's nickname as a whole word (case-insensitive)
  // Build the mention matcher once per nick change, not once per message row.
  const mentionRegex = useMemo(() => {
    if (!currentNickname) return null;
    const escaped = currentNickname.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    return new RegExp(`(?:^|[^a-zA-Z0-9_])${escaped}(?:[^a-zA-Z0-9_]|$)`, 'i');
  }, [currentNickname]);

  const isMention = useCallback(
    (text: string): boolean => mentionRegex?.test(text) ?? false,
    [mentionRegex]
  );

  // Trigger color generation for users in messages by simulating message events
  // This ensures colors are generated even for old messages
  useEffect(() => {
    if (!networkId || uniqueNicknames.length === 0) return;
    
    // The plugin generates colors when it receives message.received events
    // We don't need to do anything here - the hook will fetch colors and
    // the plugin will generate them when it sees users in channel.names.complete events
    // Colors will be updated via metadata-updated events
  }, [networkId, uniqueNicknames]);

  // Drive the return-to-bottom badge (isNearBottom) and maintain the bottom-pin by
  // scroll *direction*: an upward (user) scroll releases it; reaching the bottom
  // re-engages it. Our own scroll-to-bottom and content growth only move scrollTop
  // down (or leave it), so they never spuriously release the pin.
  const checkIfNearBottom = () => {
    const container = scrollContainerRef.current;
    if (!container) return;
    const threshold = 100; // pixels from bottom
    const scrollTop = container.scrollTop;
    const isNear = container.scrollHeight - scrollTop - container.clientHeight < threshold;
    setIsNearBottom(isNear);

    if (scrollTop < lastScrollTopRef.current - 4) {
      stickToBottomRef.current = false;
    } else if (isNear) {
      stickToBottomRef.current = true;
    }
    lastScrollTopRef.current = scrollTop;
    // Mirror the follow-state to the store (no-op when unchanged) so loadMessages
    // merges rather than replaces the buffer while the user is scrolled up.
    setAtBottom(stickToBottomRef.current);
  };

  // When scrolled near the bottom while anchored (after a jump-to-pin or
  // scrollback), load the next newer page to bridge toward live. When there are
  // no more newer rows, loadNewerMessages flips back to live (badge clears).
  // No scroll adjustment needed — appends add content below the viewport.
  const maybeLoadNewer = () => {
    const container = scrollContainerRef.current;
    if (!container || loadingNewerRef.current || loadingOlderRef.current) return;
    if (useNetworkStore.getState().viewMode !== 'anchored') return;
    if (container.scrollHeight - container.scrollTop - container.clientHeight > 120) return;

    loadingNewerRef.current = true;
    loadNewerMessages().finally(() => {
      loadingNewerRef.current = false;
    });
  };

  // Always-current processedMessages, so the async scroll-restore can map the anchor
  // message id → its live row index after the prepend.
  const procRef = useRef(processedMessages);
  procRef.current = processedMessages;

  // When scrolled to the top, load an older page and keep the reading position fixed
  // across the prepend (see the element-anchored restore inside). Stops once the
  // backend reports no more history.
  const maybeLoadOlder = () => {
    const container = scrollContainerRef.current;
    if (!container || loadingOlderRef.current || reachedStartRef.current) return;
    // While pinned to the bottom (including the channel-switch window, where
    // selectedChannel and `messages` are briefly out of sync) don't paginate — a
    // stray scroll event must not page the previous channel's history.
    if (stickToBottomRef.current) return;
    if (container.scrollTop > 80) return;

    loadingOlderRef.current = true;
    // Preserve the reading position across the prepend by pinning to the ACTUAL anchor
    // ELEMENT — the row currently at the top of the viewport (by message id + its
    // on-screen offset). After the prepend we keep putting that row back at its offset
    // until the prepended rows stop re-measuring. Element-anchored (not a
    // scrollHeight-delta), so the estimate→measured settling — and the windowing that
    // reveals rows taller than estimateSize as you scroll — can't make it drift or run
    // away. The pagination lock is held until settled, so a fast scroll can't spawn
    // overlapping restores that fight over scrollTop. Pairs with
    // shouldAdjustScrollPositionOnItemSizeChange:false — we own all scroll compensation.
    const prevHeight = container.scrollHeight;
    const prevTop = container.scrollTop;
    const listTop0 = container.getBoundingClientRect().top;
    let anchorId: number | null = null;
    let anchorOffset = 0;
    for (const rowEl of container.querySelectorAll('[data-index]')) {
      const rect = (rowEl as HTMLElement).getBoundingClientRect();
      if (rect.bottom > listTop0 + 1) {
        anchorId = (processedMessages[Number(rowEl.getAttribute('data-index'))] as storage.Message)?.id ?? null;
        anchorOffset = rect.top - listTop0;
        break;
      }
    }
    loadOlderMessages().then((added) => {
      if (added > 0 && anchorId != null) {
        let stableFrames = 0;
        let lastSH = -1;
        let elapsed = 0;
        const step = () => {
          const c = scrollContainerRef.current;
          if (!c) { loadingOlderRef.current = false; return; }
          const prevBehavior = c.style.scrollBehavior;
          c.style.scrollBehavior = 'auto';
          // Coarse height-delta first, to bring the anchor row back into the mounted
          // window; then correct to the anchor row's real on-screen offset.
          c.scrollTop = c.scrollHeight - prevHeight + prevTop;
          const idx = procRef.current.findIndex((m) => (m as storage.Message).id === anchorId);
          const rowEl = idx >= 0 ? c.querySelector(`[data-index="${idx}"]`) : null;
          if (rowEl) {
            const cur = (rowEl as HTMLElement).getBoundingClientRect().top - c.getBoundingClientRect().top;
            c.scrollTop += cur - anchorOffset;
          }
          c.style.scrollBehavior = prevBehavior;
          if (c.scrollHeight === lastSH) stableFrames++;
          else { stableFrames = 0; lastSH = c.scrollHeight; }
          elapsed += 16;
          if (stableFrames < 5 && elapsed < 600) requestAnimationFrame(step);
          else loadingOlderRef.current = false;
        };
        step();
      } else {
        if (added === 0) reachedStartRef.current = true;
        loadingOlderRef.current = false;
      }
    });
  };

  const handleScroll = () => {
    checkIfNearBottom();
    maybeLoadOlder();
    maybeLoadNewer();
  };

  // Reset pagination state when the channel changes.
  useEffect(() => {
    reachedStartRef.current = false;
    loadingOlderRef.current = false;
    loadingNewerRef.current = false;
  }, [selectedChannel]);

  // Handle scroll events
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    container.addEventListener('scroll', handleScroll);
    return () => container.removeEventListener('scroll', handleScroll);
  }, []);

  // Jump-to-message: scroll to the anchored message and flash it briefly.
  useEffect(() => {
    if (anchoredMessageId == null) return;
    const idx = rowIndexById.get(anchoredMessageId);
    if (idx === undefined) return;
    rowVirtualizer.scrollToIndex(idx, { align: 'center', behavior: 'smooth' });
    setFlashPinId(anchoredMessageId);
    const timeout = setTimeout(() => {
      setFlashPinId((cur) => (cur === anchoredMessageId ? null : cur));
      clearAnchorFlash(); // clears the id but keeps viewMode 'anchored' (poll stays frozen)
    }, 1600);
    return () => clearTimeout(timeout);
  }, [anchoredMessageId, rowIndexById, rowVirtualizer, clearAnchorFlash]);

  // Keep the pane pinned to the latest message while "stuck to bottom".
  //
  // selectedChannel updates synchronously on a switch, but the new buffer loads a
  // round-trip later (selectPane keeps the old rows visible meanwhile). Rather than
  // race that load, every channel switch (re-)arms stickToBottom and this layout
  // effect re-pins to the bottom on *every* render while stuck — so the landing is
  // independent of load timing/order, and a fast incremental flood of new messages
  // can't strand the pane part-way up. scrollToEnd is instant here (the container no
  // longer forces CSS smooth) and reconciles to the true last row as it measures.
  // The user scrolling up releases the pin (see checkIfNearBottom).
  useLayoutEffect(() => {
    if (viewMode === 'anchored') {
      // Anchored (jump-to-pin / scrollback) view manages its own scroll; just track
      // the channel so a later switch back is detected.
      prevChannelRef.current = selectedChannel;
      return;
    }

    if (selectedChannel !== prevChannelRef.current) {
      prevChannelRef.current = selectedChannel;
      stickToBottomRef.current = true; // a freshly opened pane starts at the latest message
      setIsNearBottom(true);
    }

    if (stickToBottomRef.current && processedMessages.length > 0) {
      rowVirtualizer.scrollToEnd();
    }
  }, [selectedChannel, messages, viewMode, processedMessages.length, rowVirtualizer]);

  // Scroll-to-bottom / return-to-live handler for the floating badge.
  const handleScrollToBottom = useCallback(() => {
    if (viewMode === 'anchored') {
      returnToLive(); // reloads the latest messages and flips back to live mode
    }
    // Re-arm the pin; the layout effect refines to the exact last row as rows
    // measure (and once returnToLive's new buffer lands). scrollToEnd animates for
    // the live case since the container no longer defaults to CSS smooth.
    stickToBottomRef.current = true;
    setIsNearBottom(true);
    setAtBottom(true); // resume plain latest-100 loads (returnToLive also sets this)
    if (viewMode !== 'anchored') rowVirtualizer.scrollToEnd({ behavior: 'smooth' });
  }, [viewMode, returnToLive, rowVirtualizer, setAtBottom]);

  const showScrollBadge = viewMode === 'anchored' || !isNearBottom;

  return (
    <div className="relative h-full">
    {/* Top-of-list spinner while server-side history (CHATHISTORY) is loading.
        Absolutely positioned so it overlays without changing scrollHeight, which
        would otherwise break the scroll-preservation math on prepend. */}
    {loadingHistory && (
      <div
        className="absolute top-2 left-1/2 -translate-x-1/2 z-10 flex items-center gap-2 rounded-full bg-muted/90 px-3 py-1 text-xs text-muted-foreground shadow-sm backdrop-blur"
        data-testid="history-loading"
      >
        <span className="h-3 w-3 animate-spin rounded-full border-2 border-current border-t-transparent" />
        Loading older messages…
      </div>
    )}
    <div
      ref={scrollContainerRef}
      className="h-full overflow-y-auto"
      onScroll={handleScroll}
      data-testid="message-list"
    >
      {processedMessages.length === 0 ? (
        <div className="text-center text-muted-foreground py-12 px-4">
          <div className="text-4xl mb-3 opacity-50">💬</div>
          <div className="text-lg font-medium mb-1">No messages yet</div>
          <div className="text-sm">Start chatting to see messages here!</div>
        </div>
      ) : (
        <div style={{ height: `${rowVirtualizer.getTotalSize()}px`, width: '100%', position: 'relative' }}>
        {rowVirtualizer.getVirtualItems().map((virtualRow) => {
          const index = virtualRow.index;
          const msg = processedMessages[index];
          const consolidated = msg as ConsolidatedMessage;
          const isConsolidated = consolidated._consolidated === true;
          const isError = msg.message_type === 'error';
          const isWarning = msg.message_type === 'warning';
          const isStatus = msg.message_type === 'status';
          const isCommand = msg.message_type === 'command';
          const isSystemMessage = msg.message_type === 'join' || msg.message_type === 'part' || msg.message_type === 'quit' || msg.message_type === 'mode';
          const isEven = index % 2 === 0;
          const isRegularMessage = !isError && !isWarning && !isStatus && !isCommand && !isSystemMessage;
          const hasMention = isRegularMessage && isMention(msg.message);
          const isFlashing = !!msg.msgid && msg.msgid === flashMsgid;
          const isPinFlashing = msg.id === flashPinId;

          return (
            <div
              key={virtualRow.key}
              data-index={index}
              ref={rowVirtualizer.measureElement}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${virtualRow.start}px)`,
                paddingLeft: '1rem',
                paddingRight: '1rem',
              }}
            >
            <div
              data-testid="message-item"
              data-msgid={msg.msgid || undefined}
              className={`group flex flex-col py-1 px-2 rounded transition-colors ${isFlashing ? 'msg-flash' : ''} ${isPinFlashing ? 'pin-flash' : ''} ${
                hasMention
                  ? 'cc-mention border-l-2'
                  : isError
                  ? 'bg-destructive/10 border-l-2 border-destructive shadow-[var(--shadow-sm)]'
                  : isWarning
                  ? 'bg-amber-500/10 border-l-2 border-amber-500'
                  : isStatus || isCommand
                  ? 'opacity-70'
                  : isEven
                  ? 'bg-muted/20'
                  : ''
              } hover:bg-muted/30`}
            >
              {msg.reply_msgid && (() => {
                const parent = resolveParent(msg.reply_msgid, msgidIndex);
                return (
                  <button
                    type="button"
                    className="reply-quote flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground self-start mb-0.5"
                    onClick={() => jumpToReplyMsgid(msg.reply_msgid)}
                  >
                    <CornerUpLeft className="h-3 w-3 shrink-0" />
                    {parent ? (
                      <span className="truncate max-w-xs">
                        <span className="font-medium">{parent.user}</span>
                        {': '}
                        {quoteSnippet(parent)}
                      </span>
                    ) : (
                      <span className="italic">replying to an earlier message</span>
                    )}
                  </button>
                );
              })()}
              <div className="flex items-baseline space-x-3">
              {isError || isWarning ? (
                <>
                  <span className={`text-sm font-semibold flex-shrink-0 ${isError ? 'text-destructive' : 'text-amber-500'}`}>⚠</span>
                  <span className="hidden sm:inline text-xs text-muted-foreground/70 flex-shrink-0 font-mono">
                    {TIME_FORMAT.format(new Date(msg.timestamp))}
                  </span>
                  <span className={`text-sm flex-1 font-medium ${isError ? 'text-destructive' : 'text-amber-500'}`}>
                    {msg.message.replace(/^Error: /, '')}
                  </span>
                </>
              ) : (
                <>
                  <span className="hidden sm:inline text-xs text-muted-foreground/60 flex-shrink-0 font-mono">
                    {TIME_FORMAT.format(new Date(msg.timestamp))}
                  </span>
                  {msg.user !== '*' && !isSystemMessage && (
                    <span
                      data-testid="author-nick"
                      className={`text-sm font-medium ${
                        isStatus || isCommand ? 'text-muted-foreground italic' : 'text-primary cursor-pointer hover:underline'
                      }`}
                      style={{
                        color: (isStatus || isCommand) ? undefined : (nicknameColors.get(msg.user) || undefined)
                      }}
                      title={isStatus || isCommand ? undefined : `Double-click to message ${msg.user}, right-click for actions`}
                      onDoubleClick={() => {
                        if (!isStatus && !isCommand && networkId !== null) {
                          void openQuery(networkId, msg.user);
                        }
                      }}
                      onContextMenu={(e) => {
                        if (isStatus || isCommand || networkId === null) return;
                        e.preventDefault();
                        e.stopPropagation();
                        window.getSelection?.()?.removeAllRanges();
                        setUserMenu({ x: e.clientX, y: e.clientY, nick: msg.user });
                      }}
                    >
                      {msg.user}
                    </span>
                  )}
                  {msg.user !== '*' && !isSystemMessage && botSet?.has(casefold(caseMapping, msg.user)) && (
                    <span
                      className="text-[10px] uppercase font-semibold tracking-wide px-1 py-0.5 rounded bg-accent text-muted-foreground flex-shrink-0"
                      title="This user is a bot (IRCv3 bot mode)"
                    >
                      bot
                    </span>
                  )}
                  {!msg.channel_id && msg.channel_context && networkId !== null && (
                    <button
                      type="button"
                      className="channel-context-pill inline-flex items-center gap-1 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground hover:text-foreground flex-shrink-0"
                      onClick={() => void selectPane(networkId, msg.channel_context)}
                      title={`This message is about ${msg.channel_context}`}
                    >
                      <Hash className="h-3 w-3" />
                      {msg.channel_context.replace(/^#/, '')}
                    </button>
                  )}
                  {isSystemMessage ? (
                    <span className="text-sm flex-1 text-muted-foreground">
                      {isConsolidated && consolidated._nicknames && consolidated._actionText ? (
                        // Render consolidated message with colored nicknames
                        (() => {
                          const parts: (string | React.ReactElement)[] = [];
                          const nicknames = consolidated._nicknames;
                          const actionText = consolidated._actionText;
                          
                          // Build the message with colored nicknames
                          nicknames.forEach((nick, idx) => {
                            const color = nicknameColors.get(nick);
                            const isLast = idx === nicknames.length - 1;
                            
                            // Add the colored nickname
                            parts.push(
                              <span key={`nick-${idx}`} style={{ color: color || undefined }} className="font-medium">
                                {nick}
                              </span>
                            );
                            
                            // Add separator if not last
                            if (!isLast) {
                              if (nicknames.length === 2) {
                                // Two nicknames: "A and B"
                                parts.push(<span key={`sep-${idx}`}> and </span>);
                              } else if (idx < nicknames.length - 2) {
                                // Not the last two: "A, "
                                parts.push(<span key={`sep-${idx}`}>, </span>);
                              } else {
                                // Last separator: ", and "
                                parts.push(<span key={`sep-${idx}`}>, and </span>);
                              }
                            }
                          });
                          
                          // Add the action text
                          parts.push(<span key="action"> {actionText}</span>);
                          
                          return parts;
                        })()
                      ) : msg.user !== '*' && nicknameColors.get(msg.user) ? (
                        // For regular system messages, color the nickname within the message text
                        msg.message.split(new RegExp(`(${msg.user.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')})`)).map((part, i) => 
                          part === msg.user ? (
                            <span key={i} style={{ color: nicknameColors.get(msg.user) }} className="font-medium">
                              {part}
                            </span>
                          ) : (
                            <span key={i}>{part}</span>
                          )
                        )
                      ) : (
                        msg.message
                      )}
                    </span>
                  ) : (
                    <IRCFormattedText
                      text={msg.message}
                      networkId={networkId ?? undefined}
                      className={`text-sm flex-1 ${
                        isStatus || isCommand ? 'text-muted-foreground italic' : ''
                      }`}
                      enableUnfurls={unfurlsEnabled}
                    />
                  )}
                </>
              )}
              {isRegularMessage && msg.msgid && (
                <button
                  onClick={() =>
                    setReplyTarget({ msgid: msg.msgid, nick: msg.user, snippet: quoteSnippet(msg, 60) })
                  }
                  data-testid="reply-button"
                  className="self-start flex-shrink-0 p-0.5 rounded transition-opacity cursor-pointer hover:text-foreground opacity-0 group-hover:opacity-100 focus:opacity-100 text-muted-foreground"
                  title="Reply to this message"
                  aria-label="Reply to this message"
                >
                  <CornerUpLeft width="14" height="14" />
                </button>
              )}
              {isRegularMessage && msg.id < OPTIMISTIC_ID_THRESHOLD && (
                <button
                  onClick={() =>
                    pinnedIds.has(msg.id) ? unpinMessage(msg.id) : pinMessage(msg.id)
                  }
                  data-testid="pin-button"
                  className={`self-start flex-shrink-0 p-0.5 rounded transition-opacity cursor-pointer hover:text-foreground ${
                    pinnedIds.has(msg.id)
                      ? 'opacity-100 text-primary'
                      : 'opacity-0 group-hover:opacity-100 focus:opacity-100 text-muted-foreground'
                  }`}
                  title={pinnedIds.has(msg.id) ? 'Unpin message' : 'Pin message'}
                  aria-label={pinnedIds.has(msg.id) ? 'Unpin message' : 'Pin message'}
                >
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    width="14"
                    height="14"
                    viewBox="0 0 24 24"
                    fill={pinnedIds.has(msg.id) ? 'currentColor' : 'none'}
                    stroke="currentColor"
                    strokeWidth="2"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                  >
                    <path d="M12 17v5" />
                    <path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z" />
                  </svg>
                </button>
              )}
              </div>
            </div>
            </div>
          );
        })}
        </div>
      )}
    </div>

      {showScrollBadge && (
        <button
          onClick={handleScrollToBottom}
          data-testid="scroll-to-bottom"
          className="absolute bottom-4 right-4 z-20 flex items-center gap-1.5 rounded-full bg-primary text-primary-foreground shadow-[var(--shadow-md)] px-3 py-1.5 text-xs font-medium hover:opacity-90 transition-opacity cursor-pointer"
          title={viewMode === 'anchored' ? 'Return to latest messages' : 'Scroll to bottom'}
        >
          {newSinceAnchor > 0 && <span>{newSinceAnchor} new</span>}
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2.5"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <path d="M12 5v14" />
            <path d="m19 12-7 7-7-7" />
          </svg>
        </button>
      )}
      {userMenu && networkId !== null && (
        <UserContextMenu
          networkId={networkId}
          channelName={selectedChannel ?? null}
          targetNick={userMenu.nick}
          currentNickname={currentNickname}
          x={userMenu.x}
          y={userMenu.y}
          onClose={() => setUserMenu(null)}
          onSendCommand={(command) => SendCommand(networkId, command)}
          onShowUserInfo={(nick) =>
            useUIStore.getState().setShowUserInfo({ networkId, nickname: nick })
          }
        />
      )}
    </div>
  );
}

