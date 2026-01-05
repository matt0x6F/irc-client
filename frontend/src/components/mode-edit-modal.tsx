import { useState, useEffect } from 'react';
import { SendCommand } from '../../wailsjs/go/main/App';

interface ModeEditModalProps {
  networkId: number;
  channelName: string;
  currentModes: string;
  onClose: () => void;
  onUpdate: () => void;
}

export function ModeEditModal({ networkId, channelName, currentModes, onClose, onUpdate }: ModeEditModalProps) {
  const [modes, setModes] = useState(currentModes);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    setModes(currentModes);
  }, [currentModes]);

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    setSaving(true);
    try {
      // Send MODE command: /MODE #channel +modes
      // Note: This is simplified - full mode editing would need to parse current modes
      // and apply changes incrementally. For now, we'll just send the new modes.
      const command = `/MODE ${channelName} ${modes}`;
      await SendCommand(networkId, command);
      onUpdate();
      onClose();
    } catch (error) {
      console.error('Failed to set modes:', error);
      alert(`Failed to set modes: ${error}`);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={onClose}>
      <div 
        className="border border-border rounded-lg w-full max-w-md p-6"
        style={{ backgroundColor: 'var(--background)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <h2 className="text-lg font-semibold mb-4">Edit Channel Modes</h2>
        <form onSubmit={handleSave} className="space-y-4">
          <div>
            <label className="block text-sm font-medium mb-2">Modes</label>
            <input
              type="text"
              value={modes}
              onChange={(e) => setModes(e.target.value)}
              className="w-full px-3 py-2 text-sm border border-border rounded"
              placeholder="e.g., +Cnst"
              autoFocus
            />
            <p className="text-xs text-muted-foreground mt-2">
              Enter channel modes (e.g., +Cnst). Use + to add modes, - to remove modes.
            </p>
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

