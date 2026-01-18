import { useState, useEffect, useRef } from 'react';
import { SendCommand, GetNetworks } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { storage } from '../../wailsjs/go/models';

interface TopicEditModalProps {
  networkId: number;
  channelName: string;
  currentTopic: string;
  onClose: () => void;
  onUpdate: () => void;
}

export function TopicEditModal({ networkId, channelName, currentTopic, onClose, onUpdate }: TopicEditModalProps) {
  const [topic, setTopic] = useState(currentTopic);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const networksRef = useRef<storage.Network[]>([]);

  useEffect(() => {
    setTopic(currentTopic);
    // Load networks to get network address for error event matching
    GetNetworks().then(nets => {
      networksRef.current = nets || [];
    });
  }, [currentTopic]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setSaving(true);
    
    let resolved = false;
    let timeoutId: number;
    
    // Set up one-time listeners for error and topic change
    const cleanup = () => {
      if (timeoutId) clearTimeout(timeoutId);
      errorUnsubscribe();
      topicUnsubscribe();
    };
    
    const errorUnsubscribe = EventsOn('message-event', (data: any) => {
      if (resolved) return;
      const eventType = data?.type;
      if (eventType === 'error') {
        const eventData = data?.data || {};
        const network = eventData.network;
        const channel = eventData.channel;
        const currentNetwork = networksRef.current.find(n => n.id === networkId);
        // Match by network and channel (if channel is specified in error)
        if (currentNetwork && network === currentNetwork.address) {
          // If error has a channel, it must match; if no channel in error, it's a general error
          if (!channel || channel === channelName) {
            resolved = true;
            const errorMsg = eventData.error || 'An error occurred';
            setError(errorMsg);
            setSaving(false);
            cleanup();
          }
        }
      }
    });
    
    const topicUnsubscribe = EventsOn('message-event', (data: any) => {
      if (resolved) return;
      const eventType = data?.type;
      if (eventType === 'channel.topic') {
        const eventData = data?.data || {};
        const network = eventData.network;
        const channel = eventData.channel;
        const currentNetwork = networksRef.current.find(n => n.id === networkId);
        if (currentNetwork && network === currentNetwork.address && channel === channelName) {
          resolved = true;
          // Topic changed successfully
          onUpdate();
          onClose();
          cleanup();
        }
      }
    });
    
    try {
      // Send TOPIC command: /TOPIC #channel new topic
      // (backend will add the colon when formatting the IRC command)
      const command = `/TOPIC ${channelName} ${topic}`;
      await SendCommand(networkId, command);
      
      // Wait up to 2 seconds for either error or topic change
      timeoutId = setTimeout(() => {
        if (!resolved) {
          // No response - close modal and let user see if it worked
          resolved = true;
          onUpdate();
          onClose();
          cleanup();
        }
      }, 2000);
      
    } catch (error) {
      console.error('Failed to set topic:', error);
      setError(`Failed to set topic: ${error}`);
      setSaving(false);
      cleanup();
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={onClose}>
      <div 
        className="border border-border rounded-lg w-full max-w-md p-6"
        style={{ backgroundColor: 'var(--background)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold mb-4">Edit Topic</h2>
        {error && (
          <div className="mb-4 p-3 bg-destructive/10 border border-destructive rounded text-sm text-destructive">
            {error}
          </div>
        )}
        <form onSubmit={handleSave} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Topic</label>
            <textarea
              value={topic}
              onChange={(e) => setTopic(e.target.value)}
              className="w-full px-3 py-2 text-sm border border-border rounded resize-none"
              rows={3}
              placeholder="Enter channel topic..."
              autoFocus
            />
          </div>
          <div className="flex gap-2 justify-end">
            <button
              type="button"
              onClick={onClose}
              className="px-4 py-2 text-sm border border-border rounded hover:bg-accent"
              disabled={saving}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="px-4 py-2 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
              disabled={saving}
            >
              {saving ? 'Saving...' : 'Save'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

