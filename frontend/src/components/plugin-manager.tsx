import { useState } from 'react';
import { main } from '../../wailsjs/go/models';
import { ListPlugins, EnablePlugin, DisablePlugin } from '../../wailsjs/go/main/App';

interface PluginManagerProps {
  plugins: main.PluginInfo[];
  onClose: () => void;
}

export function PluginManager({ plugins: initialPlugins, onClose }: PluginManagerProps) {
  const [plugins, setPlugins] = useState<main.PluginInfo[]>(initialPlugins);
  const [pluginLoading, setPluginLoading] = useState<Set<string>>(new Set());

  const loadPlugins = async () => {
    try {
      const pluginList = await ListPlugins();
      setPlugins(pluginList || []);
    } catch (error) {
      console.error('Failed to load plugins:', error);
    }
  };

  const handleTogglePlugin = async (pluginName: string, currentlyEnabled: boolean) => {
    setPluginLoading(prev => new Set(prev).add(pluginName));
    try {
      if (currentlyEnabled) {
        await DisablePlugin(pluginName);
      } else {
        await EnablePlugin(pluginName);
      }
      // Reload plugins to get updated state
      await loadPlugins();
    } catch (error) {
      console.error(`Failed to ${currentlyEnabled ? 'disable' : 'enable'} plugin:`, error);
      alert(`Failed to ${currentlyEnabled ? 'disable' : 'enable'} plugin: ${error}`);
    } finally {
      setPluginLoading(prev => {
        const next = new Set(prev);
        next.delete(pluginName);
        return next;
      });
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
      <div className="bg-background border border-border rounded-lg w-full max-w-2xl max-h-[80vh] flex flex-col">
        <div className="p-4 border-b border-border flex items-center justify-between">
          <h2 className="text-lg font-semibold">Plugin Manager</h2>
          <button
            onClick={onClose}
            className="px-3 py-1 text-sm border border-border rounded hover:bg-accent"
          >
            Close
          </button>
        </div>
        <div className="flex-1 overflow-y-auto p-4">
          {plugins.length === 0 ? (
            <div className="text-center text-muted-foreground py-8">
              No plugins installed
            </div>
          ) : (
            <div className="space-y-4">
              {plugins.map((plugin) => (
                <div key={plugin.name} className="border border-border rounded p-4">
                  <div className="flex items-center justify-between mb-2">
                    <h3 className="font-semibold">{plugin.name}</h3>
                    <div className="flex items-center gap-2">
                      <button
                        onClick={() => handleTogglePlugin(plugin.name, plugin.enabled)}
                        disabled={pluginLoading.has(plugin.name)}
                        className={`px-3 py-1 text-xs rounded border transition-colors ${
                          plugin.enabled
                            ? 'bg-green-500/20 text-green-500 border-green-500/30 hover:bg-green-500/30'
                            : 'bg-gray-500/20 text-gray-500 border-gray-500/30 hover:bg-gray-500/30'
                        } disabled:opacity-50 disabled:cursor-not-allowed`}
                      >
                        {pluginLoading.has(plugin.name) ? '...' : (plugin.enabled ? 'Disable' : 'Enable')}
                      </button>
                      <span className={`px-2 py-1 text-xs rounded ${
                        plugin.enabled ? 'bg-green-500/20 text-green-500' : 'bg-gray-500/20 text-gray-500'
                      }`}>
                        {plugin.enabled ? 'Enabled' : 'Disabled'}
                      </span>
                    </div>
                  </div>
                  {plugin.description && (
                    <p className="text-sm text-muted-foreground mb-2">{plugin.description}</p>
                  )}
                  <div className="text-xs text-muted-foreground">
                    <div>Version: {plugin.version}</div>
                    {plugin.author && <div>Author: {plugin.author}</div>}
                      {plugin.metadata_types && plugin.metadata_types.length > 0 && (
                        <div>Metadata Types: {plugin.metadata_types.join(', ')}</div>
                      )}
                    <div>Path: {plugin.path}</div>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

