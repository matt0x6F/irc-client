import { useState } from 'react';
import { storage } from '../../wailsjs/go/models';
import { PreferPrivateContactSession } from '../../wailsjs/go/main/App';
import { useNetworkStore } from '../stores/network';

interface PrivateContactHeaderProps {
  networkId: number;
  contact?: storage.PrivateMessageConversation;
  fallback: string;
}

export function PrivateContactHeader({ networkId, contact, fallback }: PrivateContactHeaderProps) {
  const [error, setError] = useState('');
  const name = contact?.target || contact?.target_user || (fallback.startsWith('@') ? '' : fallback);

  const preferSession = async (nick: string) => {
    if (!contact) return;
    setError('');
    try {
      await PreferPrivateContactSession(networkId, contact.reference, nick);
      await useNetworkStore.getState().loadPrivateContacts(networkId);
    } catch (error) {
      setError(error instanceof Error ? error.message : String(error));
    }
  };

  return (
    <>
      <span className="text-muted-foreground font-medium">{name ? `PM: ${name}` : 'Private message'}</span>
      {contact && contact.sessions.length > 1 && (
        <select
          aria-label="Send to session"
          value={contact.target || contact.target_user}
          className="max-w-40 rounded border border-border bg-background text-sm"
          onChange={(event) => void preferSession(event.target.value)}
        >
          {contact.sessions.map((nick) => <option key={nick} value={nick}>{nick}</option>)}
        </select>
      )}
      {contact?.account && contact.presence !== 'online' && (
        <span className="text-xs text-muted-foreground">Current nickname unknown</span>
      )}
      {error && <span role="alert" className="text-sm text-destructive">{error}</span>}
    </>
  );
}
