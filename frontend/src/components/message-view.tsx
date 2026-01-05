import { useEffect, useRef, useState } from 'react';
import { storage } from '../../wailsjs/go/models';
import { IRCFormattedText } from './irc-formatted-text';

interface MessageViewProps {
  messages: storage.Message[];
}

export function MessageView({ messages }: MessageViewProps) {
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const scrollContainerRef = useRef<HTMLDivElement>(null);
  const [isNearBottom, setIsNearBottom] = useState(true);
  const prevMessagesLengthRef = useRef(0);

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
                  {msg.user !== '*' && (
                    <span className={`text-sm font-medium ${
                      isStatus || isCommand ? 'text-muted-foreground italic' : 'text-primary'
                    }`}>
                      {msg.user}
                    </span>
                  )}
                  <span className="text-sm text-muted-foreground">
                    {new Date(msg.timestamp).toLocaleTimeString()}
                  </span>
                  <IRCFormattedText 
                    text={msg.message} 
                    className={`text-sm flex-1 ${
                      isStatus || isCommand ? 'text-muted-foreground italic' : ''
                    }`} 
                  />
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

