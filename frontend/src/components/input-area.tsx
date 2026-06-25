import { useState, useRef, useEffect, useMemo } from 'react';
import { GetChannelInfo } from '../../wailsjs/go/main/App';
import { main, storage } from '../../wailsjs/go/models';
import { FormattingToolbar } from './formatting-toolbar';
import { MessagePreview } from './message-preview';
import { wrapSelection, applyToInput } from '../lib/input-insert';
import { useCommandsStore, filterCommands, lookupCommand } from '../stores/commands';
import { parseCommandLine } from '../lib/command-line';
import { useTypingStore, typingNicksFor, formatTypingLabel } from '../stores/typing';
import { useTypingSender } from '../hooks/useTypingSender';

interface InputAreaProps {
  onSendMessage: (message: string) => Promise<void>;
  placeholder?: string;
  networkId?: number | null;
  channelName?: string | null;
}

export function InputArea({ onSendMessage, placeholder = 'Type a message...', networkId, channelName }: InputAreaProps) {
  const [message, setMessage] = useState('');
  const [completionIndex, setCompletionIndex] = useState(-1);
  const [completions, setCompletions] = useState<string[]>([]);
  const [lastCompletionPrefix, setLastCompletionPrefix] = useState('');
  const inputRef = useRef<HTMLInputElement>(null);

  // Command history state (global, per-session)
  const historyRef = useRef<string[]>([]);
  const historyIndexRef = useRef(-1);
  const draftRef = useRef('');

  // Command autocomplete state
  const commands = useCommandsStore((s) => s.commands);
  const [cmdSelIndex, setCmdSelIndex] = useState(0);
  const [cmdPopupDismissed, setCmdPopupDismissed] = useState(false);

  // IRCv3 +typing: send our own typing state, and surface peers' typing as a line
  // above the input. `channelName` is already the normalized conversation key
  // ('#chan' or 'pm:<nick>'); the sender strips the pm: prefix for the wire.
  const typingSender = useTypingSender(networkId, channelName);
  const typingMap = useTypingStore((s) => s.typing);
  const typingLabel = useMemo(() => {
    if (networkId == null || !channelName) return null;
    return formatTypingLabel(typingNicksFor(typingMap, networkId, channelName));
  }, [typingMap, networkId, channelName]);

  const cmdLine = parseCommandLine(message);
  const cmdMatches =
    cmdLine.isCommand && !cmdLine.afterCommandName ? filterCommands(commands, cmdLine.word) : [];
  const showCmdPopup = cmdMatches.length > 0 && !cmdPopupDismissed;
  const usageHint =
    cmdLine.isCommand && cmdLine.afterCommandName ? lookupCommand(commands, cmdLine.word) : null;

  // Auto-focus the input whenever the user switches to a different buffer
  // (channel or PM). The same InputArea instance stays mounted across switches,
  // so keying this effect on networkId/channelName makes it fire on each change.
  // Don't steal focus from another editable field — e.g. a modal's search box
  // the user is actively typing in. Switching via a tree <button> leaves a button
  // focused, which should yield to the message input.
  useEffect(() => {
    const active = document.activeElement as HTMLElement | null;
    const typingElsewhere =
      active &&
      active !== inputRef.current &&
      (active.tagName === 'INPUT' ||
        active.tagName === 'TEXTAREA' ||
        active.isContentEditable);
    if (!typingElsewhere) {
      inputRef.current?.focus();
    }
  }, [networkId, channelName]);

  // Reset completion state when message changes (but not during completion)
  useEffect(() => {
    if (completionIndex === -1) {
      setCompletions([]);
      setLastCompletionPrefix('');
    }
  }, [message, completionIndex]);

  // Reset popup selection/dismissal as the typed command word changes.
  useEffect(() => {
    setCmdSelIndex(0);
    setCmdPopupDismissed(false);
  }, [cmdLine.word, cmdLine.afterCommandName]);

  const acceptCommand = (info: main.CommandInfo) => {
    setMessage(`/${info.name.toLowerCase()} `);
    setCmdPopupDismissed(true);
    setTimeout(() => inputRef.current?.focus(), 0);
  };

  const handleCommandKeys = (e: React.KeyboardEvent<HTMLInputElement>): boolean => {
    if (!showCmdPopup) return false;
    if (e.key === 'ArrowDown') {
      e.preventDefault();
      setCmdSelIndex((i) => (i + 1) % cmdMatches.length);
      return true;
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault();
      setCmdSelIndex((i) => (i - 1 + cmdMatches.length) % cmdMatches.length);
      return true;
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault();
      acceptCommand(cmdMatches[cmdSelIndex]);
      return true;
    }
    if (e.key === 'Escape') {
      e.preventDefault();
      setCmdPopupDismissed(true);
      return true;
    }
    return false;
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (message.trim()) {
      const trimmed = message.trim();
      await onSendMessage(trimmed);

      // Add to history if not a duplicate of the last entry
      const history = historyRef.current;
      if (history.length === 0 || history[history.length - 1] !== trimmed) {
        history.push(trimmed);
        if (history.length > 100) {
          history.shift();
        }
      }
      historyIndexRef.current = -1;
      draftRef.current = '';

      typingSender.onSubmit();
      setMessage('');
      setCompletionIndex(-1);
      setCompletions([]);
      setLastCompletionPrefix('');
    }
  };

  const handleHistoryNavigation = (e: React.KeyboardEvent<HTMLInputElement>) => {
    const history = historyRef.current;
    if (history.length === 0) return;

    if (e.key === 'ArrowUp') {
      const input = e.currentTarget;
      const cursorPos = input.selectionStart || 0;
      // Only navigate history when cursor is at position 0 or input is empty
      if (cursorPos !== 0 && message.length > 0) return;

      e.preventDefault();

      if (historyIndexRef.current === -1) {
        // Starting history navigation — save current draft
        draftRef.current = message;
        historyIndexRef.current = history.length - 1;
      } else if (historyIndexRef.current > 0) {
        historyIndexRef.current--;
      } else {
        return; // Already at oldest entry
      }

      setMessage(history[historyIndexRef.current]);
    } else if (e.key === 'ArrowDown') {
      if (historyIndexRef.current === -1) return; // Not in history mode

      e.preventDefault();

      if (historyIndexRef.current < history.length - 1) {
        historyIndexRef.current++;
        setMessage(history[historyIndexRef.current]);
      } else {
        // Past newest entry — restore draft
        historyIndexRef.current = -1;
        setMessage(draftRef.current);
      }
    }
  };

  const performTabCompletion = async (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key !== 'Tab' || !networkId || !channelName || channelName === 'status') {
      return;
    }

    e.preventDefault();
    e.stopPropagation();

    const input = e.currentTarget;
    const cursorPos = input.selectionStart || 0;
    const textBeforeCursor = message.substring(0, cursorPos);
    
    // Find the word at the cursor position
    // Match @nickname or just nickname at the start of the message or after whitespace
    const wordMatch = textBeforeCursor.match(/(?:^|\s)(@?)(\w*)$/);
    if (!wordMatch) {
      return;
    }

    const trigger = wordMatch[1]; // '@' or ''
    const currentWord = wordMatch[2]; // current word (could be partial or complete)
    const partial = currentWord.toLowerCase();

    // Get channel users
    try {
      const channelInfo = await GetChannelInfo(networkId, channelName);
      if (!channelInfo || !channelInfo.users) {
        return;
      }

      const users = (channelInfo.users || []) as storage.ChannelUser[];
      const userNicknames = users.map(u => u.nickname);

      // Check if we're continuing a previous completion cycle
      // This happens if we have completions and the current word matches one of them
      const isContinuingCycle = completions.length > 0 && 
        completions.some(nick => nick.toLowerCase() === currentWord.toLowerCase());
      
      if (isContinuingCycle && completionIndex >= 0) {
        // Cycle to next completion
        const nextIndex = (completionIndex + 1) % completions.length;
        setCompletionIndex(nextIndex);
        const selectedNick = completions[nextIndex];
        
        // Find the start of the current word (including trigger)
        const wordStart = cursorPos - (trigger.length + currentWord.length);
        const textAfterCursor = message.substring(cursorPos);
        
        // Remove any trailing separator that was added
        const separatorMatch = textAfterCursor.match(/^[: ]/);
        const separator = separatorMatch ? separatorMatch[0] : (channelInfo.channel?.name ? ':' : ' ');
        
        const newMessage = 
          message.substring(0, wordStart) + 
          trigger + selectedNick + separator + 
          (separatorMatch ? textAfterCursor.substring(1) : textAfterCursor);
        
        setMessage(newMessage);
        
        // Set cursor position after the completed nickname and separator
        setTimeout(() => {
          if (inputRef.current) {
            const newPos = wordStart + trigger.length + selectedNick.length + separator.length;
            inputRef.current.setSelectionRange(newPos, newPos);
          }
        }, 0);
      } else {
        // Start new completion cycle - filter matching nicknames
        const matching = userNicknames.filter(nick => 
          nick.toLowerCase().startsWith(partial)
        );

        if (matching.length === 0) {
          return; // No matches
        }

        // Sort matches (case-insensitive)
        matching.sort((a, b) => a.toLowerCase().localeCompare(b.toLowerCase()));

        // Store completions and start cycle
        setCompletions(matching);
        setLastCompletionPrefix(trigger + partial);
        setCompletionIndex(0);
        const selectedNick = matching[0];
        
        // Replace the word with the first matching nickname
        const wordStart = cursorPos - (trigger.length + currentWord.length);
        const textAfterCursor = message.substring(cursorPos);
        const separator = channelInfo.channel?.name ? ':' : ' ';
        
        const newMessage = 
          message.substring(0, wordStart) + 
          trigger + selectedNick + separator + 
          textAfterCursor;
        
        setMessage(newMessage);
        
        // Set cursor position after the completed nickname and separator
        setTimeout(() => {
          if (inputRef.current) {
            const newPos = wordStart + trigger.length + selectedNick.length + separator.length;
            inputRef.current.setSelectionRange(newPos, newPos);
          }
        }, 0);
      }
    } catch (error) {
      console.error('Failed to get channel info for tab completion:', error);
    }
  };

  // Cmd/Ctrl+B / I / U wrap the selection in the matching markup, mirroring the
  // toolbar buttons (uses the same pure helpers).
  const handleFormattingShortcut = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (!(e.metaKey || e.ctrlKey) || e.altKey) return;
    const delims: Record<string, [string, string]> = {
      b: ['*', '*'],
      i: ['_', '_'],
      u: ['__', '__'],
    };
    const pair = delims[e.key.toLowerCase()];
    if (!pair) return;
    e.preventDefault();
    const input = e.currentTarget;
    const start = input.selectionStart ?? message.length;
    const end = input.selectionEnd ?? message.length;
    applyToInput(input, wrapSelection(message, start, end, pair[0], pair[1]), setMessage);
  };

  return (
    <div
      className="border-t border-border p-4"
      style={{ background: 'var(--glass-bg)', backdropFilter: 'blur(var(--backdrop-blur))', WebkitBackdropFilter: 'blur(var(--backdrop-blur))' }}
    >
      {showCmdPopup && (
        <div className="mb-2 max-h-48 overflow-y-auto rounded-md border border-border bg-background shadow-[var(--shadow-md)]" role="listbox">
          {cmdMatches.map((c, i) => (
            <div
              key={c.name}
              role="option"
              aria-selected={i === cmdSelIndex}
              className={`flex items-baseline gap-2 px-3 py-1.5 cursor-pointer text-sm ${i === cmdSelIndex ? 'bg-accent' : ''}`}
              onMouseDown={(e) => { e.preventDefault(); acceptCommand(c); }}
            >
              <span className="font-medium"><span className="opacity-50">/</span><span>{c.name.toLowerCase()}</span></span>
              <span className="text-muted-foreground text-xs">{c.usage}</span>
              <span className="ml-auto text-muted-foreground/70 text-xs">{c.category}</span>
            </div>
          ))}
        </div>
      )}
      {usageHint && !showCmdPopup && (
        <div className="mb-2 px-1 text-xs text-muted-foreground">
          <span className="font-medium">/{usageHint.name.toLowerCase()}</span> {usageHint.usage}
          {usageHint.description ? <span className="text-muted-foreground/70"> — {usageHint.description}</span> : null}
        </div>
      )}
      <MessagePreview message={message} />
      <FormattingToolbar
        inputRef={inputRef}
        value={message}
        setValue={setMessage}
        networkId={networkId}
        channelName={channelName}
      />
      {typingLabel && (
        <div
          className="mb-1 px-2 text-xs italic text-muted-foreground"
          aria-live="polite"
          data-testid="typing-indicator"
        >
          {typingLabel}
        </div>
      )}
      <form onSubmit={handleSubmit} className="flex space-x-3">
        <input
          ref={inputRef}
          type="text"
          value={message}
          onChange={(e) => { setMessage(e.target.value); typingSender.onChange(e.target.value); }}
          onKeyDown={(e) => { if (handleCommandKeys(e)) return; handleFormattingShortcut(e); handleHistoryNavigation(e); performTabCompletion(e); }}
          placeholder={placeholder}
          className="flex-1 px-4 py-2.5 border border-border rounded-full bg-background text-foreground placeholder:text-muted-foreground/60 focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
          style={{ transition: 'var(--transition-base)' }}
          data-testid="message-input"
        />
        <button
          type="submit"
          className="px-6 py-2.5 bg-primary text-primary-foreground rounded-full hover:bg-primary/90 active:scale-[0.98] font-medium shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)] transition-all disabled:opacity-50 disabled:cursor-not-allowed"
          style={{ transition: 'var(--transition-base)' }}
          disabled={!message.trim()}
        >
          Send
        </button>
      </form>
    </div>
  );
}

