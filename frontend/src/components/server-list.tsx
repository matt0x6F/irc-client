import { useState } from 'react';
import { main, storage } from '../../wailsjs/go/models';

interface ServerListProps {
  servers: storage.Network[];
  selectedServer: number | null;
  onSelectServer: (id: number | null) => void;
  onConnect: (config: main.NetworkConfig) => Promise<void>;
  onDisconnect: (id: number) => Promise<void>;
  onDelete: (id: number) => Promise<void>;
  connectionStatus: Record<number, boolean>;
}

export function ServerList({ servers, selectedServer, onSelectServer, onConnect, onDisconnect, onDelete, connectionStatus }: ServerListProps) {
  const [showAddForm, setShowAddForm] = useState(false);
  const [formData, setFormData] = useState<main.NetworkConfig>(main.NetworkConfig.createFrom({
    name: '',
    address: '',
    port: 6667,
    tls: false,
    servers: [],
    nickname: '',
    username: '',
    realname: '',
    password: '',
  }));

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    await onConnect(formData);
    setShowAddForm(false);
    setFormData(main.NetworkConfig.createFrom({
      name: '',
      address: '',
      port: 6667,
      tls: false,
      servers: [],
      nickname: '',
      username: '',
      realname: '',
      password: '',
    }));
  };

  return (
    <div className="h-full flex flex-col">
      <div className="p-4 border-b border-border">
        <h2 className="font-semibold mb-2">Networks</h2>
        <button
          onClick={() => setShowAddForm(!showAddForm)}
          className="w-full px-3 py-2 text-sm border border-border rounded hover:bg-accent"
        >
          {showAddForm ? 'Cancel' : '+ Add Network'}
        </button>
      </div>

      {showAddForm && (
        <div className="p-4 border-b border-border bg-muted/50">
          <form onSubmit={handleSubmit} className="space-y-2">
            <input
              type="text"
              placeholder="Name"
              value={formData.name || ''}
              onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, name: e.target.value }))}
              className="w-full px-2 py-1 text-sm border border-border rounded"
              required
            />
            <input
              type="text"
              placeholder="Address"
              value={formData.address}
              onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, address: e.target.value }))}
              className="w-full px-2 py-1 text-sm border border-border rounded"
              required
            />
            <input
              type="number"
              placeholder="Port"
              value={formData.port}
              onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, port: parseInt(e.target.value) || 6667 }))}
              className="w-full px-2 py-1 text-sm border border-border rounded"
              required
            />
            <input
              type="text"
              placeholder="Nickname"
              value={formData.nickname}
              onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, nickname: e.target.value }))}
              className="w-full px-2 py-1 text-sm border border-border rounded"
              required
            />
            <label className="flex items-center space-x-2">
              <input
                type="checkbox"
                checked={formData.tls}
                onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, tls: e.target.checked }))}
              />
              <span className="text-sm">Use TLS</span>
            </label>
            <button
              type="submit"
              className="w-full px-3 py-2 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
            >
              Connect
            </button>
          </form>
        </div>
      )}

      <div className="flex-1 overflow-y-auto">
        {servers && servers.length > 0 ? servers.map((network) => {
          const isConnected = connectionStatus[network.id] || false;
          return (
            <div
              key={network.id}
              className={`group ${
                selectedServer === network.id ? 'bg-accent border-l-2 border-primary' : ''
              }`}
            >
              <div
                onClick={() => onSelectServer(network.id)}
                className="p-3 cursor-pointer hover:bg-accent"
              >
                <div className="flex items-center justify-between">
                  <div className="flex-1">
                    <div className="font-medium flex items-center gap-2">
                      {network.name}
                      <span className={`w-2 h-2 rounded-full ${
                        isConnected ? 'bg-green-500' : 'bg-gray-400'
                      }`} title={isConnected ? 'Connected' : 'Disconnected'} />
                    </div>
                    <div className="text-sm text-muted-foreground">{network.address}</div>
                  </div>
                </div>
              </div>
              <div className="px-3 pb-2 flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                {isConnected ? (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      onDisconnect(network.id);
                    }}
                    className="px-2 py-1 text-xs border border-border rounded hover:bg-destructive hover:text-destructive-foreground"
                    title="Disconnect"
                  >
                    Disconnect
                  </button>
                ) : (
                  <button
                    onClick={(e) => {
                      e.stopPropagation();
                      // Reconnect by calling onConnect with network config
                      onConnect(main.NetworkConfig.createFrom({
                        name: network.name,
                        address: network.address,
                        port: network.port,
                        tls: network.tls,
                        servers: [],
                        nickname: network.nickname,
                        username: network.username,
                        realname: network.realname,
                        password: network.password,
                      }));
                    }}
                    className="px-2 py-1 text-xs border border-border rounded hover:bg-primary hover:text-primary-foreground"
                    title="Connect"
                  >
                    Connect
                  </button>
                )}
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(network.id);
                  }}
                  className="px-2 py-1 text-xs border border-border rounded hover:bg-destructive hover:text-destructive-foreground"
                  title="Delete"
                >
                  Delete
                </button>
              </div>
            </div>
          );
        }) : (
          <div className="p-4 text-center text-muted-foreground text-sm">
            No networks configured
          </div>
        )}
      </div>
    </div>
  );
}

