import { useEffect, useLayoutEffect, useRef, useState, useMemo, useCallback } from 'react';
import { storage } from '../../wailsjs/go/models';
import { IRCFormattedText } from './irc-formatted-text';
import { useNicknameColors } from '../hooks/useNicknameColors';
import { useNetworkStore } from '../stores/network';

interface MessageViewProps {
  messages: storage.Message[];
  networkId: number | null;
  selectedChannel?: string | null;
}

type ConsolidatedMessage = storage.Message & {
  _consolidated?: boolean;
  _originalMessages?: storage.Message[];
  _nicknames?: string[];
  _actionText?: string;
}

const CONSOLIDATE_JOIN_QUIT_KEY = 'cascade-chat-consolidate-join-quit';

// Optimistic messages use `Date.now()` as a placeholder id (~1.7e12) until the real
// DB row (a small autoincrement id) is loaded. Don't offer pinning on those rows.
const OPTIMISTIC_ID_THRESHOLD = 1_000_000_000_000;

export function MessageView({ messages, networkId, selectedChannel }: MessageViewProps) {
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const prevMessagesLengthRef = useRef(0);
  const prevChannelRef = useRef<string | null | undefined>(selectedChannel);
  // Whether the pane should stay pinned to the latest message. Armed on every
  // channel switch and whenever the user is at the bottom; released when the user
  // scrolls up. While set, the pane is kept at the bottom across *all* async message
  // updates — so a channel switch lands on the latest message regardless of how the
  // load races the render (the original bug), and stale/out-of-order loads can't
  // strand it part-way up.
  const stickToBottomRef = useRef(true);
  // Last observed scrollTop, used to detect the *direction* of a scroll. The pin is
  // released only on a genuine upward (user) scroll — never on the transient
  // "not at bottom" readings that our own programmatic scroll-to-bottom and
  // content-height growth produce, which is what made a naive isNear-based pin flaky.
  const lastScrollTopRef = useRef(0);

  // Load consolidate setting from localStorage and make it reactive
  const [consolidateEnabled, setConsolidateEnabled] = useState(() => {
    try {
      const saved = localStorage.getItem(CONSOLIDATE_JOIN_QUIT_KEY);
      return saved === 'true';
    } catch (error) {
      return false;
    }
  });

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

  // Get the current user's nickname for mention highlighting
  const currentNickname = useNetworkStore((state) => {
    if (networkId === null) return null;
    const network = state.networks.find((n) => n.id === networkId);
    return network?.nickname || null;
  });

  // Pinned messages / jump-to-message state
  const pinnedMessages = useNetworkStore((s) => s.pinnedMessages);
  const pinMessage = useNetworkStore((s) => s.pinMessage);
  const unpinMessage = useNetworkStore((s) => s.unpinMessage);
  const viewMode = useNetworkStore((s) => s.viewMode);
  const anchoredMessageId = useNetworkStore((s) => s.anchoredMessageId);
  const clearAnchorFlash = useNetworkStore((s) => s.clearAnchorFlash);
  const returnToLive = useNetworkStore((s) => s.returnToLive);
  const newSinceAnchor = useNetworkStore((s) => s.newSinceAnchor);
  const loadOlderMessages = useNetworkStore((s) => s.loadOlderMessages);
  const loadNewerMessages = useNetworkStore((s) => s.loadNewerMessages);
  const loadingHistory = useNetworkStore((s) => s.loadingHistory);

  // Pagination state (refs so they don't trigger re-renders).
  const loadingOlderRef = useRef(false);
  const loadingNewerRef = useRef(false);
  const reachedStartRef = useRef(false);

  const pinnedIds = useMemo(() => new Set(pinnedMessages.map((p) => p.id)), [pinnedMessages]);
  // Map of message id -> row element, used to scroll/flash a specific message
  // (there is no virtualization, so every row is in the DOM).
  const messageRefs = useRef<Map<number, HTMLDivElement>>(new Map());

  // Check if a message text contains the user's nickname as a whole word (case-insensitive)
  const isMention = useCallback(
    (text: string): boolean => {
      if (!currentNickname) return false;
      const escaped = currentNickname.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const pattern = new RegExp(`(?:^|[^a-zA-Z0-9_])${escaped}(?:[^a-zA-Z0-9_]|$)`, 'i');
      return pattern.test(text);
    },
    [currentNickname]
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

  // Check if user is near bottom of scroll
  const checkIfNearBottom = () => {
    if (!scrollContainerRef.current) return;
    const container = scrollContainerRef.current;
    const threshold = 100; // pixels from bottom
    const scrollTop = container.scrollTop;
    const isNear = container.scrollHeight - scrollTop - container.clientHeight < threshold;
    setIsNearBottom(isNear);

    // Maintain the bottom-pin by scroll *direction*, not by raw position:
    //  - scrolling up (the user moving away from the latest message) releases it;
    //  - reaching the bottom re-engages it.
    // Programmatic scroll-to-bottom and content growth only ever move scrollTop down
    // (or leave it), so they never spuriously release the pin.
    if (scrollTop < lastScrollTopRef.current - 4) {
      stickToBottomRef.current = false;
    } else if (isNear) {
      stickToBottomRef.current = true;
    }
    lastScrollTopRef.current = scrollTop;
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

  // When scrolled to the top, load an older page and preserve the viewport so the
  // content doesn't jump. Stops once the backend reports no more history.
  const maybeLoadOlder = () => {
    const container = scrollContainerRef.current;
    if (!container || loadingOlderRef.current || reachedStartRef.current) return;
    // While pinned to the bottom (including the channel-switch window, where
    // selectedChannel and `messages` are briefly out of sync) don't paginate — a
    // stray scroll event must not page the previous channel's history.
    if (stickToBottomRef.current) return;
    if (container.scrollTop > 80) return;

    loadingOlderRef.current = true;
    const prevHeight = container.scrollHeight;
    const prevTop = container.scrollTop;
    loadOlderMessages().then((added) => {
      if (added > 0) {
        // Keep the previously-top message visually fixed after prepending.
        // Bypass the container's smooth scroll-behavior so this is instant.
        requestAnimationFrame(() => {
          const c = scrollContainerRef.current;
          if (!c) return;
          const prevBehavior = c.style.scrollBehavior;
          c.style.scrollBehavior = 'auto';
          c.scrollTop = c.scrollHeight - prevHeight + prevTop;
          c.style.scrollBehavior = prevBehavior;
        });
      } else {
        reachedStartRef.current = true;
      }
      loadingOlderRef.current = false;
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
    const el = messageRefs.current.get(anchoredMessageId);
    if (!el) return;
    el.scrollIntoView({ behavior: 'smooth', block: 'center' });
    el.classList.add('pin-flash');
    const timeout = setTimeout(() => {
      el.classList.remove('pin-flash');
      clearAnchorFlash(); // clears the id but keeps viewMode 'anchored' (poll stays frozen)
    }, 1600);
    return () => clearTimeout(timeout);
  }, [anchoredMessageId, messages, clearAnchorFlash]);

  // Keep the pane pinned to the latest message while "stuck to bottom".
  //
  // The original bug: selectedChannel updates synchronously on a switch, but
  // loadMessages() resolves the new channel's messages a round-trip later, so the
  // first render after a switch still shows the *old* pane. The previous code
  // advanced prevChannelRef and fired a single 100ms timeout on that first render,
  // so the scroll raced (and usually lost to) the async load — leaving the pane
  // stranded at the old scroll position, blank/short of the latest message until
  // the user scrolled.
  //
  // Instead, every channel switch (re-)arms stickToBottom, and this layout effect
  // re-pins to the bottom on *every* subsequent render while stuck. That makes the
  // landing position independent of load timing/order: whenever the real content
  // arrives (or a later poll/new message updates it) we are already at the bottom.
  // The scroll is instant (behavior: auto) inside a layout effect, so the pane never
  // paints at a stale position and there's no smooth-scroll-during-swap flicker.
  // The user scrolling up releases the pin (see checkIfNearBottom).
  useLayoutEffect(() => {
    if (viewMode === 'anchored') {
      // Anchored (jump-to-pin) view manages its own scroll; just track the channel.
      prevChannelRef.current = selectedChannel;
      return;
    }

    if (selectedChannel !== prevChannelRef.current) {
      prevChannelRef.current = selectedChannel;
      stickToBottomRef.current = true; // a freshly opened pane starts at the latest message
      setIsNearBottom(true);
    }

    if (stickToBottomRef.current) {
      const container = scrollContainerRef.current;
      if (container) {
        const prevBehavior = container.style.scrollBehavior;
        container.style.scrollBehavior = 'auto';
        container.scrollTop = container.scrollHeight;
        container.style.scrollBehavior = prevBehavior;
      }
    }
  }, [selectedChannel, messages, viewMode]);

  // Auto-scroll only if user is near bottom and there are new messages
  useEffect(() => {
    const hasNewMessages = messages.length > prevMessagesLengthRef.current;
    prevMessagesLengthRef.current = messages.length;

    if (isNearBottom && hasNewMessages && viewMode !== 'anchored') {
      // Small delay to ensure DOM is updated
      setTimeout(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      }, 50);
    }
  }, [messages, isNearBottom, viewMode]);

  // Scroll-to-bottom / return-to-live handler for the floating badge.
  const handleScrollToBottom = useCallback(() => {
    const wasAnchored = viewMode === 'anchored';
    if (wasAnchored) {
      returnToLive(); // reloads the latest messages and flips back to live mode
    }
    setTimeout(
      () => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
        setIsNearBottom(true);
      },
      wasAnchored ? 150 : 0
    );
  }, [viewMode, returnToLive]);

  // Listen for setting changes
  useEffect(() => {
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === CONSOLIDATE_JOIN_QUIT_KEY) {
        setConsolidateEnabled(e.newValue === 'true');
      }
    };
    
    // Listen for storage events (changes from other tabs/windows)
    window.addEventListener('storage', handleStorageChange);
    
    // Also poll for changes in the same window (localStorage events don't fire in same window)
    const interval = setInterval(() => {
      try {
        const current = localStorage.getItem(CONSOLIDATE_JOIN_QUIT_KEY) === 'true';
        if (current !== consolidateEnabled) {
          setConsolidateEnabled(current);
        }
      } catch (e) {
        // Ignore
      }
    }, 200);

    return () => {
      window.removeEventListener('storage', handleStorageChange);
      clearInterval(interval);
    };
  }, [consolidateEnabled]);

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
      className="h-full overflow-y-auto p-4 space-y-1"
      onScroll={handleScroll}
      style={{ scrollBehavior: 'smooth' }}
      data-testid="message-list"
    >
      {processedMessages.length === 0 ? (
        <div className="text-center text-muted-foreground py-12 px-4">
          <div className="text-4xl mb-3 opacity-50">💬</div>
          <div className="text-lg font-medium mb-1">No messages yet</div>
          <div className="text-sm">Start chatting to see messages here!</div>
        </div>
      ) : (
        processedMessages.map((msg, index) => {
          const consolidated = msg as ConsolidatedMessage;
          const isConsolidated = consolidated._consolidated === true;
          const isError = msg.message_type === 'error';
          const isStatus = msg.message_type === 'status';
          const isCommand = msg.message_type === 'command';
          const isMarker = msg.message_type === 'marker';
          const isSystemMessage = msg.message_type === 'join' || msg.message_type === 'part' || msg.message_type === 'quit' || msg.message_type === 'mode';
          const isEven = index % 2 === 0;
          const isRegularMessage = !isError && !isStatus && !isCommand && !isMarker && !isSystemMessage;
          const hasMention = isRegularMessage && isMention(msg.message);

          // Connection delineation marker ("Disconnected"/"Reconnected"): a centered
          // divider line that brackets a drop→reconnect gap in the buffer's scrollback.
          // Rendered as its own row rather than a normal message (no avatar, no pin).
          if (isMarker) {
            return (
              <div
                key={msg.id}
                ref={(el) => {
                  if (el) messageRefs.current.set(msg.id, el);
                  else messageRefs.current.delete(msg.id);
                }}
                data-testid="connection-marker"
                className="flex items-center gap-3 py-2 select-none"
              >
                <div className="flex-1 h-px bg-border" />
                <span className="flex-shrink-0 text-xs font-medium uppercase tracking-wide text-muted-foreground/80">
                  {msg.message}
                  <span className="ml-2 font-mono normal-case tracking-normal text-muted-foreground/60">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                </span>
                <div className="flex-1 h-px bg-border" />
              </div>
            );
          }

          return (
            <div
              key={msg.id}
              ref={(el) => {
                if (el) messageRefs.current.set(msg.id, el);
                else messageRefs.current.delete(msg.id);
              }}
              data-testid="message-item"
              className={`group flex space-x-3 py-1 px-2 rounded transition-colors ${
                hasMention
                  ? 'cc-mention border-l-2'
                  : isError
                  ? 'bg-destructive/10 border-l-2 border-destructive shadow-[var(--shadow-sm)]'
                  : isStatus || isCommand
                  ? 'opacity-70'
                  : isEven
                  ? 'bg-muted/20'
                  : ''
              } hover:bg-muted/30`}
            >
              {isError ? (
                <>
                  <span className="text-sm text-destructive font-semibold flex-shrink-0">⚠</span>
                  <span className="hidden sm:inline text-xs text-muted-foreground/70 flex-shrink-0 font-mono">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                  <span className="text-sm text-destructive flex-1 font-medium">
                    {msg.message.replace(/^Error: /, '')}
                  </span>
                </>
              ) : (
                <>
                  <span className="hidden sm:inline text-xs text-muted-foreground/60 flex-shrink-0 font-mono">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                  {msg.user !== '*' && !isSystemMessage && (
                    <span 
                      className={`text-sm font-medium ${
                        isStatus || isCommand ? 'text-muted-foreground italic' : 'text-primary'
                      }`}
                      style={{ 
                        color: (isStatus || isCommand) ? undefined : (nicknameColors.get(msg.user) || undefined)
                      }}
                    >
                      {msg.user}
                    </span>
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
                      className={`text-sm flex-1 ${
                        isStatus || isCommand ? 'text-muted-foreground italic' : ''
                      }`}
                    />
                  )}
                </>
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
          );
        })
      )}
      <div ref={messagesEndRef} />
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
    </div>
  );
}

