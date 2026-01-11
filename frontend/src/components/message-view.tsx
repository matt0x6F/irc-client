import { useEffect, useRef, useState, useMemo } from 'react';
import { storage } from '../../wailsjs/go/models';
import { IRCFormattedText } from './irc-formatted-text';
import { useNicknameColors } from '../hooks/useNicknameColors';

interface MessageViewProps {
  messages: storage.Message[];
  networkId: number | null;
}

export function MessageView({ messages, networkId }: MessageViewProps) {
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const prevMessagesLengthRef = useRef(0);

  // Extract unique nicknames from messages (including join/part/quit for colored display)
  const uniqueNicknames = useMemo(() => {
    const nicks = new Set<string>();
    messages.forEach(msg => {
      if (msg.user && msg.user !== '*') {
        nicks.add(msg.user);
      }
    });
    return Array.from(nicks);
  }, [messages]);

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

  return (
    <div 
      ref={scrollContainerRef}
      className="h-full overflow-y-auto p-4 space-y-2"
      onScroll={checkIfNearBottom}
    >
      {messages.length === 0 ? (
        <div className="text-center text-muted-foreground py-8">
          No messages yet. Start chatting!
        </div>
      ) : (
        messages.map((msg) => {
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
                      {msg.user !== '*' && nicknameColors.get(msg.user) ? (
                        // For system messages, color the nickname within the message text
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

