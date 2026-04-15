import { useState, useEffect, useRef, useCallback } from 'react';
import { SearchMessages } from '../../wailsjs/go/main/App';
import { useNetworkStore } from '../stores/network';

interface SearchModalProps {
  onClose: () => void;
}

interface SearchResult {
  id: number;
  network_id: number;
  channel_id?: number;
  user: string;
  message: string;
  message_type: string;
  timestamp: string;
  raw_line: string;
  channel_name: string;
  network_name: string;
}

export function SearchModal({ onClose }: SearchModalProps) {
  const [query, setQuery] = useState('');
  const [results, setResults] = useState<SearchResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasSearched, setHasSearched] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const debounceRef = useRef<number | null>(null);

  const networks = useNetworkStore((s) => s.networks);
  const selectPane = useNetworkStore((s) => s.selectPane);

  // Focus input on mount
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  // Keyboard handler for Escape
  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        onClose();
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [onClose]);

  const performSearch = useCallback(async (searchQuery: string) => {
    if (searchQuery.trim().length === 0) {
      setResults([]);
      setHasSearched(false);
      setError(null);
      return;
    }

    setLoading(true);
    setError(null);
    setHasSearched(true);

    try {
      const searchResults = await SearchMessages(searchQuery, null, 50);
      setResults(searchResults || []);
    } catch (err: any) {
      console.error('Search failed:', err);
      setError(err?.message || 'Search failed');
      setResults([]);
    } finally {
      setLoading(false);
    }
  }, []);

  // Debounced search
  const handleQueryChange = (value: string) => {
    setQuery(value);

    if (debounceRef.current !== null) {
      clearTimeout(debounceRef.current);
    }

    debounceRef.current = setTimeout(() => {
      performSearch(value);
    }, 300) as unknown as number;
  };

  // Cleanup debounce on unmount
  useEffect(() => {
    return () => {
      if (debounceRef.current !== null) {
        clearTimeout(debounceRef.current);
      }
    };
  }, []);

  const handleResultClick = async (result: SearchResult) => {
    // Navigate to the channel/PM where the message was sent
    if (result.channel_name) {
      await selectPane(result.network_id, result.channel_name);
    } else {
      // Private message -- navigate to PM with user
      await selectPane(result.network_id, `pm:${result.user}`);
    }
    onClose();
  };

  const formatTimestamp = (ts: string) => {
    try {
      const date = new Date(ts);
      const now = new Date();
      const isToday =
        date.getDate() === now.getDate() &&
        date.getMonth() === now.getMonth() &&
        date.getFullYear() === now.getFullYear();

      if (isToday) {
        return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
      }

      return date.toLocaleDateString([], {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
      });
    } catch {
      return '';
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-start justify-center z-50 pt-[10vh]" onClick={onClose}>
      <div
        className="border border-border rounded-lg w-full max-w-2xl flex flex-col max-h-[70vh]"
        style={{ backgroundColor: 'var(--background)' }}
        onClick={(e) => e.stopPropagation()}
      >
        {/* Search Input */}
        <div className="p-4 border-b border-border flex items-center gap-3">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            className="text-muted-foreground flex-shrink-0"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            ref={inputRef}
            type="text"
            value={query}
            onChange={(e) => handleQueryChange(e.target.value)}
            placeholder="Search messages..."
            className="flex-1 bg-transparent border-none outline-none text-foreground placeholder:text-muted-foreground text-base"
          />
          {loading && (
            <span className="text-xs text-muted-foreground animate-pulse">Searching...</span>
          )}
          <kbd className="hidden sm:inline-flex h-5 select-none items-center gap-1 rounded border border-border bg-muted px-1.5 font-mono text-[10px] font-medium text-muted-foreground opacity-60">
            ESC
          </kbd>
        </div>

        {/* Results */}
        <div className="overflow-y-auto flex-1">
          {error && (
            <div className="p-4 text-sm text-destructive">
              {error}
            </div>
          )}

          {!hasSearched && !error && (
            <div className="p-8 text-center text-muted-foreground text-sm">
              Type to search across all messages
            </div>
          )}

          {hasSearched && results.length === 0 && !loading && !error && (
            <div className="p-8 text-center text-muted-foreground text-sm">
              No messages found
            </div>
          )}

          {results.map((result) => (
            <button
              key={result.id}
              onClick={() => handleResultClick(result)}
              className="w-full text-left px-4 py-3 hover:bg-accent/50 border-b border-border/50 last:border-b-0 transition-colors cursor-pointer"
            >
              <div className="flex items-center gap-2 mb-1">
                <span className="font-medium text-sm text-foreground">{result.user}</span>
                <span className="text-xs text-muted-foreground">
                  {result.channel_name
                    ? `${result.network_name} / ${result.channel_name}`
                    : `${result.network_name} / PM`}
                </span>
                <span className="text-xs text-muted-foreground ml-auto flex-shrink-0">
                  {formatTimestamp(result.timestamp)}
                </span>
              </div>
              <div className="text-sm text-muted-foreground truncate">
                {result.message}
              </div>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
