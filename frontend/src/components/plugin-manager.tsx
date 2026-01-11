import { main } from '../../wailsjs/go/models';

interface PluginManagerProps {
  plugins: main.PluginInfo[];
  onClose: () => void;
}

export function PluginManager({ plugins, onClose }: PluginManagerProps) {
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
                    <span className={`px-2 py-1 text-xs rounded ${
                      plugin.enabled ? 'bg-green-500/20 text-green-500' : 'bg-gray-500/20 text-gray-500'
                    }`}>
                      {plugin.enabled ? 'Enabled' : 'Disabled'}
                    </span>
                  </div>
                  {plugin.description && (
                    <p className="text-sm text-muted-foreground mb-2">{plugin.description}</p>
                  )}
                  <div className="text-xs text-muted-foreground">
                    <div>Version: {plugin.version}</div>
                    {plugin.author && <div>Author: {plugin.author}</div>}
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

