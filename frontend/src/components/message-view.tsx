import { useEffect, useRef, useState, useMemo } from 'react';
import { storage } from '../../wailsjs/go/models';
import { IRCFormattedText } from './irc-formatted-text';
import { useNicknameColors } from '../hooks/useNicknameColors';

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

export function MessageView({ messages, networkId, selectedChannel }: MessageViewProps) {
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const prevMessagesLengthRef = useRef(0);
  const prevChannelRef = useRef<string | null | undefined>(selectedChannel);

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
        const nicknames = currentGroup.map(msg => msg.user).filter(user => user && user !== '*');
        
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
    const isNear = container.scrollHeight - container.scrollTop - container.clientHeight < threshold;
    setIsNearBottom(isNear);
  };

  // Handle scroll events
  useEffect(() => {
    const container = scrollContainerRef.current;
    if (!container) return;

    container.addEventListener('scroll', checkIfNearBottom);
    return () => container.removeEventListener('scroll', checkIfNearBottom);
  }, []);

  // Auto-scroll when channel changes - always scroll to bottom when switching channels
  useEffect(() => {
    if (selectedChannel !== undefined && selectedChannel !== prevChannelRef.current) {
      prevChannelRef.current = selectedChannel;
      // When channel changes, always scroll to bottom
      // Use a small delay to ensure DOM is updated with new messages
      setTimeout(() => {
        if (messagesEndRef.current) {
          messagesEndRef.current.scrollIntoView({ behavior: 'smooth' });
        } else if (scrollContainerRef.current) {
          // Fallback: scroll container to bottom
          scrollContainerRef.current.scrollTop = scrollContainerRef.current.scrollHeight;
        }
      }, 100);
      // Reset near-bottom state since we're scrolling to bottom
      setIsNearBottom(true);
    }
  }, [selectedChannel, messages]);

  // Auto-scroll only if user is near bottom and there are new messages
  useEffect(() => {
    const hasNewMessages = messages.length > prevMessagesLengthRef.current;
    prevMessagesLengthRef.current = messages.length;

    if (isNearBottom && hasNewMessages) {
      // Small delay to ensure DOM is updated
      setTimeout(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
      }, 50);
    }
  }, [messages, isNearBottom]);

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

  return (
    <div 
      ref={scrollContainerRef}
      className="h-full overflow-y-auto p-4 space-y-2"
      onScroll={checkIfNearBottom}
    >
      {processedMessages.length === 0 ? (
        <div className="text-center text-muted-foreground py-8">
          No messages yet. Start chatting!
        </div>
      ) : (
        processedMessages.map((msg) => {
          const consolidated = msg as ConsolidatedMessage;
          const isConsolidated = consolidated._consolidated === true;
          const isError = msg.message_type === 'error';
          const isStatus = msg.message_type === 'status';
          const isCommand = msg.message_type === 'command';
          const isSystemMessage = msg.message_type === 'join' || msg.message_type === 'part' || msg.message_type === 'quit';
          
          return (
            <div 
              key={msg.id} 
              className={`flex space-x-2 ${
                isError 
                  ? 'p-2 rounded bg-destructive/10 border-l-2 border-destructive' 
                  : isStatus || isCommand
                  ? 'opacity-75'
                  : ''
              }`}
            >
              {isError ? (
                <>
                  <span className="text-sm text-destructive font-semibold">⚠</span>
                  <span className="text-sm text-muted-foreground">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                  <span className="text-sm text-destructive flex-1 font-medium">
                    {msg.message.replace(/^Error: /, '')}
                  </span>
                </>
              ) : (
                <>
                  <span className="text-sm text-muted-foreground">
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
            </div>
          );
        })
      )}
      <div ref={messagesEndRef} />
    </div>
  );
}

