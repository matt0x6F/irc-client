import { useState, useEffect } from 'react';
import { main, storage } from '../../wailsjs/go/models';
import { GetNetworks, SaveNetwork, ConnectNetwork, DeleteNetwork, DisconnectNetwork, GetConnectionStatus, GetServers, ListPlugins } from '../../wailsjs/go/main/App';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';

type SettingsSection = 'networks' | 'plugins' | 'display';

const SETTINGS_LAST_PANE_KEY = 'cascade-chat-settings-last-pane';
const CONSOLIDATE_JOIN_QUIT_KEY = 'cascade-chat-consolidate-join-quit';

interface SettingsModalProps {
  onClose: () => void;
  onServerUpdate?: () => void;
}

export function SettingsModal({ onClose, onServerUpdate }: SettingsModalProps) {
  // Load last selected pane from localStorage, default to 'networks'
  const loadLastPane = (): SettingsSection => {
    try {
      const saved = localStorage.getItem(SETTINGS_LAST_PANE_KEY);
      if (saved === 'networks' || saved === 'plugins' || saved === 'display') {
        return saved as SettingsSection;
      }
    } catch (error) {
      console.error('Failed to load last pane preference:', error);
    }
    return 'networks';
  };

  // Load consolidate join/quit setting from localStorage
  const loadConsolidateSetting = (): boolean => {
    try {
      const saved = localStorage.getItem(CONSOLIDATE_JOIN_QUIT_KEY);
      return saved === 'true';
    } catch (error) {
      console.error('Failed to load consolidate setting:', error);
      return false;
    }
  };

  const [selectedSection, setSelectedSection] = useState<SettingsSection>(loadLastPane);
  const [networks, setNetworks] = useState<storage.Network[]>([]);
  const [plugins, setPlugins] = useState<main.PluginInfo[]>([]);
  const [editingNetwork, setEditingNetwork] = useState<storage.Network | null>(null);
  const [consolidateJoinQuit, setConsolidateJoinQuit] = useState<boolean>(loadConsolidateSetting);
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
  const [connectionStatus, setConnectionStatus] = useState<Record<number, boolean>>({});
  const [showAddForm, setShowAddForm] = useState(false);
  const [networkServers, setNetworkServers] = useState<Record<number, storage.Server[]>>({});

  useEffect(() => {
    loadNetworks();
    loadPlugins();
  }, []);

  // Save selected pane to localStorage whenever it changes
  useEffect(() => {
    try {
      localStorage.setItem(SETTINGS_LAST_PANE_KEY, selectedSection);
    } catch (error) {
      console.error('Failed to save last pane preference:', error);
    }
  }, [selectedSection]);

  // Save consolidate setting to localStorage whenever it changes
  useEffect(() => {
    try {
      localStorage.setItem(CONSOLIDATE_JOIN_QUIT_KEY, consolidateJoinQuit.toString());
    } catch (error) {
      console.error('Failed to save consolidate setting:', error);
    }
  }, [consolidateJoinQuit]);

  const loadPlugins = async () => {
    try {
      const pluginList = await ListPlugins();
      setPlugins(pluginList || []);
    } catch (error) {
      console.error('Failed to load plugins:', error);
      setPlugins([]);
    }
  };

  useEffect(() => {
    if (networks.length > 0) {
      loadConnectionStatus();
      // Load servers for all networks
      networks.forEach(network => {
        loadNetworkServers(network.id);
      });
    }
  }, [networks]);

  const loadNetworks = async () => {
    try {
      const networkList = await GetNetworks();
      setNetworks(networkList || []);
    } catch (error) {
      console.error('Failed to load networks:', error);
      setNetworks([]);
    }
  };

  const loadConnectionStatus = async () => {
    const status: Record<number, boolean> = {};
    for (const network of networks) {
      try {
        const connected = await GetConnectionStatus(network.id);
        status[network.id] = connected;
      } catch (error) {
        status[network.id] = false;
      }
    }
    setConnectionStatus(status);
  };

  const loadNetworkServers = async (networkId: number) => {
    try {
      const servers = await GetServers(networkId);
      setNetworkServers(prev => ({ ...prev, [networkId]: servers || [] }));
    } catch (error) {
      console.error('Failed to load network servers:', error);
      setNetworkServers(prev => ({ ...prev, [networkId]: [] }));
    }
  };

  const handleEdit = async (network: storage.Network) => {
    setEditingNetwork(network);
    // Load servers for this network
    const servers = await GetServers(network.id);
    setNetworkServers(prev => ({ ...prev, [network.id]: servers || [] }));
    setFormData(main.NetworkConfig.createFrom({
      name: network.name,
      address: network.address,
      port: network.port,
      tls: network.tls,
      servers: [],
      nickname: network.nickname,
      username: network.username,
      realname: network.realname,
      password: network.password,
      sasl_enabled: network.sasl_enabled || false,
      sasl_mechanism: network.sasl_mechanism || '',
      sasl_username: network.sasl_username || '',
      sasl_password: network.sasl_password || '',
      sasl_external_cert: network.sasl_external_cert || '',
      auto_connect: network.auto_connect || false,
    }));
    setShowAddForm(false);
  };

  const handleAdd = () => {
    setEditingNetwork(null);
    setFormData(main.NetworkConfig.createFrom({
      name: '',
      address: '',
      port: 6667,
      tls: false,
      servers: [{ address: '', port: 6667, tls: false, order: 0 }],
      nickname: '',
      username: '',
      realname: '',
      password: '',
    }));
    setShowAddForm(true);
  };

  const handleCancel = () => {
    setEditingNetwork(null);
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

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      // Get servers - from networkServers if editing, or from formData if new
      let servers: main.ServerConfig[] = [];
      
      if (editingNetwork && networkServers[editingNetwork.id]) {
        // Editing existing network - use servers from state
        servers = networkServers[editingNetwork.id]
          .filter(srv => srv.address.trim() !== '') // Filter out empty addresses
          .map((srv, idx) => ({
            address: srv.address,
            port: srv.port,
            tls: srv.tls,
            order: idx, // Reorder based on current position
          }));
      } else if (formData.servers && formData.servers.length > 0) {
        // New network - use servers from formData
        servers = formData.servers
          .filter(srv => srv.address && srv.address.trim() !== '')
          .map((srv, idx) => ({
            address: srv.address,
            port: srv.port,
            tls: srv.tls,
            order: idx,
          }));
      }
      
      // Validate at least one server
      if (servers.length === 0) {
        alert('Please add at least one server address');
        return;
      }
      
      const config = main.NetworkConfig.createFrom({
        name: formData.name,
        nickname: formData.nickname,
        username: formData.username,
        realname: formData.realname,
        password: formData.password,
        servers: servers,
        sasl_enabled: formData.sasl_enabled || false,
        sasl_mechanism: formData.sasl_mechanism || '',
        sasl_username: formData.sasl_username || '',
        sasl_password: formData.sasl_password || '',
        sasl_external_cert: formData.sasl_external_cert || '',
        auto_connect: (formData as any).auto_connect || false,
      });
      
      await SaveNetwork(config);
      await loadNetworks();
      // Reload servers for the updated network
      if (editingNetwork) {
        await loadNetworkServers(editingNetwork.id);
      }
      await loadConnectionStatus();
      handleCancel();
      if (onServerUpdate) {
        onServerUpdate();
      }
    } catch (error) {
      console.error('Failed to save network:', error);
      alert(`Failed to save network: ${error}`);
    }
  };

  const handleDelete = async (networkId: number) => {
    if (!confirm(`Are you sure you want to delete this network? This will also delete all associated channels and messages.`)) {
      return;
    }
    try {
      // Disconnect if connected
      if (connectionStatus[networkId]) {
        await DisconnectNetwork(networkId);
      }
      await DeleteNetwork(networkId);
      await loadNetworks();
      await loadConnectionStatus();
      if (onServerUpdate) {
        onServerUpdate();
      }
    } catch (error) {
      console.error('Failed to delete server:', error);
      alert(`Failed to delete server: ${error}`);
    }
  };

  const handleConnect = async (network: storage.Network) => {
    try {
      // Load network servers from database
      const servers = await GetServers(network.id);
      const configData: any = {
        name: network.name,
        nickname: network.nickname,
        username: network.username,
        realname: network.realname,
        password: network.password,
        sasl_enabled: network.sasl_enabled || false,
        sasl_mechanism: network.sasl_mechanism || '',
        sasl_username: network.sasl_username || '',
        sasl_password: network.sasl_password || '',
        sasl_external_cert: network.sasl_external_cert || '',
      };
      
      // Use servers from database if available, otherwise fall back to legacy fields
      if (servers && servers.length > 0) {
        configData.servers = servers.map(srv => ({
          address: srv.address,
          port: srv.port,
          tls: srv.tls,
          order: srv.order,
        }));
      } else {
        // Fallback to legacy single address fields
        configData.address = network.address;
        configData.port = network.port;
        configData.tls = network.tls;
      }
      
      const config = main.NetworkConfig.createFrom(configData);
      await ConnectNetwork(config);
      await loadNetworks();
      await loadConnectionStatus();
      if (onServerUpdate) {
        onServerUpdate();
      }
    } catch (error) {
      console.error('Failed to connect:', error);
      alert(`Failed to connect: ${error}`);
    }
  };

  const handleDisconnect = async (networkId: number) => {
    try {
      await DisconnectNetwork(networkId);
      await loadNetworks();
      await loadConnectionStatus();
      if (onServerUpdate) {
        onServerUpdate();
      }
    } catch (error) {
      console.error('Failed to disconnect:', error);
      alert(`Failed to disconnect: ${error}`);
    }
  };

  const renderContent = () => {
    switch (selectedSection) {
      case 'networks':
        return (
          <div className="mb-6">
            <div className="flex items-center justify-between mb-4">
              <h3 className="text-md font-semibold">IRC Networks</h3>
              <button
                type="button"
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  handleAdd();
                }}
                className="px-3 py-1 text-sm border border-border rounded hover:bg-accent"
                disabled={showAddForm || editingNetwork !== null}
              >
                + Add Network
              </button>
            </div>

            {/* Add/Edit Form */}
            {(showAddForm || editingNetwork) && (
              <div className="mb-4 p-4 border border-border rounded bg-muted/50">
                <h4 className="font-semibold mb-3">
                  {editingNetwork ? 'Edit Network' : 'Add Network'}
                </h4>
                <form onSubmit={handleSave} className="space-y-3">
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-sm font-medium mb-1">Name</label>
                      <input
                        type="text"
                        value={formData.name || ''}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, name: e.target.value }))}
                        className="w-full px-2 py-1 text-sm border border-border rounded"
                        required
                        placeholder="My IRC Server"
                      />
                    </div>
                  </div>
                  
                  {/* Server Addresses Section */}
                  <div className="mt-4">
                    <div className="flex items-center justify-between mb-2">
                      <div>
                        <label className="block text-sm font-medium">Server Addresses</label>
                        <p className="text-xs text-muted-foreground mt-0.5">
                          Configure one or more server addresses. Each server can have its own TLS setting.
                        </p>
                      </div>
                      <button
                        type="button"
                        onClick={() => {
                          if (editingNetwork) {
                            const current = networkServers[editingNetwork.id] || [];
                            const newSrv = storage.Server.createFrom({
                              id: 0,
                              network_id: editingNetwork.id,
                              address: '',
                              port: 6667,
                              tls: false,
                              order: current.length,
                              created_at: new Date().toISOString(),
                            });
                            setNetworkServers(prev => ({
                              ...prev,
                              [editingNetwork.id]: [...current, newSrv],
                            }));
                          } else {
                            // New network - add to formData
                            const current = formData.servers || [];
                            setFormData(main.NetworkConfig.createFrom({
                              ...formData,
                              servers: [...current, { address: '', port: 6667, tls: false, order: current.length }],
                            }));
                          }
                        }}
                        className="px-2 py-1 text-xs border border-border rounded hover:bg-accent"
                      >
                        + Add Server
                      </button>
                    </div>
                    <div className="space-y-2">
                      {(editingNetwork 
                        ? (networkServers[editingNetwork.id] || [])
                        : (formData.servers || [])
                      ).map((srv, index) => (
                        <div key={index} className="flex gap-2 items-center p-2 border border-border rounded bg-background">
                          <div className="flex-1 grid grid-cols-3 gap-2">
                            <input
                              type="text"
                              value={editingNetwork ? (srv as storage.Server).address : (srv as main.ServerConfig).address}
                              onChange={(e) => {
                                if (editingNetwork) {
                                  const updated = [...(networkServers[editingNetwork.id] || [])];
                                  updated[index] = { ...updated[index], address: e.target.value } as storage.Server;
                                  setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                } else {
                                  const updated = [...(formData.servers || [])];
                                  updated[index] = { ...updated[index], address: e.target.value };
                                  setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                }
                              }}
                              className="px-2 py-1 text-sm border border-border rounded"
                              placeholder="irc.example.com"
                            />
                            <input
                              title="Server Port"
                              placeholder="6667"
                              type="number"
                              value={editingNetwork ? (srv as storage.Server).port : (srv as main.ServerConfig).port}
                              onChange={(e) => {
                                if (editingNetwork) {
                                  const updated = [...(networkServers[editingNetwork.id] || [])];
                                  updated[index] = { ...updated[index], port: parseInt(e.target.value) || 6667 } as storage.Server;
                                  setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                } else {
                                  const updated = [...(formData.servers || [])];
                                  updated[index] = { ...updated[index], port: parseInt(e.target.value) || 6667 };
                                  setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                }
                              }}
                              className="px-2 py-1 text-sm border border-border rounded"
                              min="1"
                              max="65535"
                            />
                            <label className="flex items-center space-x-2">
                              <input
                                type="checkbox"
                                checked={editingNetwork ? (srv as storage.Server).tls : (srv as main.ServerConfig).tls}
                                onChange={(e) => {
                                  if (editingNetwork) {
                                    const updated = [...(networkServers[editingNetwork.id] || [])];
                                    updated[index] = { ...updated[index], tls: e.target.checked } as storage.Server;
                                    setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                  } else {
                                    const updated = [...(formData.servers || [])];
                                    updated[index] = { ...updated[index], tls: e.target.checked };
                                    setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                  }
                                }}
                              />
                              <span className="text-xs">TLS</span>
                            </label>
                          </div>
                          <div className="flex gap-1">
                            <button
                              type="button"
                              onClick={() => {
                                if (editingNetwork) {
                                  const updated = [...(networkServers[editingNetwork.id] || [])];
                                  if (index > 0) {
                                    [updated[index - 1], updated[index]] = [updated[index], updated[index - 1]];
                                    updated[index - 1].order = index - 1;
                                    updated[index].order = index;
                                    setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                  }
                                } else {
                                  const updated = [...(formData.servers || [])];
                                  if (index > 0) {
                                    [updated[index - 1], updated[index]] = [updated[index], updated[index - 1]];
                                    updated[index - 1].order = index - 1;
                                    updated[index].order = index;
                                    setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                  }
                                }
                              }}
                              className="px-2 py-1 text-xs border border-border rounded hover:bg-accent"
                              disabled={index === 0}
                              title="Move up"
                            >
                              ↑
                            </button>
                            <button
                              type="button"
                              onClick={() => {
                                if (editingNetwork) {
                                  const updated = [...(networkServers[editingNetwork.id] || [])];
                                  if (index < updated.length - 1) {
                                    [updated[index], updated[index + 1]] = [updated[index + 1], updated[index]];
                                    updated[index].order = index;
                                    updated[index + 1].order = index + 1;
                                    setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                  }
                                } else {
                                  const updated = [...(formData.servers || [])];
                                  if (index < updated.length - 1) {
                                    [updated[index], updated[index + 1]] = [updated[index + 1], updated[index]];
                                    updated[index].order = index;
                                    updated[index + 1].order = index + 1;
                                    setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                  }
                                }
                              }}
                              className="px-2 py-1 text-xs border border-border rounded hover:bg-accent"
                              disabled={index === (editingNetwork 
                                ? (networkServers[editingNetwork.id] || []).length - 1
                                : (formData.servers || []).length - 1)}
                              title="Move down"
                            >
                              ↓
                            </button>
                            <button
                              type="button"
                              onClick={() => {
                                if (editingNetwork) {
                                  const updated = [...(networkServers[editingNetwork.id] || [])];
                                  updated.splice(index, 1);
                                  updated.forEach((s, i) => { s.order = i; });
                                  setNetworkServers(prev => ({ ...prev, [editingNetwork.id]: updated }));
                                } else {
                                  const updated = [...(formData.servers || [])];
                                  updated.splice(index, 1);
                                  updated.forEach((s, i) => { s.order = i; });
                                  setFormData(main.NetworkConfig.createFrom({ ...formData, servers: updated }));
                                }
                              }}
                              className="px-2 py-1 text-xs border border-border rounded hover:bg-destructive hover:text-destructive-foreground"
                              title="Remove"
                            >
                              ×
                            </button>
                          </div>
                        </div>
                      ))}
                      {((editingNetwork && (!networkServers[editingNetwork.id] || networkServers[editingNetwork.id].length === 0)) ||
                        (!editingNetwork && (!formData.servers || formData.servers.length === 0))) && (
                        <div className="text-sm text-muted-foreground p-2 text-center">
                          No servers configured. Add at least one server address.
                        </div>
                      )}
                    </div>
                  </div>
                  
                  <div className="grid grid-cols-2 gap-3 mt-4">
                    <div>
                      <label className="block text-sm font-medium mb-1">Nickname</label>
                      <input
                        type="text"
                        value={formData.nickname}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, nickname: e.target.value }))}
                        className="w-full px-2 py-1 text-sm border border-border rounded"
                        required
                        placeholder="MyNick"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1">Username</label>
                      <input
                        type="text"
                        value={formData.username}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, username: e.target.value }))}
                        className="w-full px-2 py-1 text-sm border border-border rounded"
                        placeholder="username"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1">Realname</label>
                      <input
                        type="text"
                        value={formData.realname}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, realname: e.target.value }))}
                        className="w-full px-2 py-1 text-sm border border-border rounded"
                        placeholder="Real Name"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1">Password (optional)</label>
                      <input
                        type="password"
                        value={formData.password}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, password: e.target.value }))}
                        className="w-full px-2 py-1 text-sm border border-border rounded"
                        placeholder="Server password"
                      />
                    </div>
                  </div>

                  {/* Auto-Connect Section */}
                  <div className="mt-4">
                    <label className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={(formData as any).auto_connect || false}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, auto_connect: e.target.checked }))}
                      />
                      <span className="text-sm">Auto-connect on start</span>
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Automatically connect to this network when the application starts
                    </p>
                  </div>

                  {/* SASL Configuration Section */}
                  <div className="mt-4 p-4 border border-border rounded bg-muted/30">
                    <div className="flex items-center justify-between mb-3">
                      <h5 className="font-semibold text-sm">SASL Authentication</h5>
                      <label className="flex items-center space-x-2">
                        <input
                          type="checkbox"
                          checked={formData.sasl_enabled || false}
                          onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_enabled: e.target.checked }))}
                        />
                        <span className="text-sm">Enable SASL</span>
                      </label>
                    </div>
                    
                    {formData.sasl_enabled && (
                      <div className="space-y-3">
                        <div>
                          <label className="block text-sm font-medium mb-1">SASL Mechanism</label>
                          <Select
                            value={formData.sasl_mechanism || ''}
                            onValueChange={(value) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_mechanism: value || '' }))}
                          >
                            <SelectTrigger className="w-full">
                              <SelectValue placeholder="Select mechanism..." />
                            </SelectTrigger>
                            <SelectContent>
                              <SelectItem value="PLAIN">PLAIN</SelectItem>
                              <SelectItem value="EXTERNAL">EXTERNAL</SelectItem>
                              <SelectItem value="SCRAM-SHA-256">SCRAM-SHA-256</SelectItem>
                              <SelectItem value="SCRAM-SHA-512">SCRAM-SHA-512</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                        
                        {formData.sasl_mechanism && formData.sasl_mechanism !== 'EXTERNAL' && (
                          <>
                            <div>
                              <label className="block text-sm font-medium mb-1">SASL Username</label>
                              <input
                                type="text"
                                value={formData.sasl_username || ''}
                                onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_username: e.target.value }))}
                                className="w-full px-2 py-1 text-sm border border-border rounded"
                                placeholder="SASL username"
                              />
                            </div>
                            <div>
                              <label className="block text-sm font-medium mb-1">SASL Password</label>
                              <input
                                type="password"
                                value={formData.sasl_password || ''}
                                onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_password: e.target.value }))}
                                className="w-full px-2 py-1 text-sm border border-border rounded"
                                placeholder="SASL password"
                              />
                            </div>
                          </>
                        )}
                        
                        {formData.sasl_mechanism === 'EXTERNAL' && (
                          <div>
                            <label className="block text-sm font-medium mb-1">Client Certificate Path (optional)</label>
                            <input
                              type="text"
                              value={formData.sasl_external_cert || ''}
                              onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_external_cert: e.target.value }))}
                              className="w-full px-2 py-1 text-sm border border-border rounded"
                              placeholder="/path/to/client.crt"
                            />
                            <p className="text-xs text-muted-foreground mt-1">
                              EXTERNAL uses TLS client certificate authentication. Leave empty to use system certificate.
                            </p>
                          </div>
                        )}
                        
                        {formData.sasl_mechanism === 'PLAIN' && (
                          <p className="text-xs text-muted-foreground">
                            ⚠️ PLAIN mechanism sends credentials in plain text. Only use over TLS/SSL connections.
                          </p>
                        )}
                      </div>
                    )}
                  </div>

                  <div className="flex gap-2 justify-end mt-4">
                    <button
                      type="button"
                      onClick={handleCancel}
                      className="px-4 py-2 text-sm border border-border rounded hover:bg-accent"
                    >
                      Cancel
                    </button>
                    <button
                      type="submit"
                      className="px-4 py-2 text-sm bg-primary text-primary-foreground rounded hover:bg-primary/90"
                    >
                      {editingNetwork ? 'Update' : 'Add'} Network
                    </button>
                  </div>
                </form>
              </div>
            )}

            {/* Network List */}
            {networks.length === 0 ? (
              <div className="text-center text-muted-foreground py-8">
                No networks configured. Click "Add Network" to get started.
              </div>
            ) : (
              <div className="space-y-2">
                {networks.map((network) => {
                  const isConnected = connectionStatus[network.id] || false;
                  const isEditing = editingNetwork?.id === network.id;
                  
                  return (
                    <div
                      key={network.id}
                      className={`border border-border rounded p-4 ${
                        isEditing ? 'bg-accent' : ''
                      }`}
                    >
                      <div className="flex items-start justify-between">
                        <div className="flex-1">
                          <div className="flex items-center gap-2 mb-2">
                            <h4 className="font-semibold">{network.name}</h4>
                            <span className={`w-2 h-2 rounded-full ${
                              isConnected ? 'bg-green-500' : 'bg-gray-400'
                            }`} title={isConnected ? 'Connected' : 'Disconnected'} />
                          </div>
                          <div className="text-sm text-muted-foreground space-y-1">
                            {networkServers[network.id] && networkServers[network.id].length > 0 ? (
                              <div>
                                {networkServers[network.id].map((srv, idx) => (
                                  <div key={idx}>
                                    {srv.address}:{srv.port} {srv.tls && '(TLS)'} {idx === 0 && '(Primary)'}
                                  </div>
                                ))}
                              </div>
                            ) : (
                              <div>{network.address}:{network.port} {network.tls && '(TLS)'}</div>
                            )}
                            <div>Nickname: {network.nickname}</div>
                            {network.username && <div>Username: {network.username}</div>}
                            {network.realname && <div>Realname: {network.realname}</div>}
                          </div>
                        </div>
                        <div className="flex gap-2 ml-4">
                          {isConnected ? (
                            <button
                              onClick={() => handleDisconnect(network.id)}
                              className="px-3 py-1 text-xs border border-border rounded hover:bg-destructive hover:text-destructive-foreground"
                            >
                              Disconnect
                            </button>
                          ) : (
                            <button
                              onClick={() => handleConnect(network)}
                              className="px-3 py-1 text-xs border border-border rounded hover:bg-primary hover:text-primary-foreground"
                            >
                              Connect
                            </button>
                          )}
                          <button
                            onClick={() => handleEdit(network)}
                            className="px-3 py-1 text-xs border border-border rounded hover:bg-accent"
                            disabled={showAddForm}
                          >
                            Edit
                          </button>
                          <button
                            onClick={() => handleDelete(network.id)}
                            className="px-3 py-1 text-xs border border-border rounded hover:bg-destructive hover:text-destructive-foreground"
                            disabled={isConnected || isEditing}
                          >
                            Delete
                          </button>
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        );
      case 'plugins':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Plugins</h3>
            {plugins.length === 0 ? (
              <div className="text-center text-muted-foreground py-8">
                No plugins installed
              </div>
            ) : (
              <div className="space-y-4">
                {plugins.map((plugin) => (
                  <div key={plugin.name} className="border border-border rounded p-4">
                    <div className="flex items-center justify-between mb-2">
                      <h4 className="font-semibold">{plugin.name}</h4>
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
        );
      case 'display':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Display Settings</h3>
            <div className="space-y-4">
              <div className="border border-border rounded p-4">
                <label className="flex items-center space-x-2">
                  <input
                    type="checkbox"
                    checked={consolidateJoinQuit}
                    onChange={(e) => setConsolidateJoinQuit(e.target.checked)}
                  />
                  <span className="text-sm font-medium">Consolidate join/quit messages</span>
                </label>
                <p className="text-xs text-muted-foreground mt-2 ml-6">
                  When enabled, consecutive join, part, or quit messages of the same type will be combined into a single line (e.g., "A, B, C joins" instead of three separate lines).
                </p>
              </div>
            </div>
          </div>
        );
      default:
        return null;
    }
  };

  return (
    <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50" onClick={onClose}>
      <div 
        className="bg-background border border-border rounded-lg w-full max-w-5xl max-h-[85vh] flex flex-col overflow-hidden"
        style={{ backgroundColor: 'var(--background)' }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-4 border-b border-border flex items-center justify-between">
          <h2 className="text-lg font-semibold">Settings</h2>
          <button
            onClick={onClose}
            className="px-3 py-1 text-sm border border-border rounded hover:bg-accent"
          >
            Close
          </button>
        </div>
        
        <div className="flex-1 flex overflow-hidden">
          {/* Left Sidebar Navigation */}
          <div className="w-48 border-r border-border flex-shrink-0 rounded-bl-lg" style={{ backgroundColor: 'var(--background)' }}>
            <nav className="p-2">
              <button
                onClick={() => setSelectedSection('networks')}
                className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
                  selectedSection === 'networks'
                    ? 'bg-primary text-primary-foreground font-medium border-l-2 border-primary'
                    : 'hover:bg-accent/50 text-foreground'
                }`}
              >
                Networks
              </button>
              <button
                onClick={() => setSelectedSection('plugins')}
                className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
                  selectedSection === 'plugins'
                    ? 'bg-primary text-primary-foreground font-medium border-l-2 border-primary'
                    : 'hover:bg-accent/50 text-foreground'
                }`}
              >
                Plugins
              </button>
              <button
                onClick={() => setSelectedSection('display')}
                className={`w-full text-left px-3 py-2 rounded text-sm transition-colors ${
                  selectedSection === 'display'
                    ? 'bg-primary text-primary-foreground font-medium border-l-2 border-primary'
                    : 'hover:bg-accent/50 text-foreground'
                }`}
              >
                Display
              </button>
            </nav>
          </div>

          {/* Right Content Area */}
          <div className="flex-1 overflow-y-auto p-4">
            {renderContent()}
          </div>
        </div>
      </div>
    </div>
  );
}

