import { useState } from 'react';

interface InputAreaProps {
  onSendMessage: (message: string) => Promise<void>;
  placeholder?: string;
}

export function InputArea({ onSendMessage, placeholder = 'Type a message...' }: InputAreaProps) {
  const [message, setMessage] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (message.trim()) {
      await onSendMessage(message.trim());
      setMessage('');
    }
  };

  return (
    <div className="border-t border-border p-4">
      <form onSubmit={handleSubmit} className="flex space-x-2">
        <input
          type="text"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          placeholder={placeholder}
          className="flex-1 px-4 py-2 border border-border rounded focus:outline-none focus:ring-2 focus:ring-primary"
        />
        <button
          type="submit"
          className="px-6 py-2 bg-primary text-primary-foreground rounded hover:bg-primary/90"
        >
          Send
        </button>
      </form>
    </div>
  );
}

