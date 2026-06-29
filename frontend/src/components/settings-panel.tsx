import { useState, useEffect, useRef } from 'react';
import { ArrowLeft, ChevronRight } from 'lucide-react';
import { main, storage } from '../../wailsjs/go/models';
import { GetNetworks, SaveNetwork, ConnectNetwork, DeleteNetwork, DisconnectNetwork, GetConnectionStatus, GetServers, ListPlugins, EnablePlugin, DisablePlugin, ReloadPlugin, GetBuildInfo, CheckForUpdates, GetLogConfig, SetLogConfig, GetDefaultLogPath, GetSTSPolicies, ClearSTSPolicy, RequestNotificationPermission, GetPendingNetworkPrefill, GetSetting, SetSetting } from '../../wailsjs/go/main/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { PluginConfigForm } from './plugin-config-form';
import { ScriptsPanel } from './scripts-panel';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from './ui/select';
import { useThemeStore, ACCENTS, type ThemeMode } from '../stores/theme';
import { useSettingsStore, type PrefixDisplayMode, type UpdateChannel } from '../stores/settings';
import { usePreferencesStore } from '../stores/preferences';

function serializeNetworkForm(
  fd: main.NetworkConfig,
  servers: Array<{ address: string; port: number; tls: boolean }>,
): string {
  return JSON.stringify({
    name: fd.name ?? '',
    nickname: fd.nickname ?? '',
    username: fd.username ?? '',
    realname: fd.realname ?? '',
    password: fd.password ?? '',
    sasl_enabled: (fd as any).sasl_enabled ?? false,
    sasl_mechanism: (fd as any).sasl_mechanism ?? '',
    sasl_username: (fd as any).sasl_username ?? '',
    sasl_password: (fd as any).sasl_password ?? '',
    sasl_external_cert: (fd as any).sasl_external_cert ?? '',
    auto_connect: (fd as any).auto_connect ?? false,
    identify_as_bot: (fd as any).identify_as_bot ?? false,
    servers: (servers ?? []).map((s) => ({ address: s.address ?? '', port: s.port ?? 6667, tls: s.tls ?? false })),
  });
}

export type SettingsSection = 'networks' | 'plugins' | 'scripts' | 'display' | 'notifications' | 'advanced' | 'about';

export const isSettingsSection = (v: string): v is SettingsSection =>
  v === 'networks' || v === 'plugins' || v === 'scripts' || v === 'display' || v === 'notifications' || v === 'advanced' || v === 'about';

interface SettingsPanelProps {
  /** Currently selected pane (controlled by the host window). */
  section: SettingsSection;
  /** Called when the user picks a different pane from the left nav. */
  onSectionChange: (section: SettingsSection) => void;
}

/** A small on-brand switch toggle (design system uses switches, not checkboxes). */
function Toggle({ checked, onChange }: { checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      onClick={() => onChange(!checked)}
      className={`relative inline-flex h-5 w-9 flex-shrink-0 items-center rounded-full transition-colors cursor-pointer ${
        checked ? 'bg-primary' : 'bg-muted-foreground/30'
      }`}
      style={{ transition: 'var(--transition-base)' }}
    >
      <span
        className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${
          checked ? 'translate-x-[1.125rem]' : 'translate-x-0.5'
        }`}
        style={{ transition: 'var(--transition-base)' }}
      />
    </button>
  );
}

/**
 * StsIndicator shows an IRCv3 STS lock badge for a host whose connections are
 * being force-upgraded to TLS, with the policy's expiry and a clear control.
 * Renders nothing when there's no active (non-expired) policy for the host.
 */
function StsIndicator({ policy, onClear }: { policy?: storage.STSPolicy; onClear: () => void }) {
  if (!policy) return null;
  const expiresMs = policy.expires_at * 1000;
  if (expiresMs <= Date.now()) return null;
  const until = new Date(expiresMs).toLocaleDateString(undefined, { year: 'numeric', month: 'short', day: 'numeric' });
  return (
    <span
      className="inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full bg-green-500/15 text-green-700 dark:text-green-400"
      title={`Strict Transport Security: TLS is enforced for ${policy.hostname} on port ${policy.port} until ${until}.`}
    >
      <span aria-hidden>🔒</span>
      STS · TLS enforced until {until}
      <button
        type="button"
        onClick={(e) => { e.stopPropagation(); onClear(); }}
        className="ml-1 underline decoration-dotted hover:no-underline cursor-pointer"
      >
        Clear
      </button>
    </span>
  );
}

export function SettingsPanel({ section, onSectionChange }: SettingsPanelProps) {
  const [networks, setNetworks] = useState<storage.Network[]>([]);
  const [plugins, setPlugins] = useState<main.PluginInfo[]>([]);
  const [editingNetwork, setEditingNetwork] = useState<storage.Network | null>(null);

  // Consolidate join/quit lives in the shared settings store (durable + reactive),
  // so toggling it here updates the message view live and survives restarts.
  const consolidateJoinQuit = useSettingsStore((s) => s.consolidateJoinQuit);
  const setConsolidateJoinQuit = useSettingsStore((s) => s.setConsolidateJoinQuit);
  const prefixDisplayMode = useSettingsStore((s) => s.prefixDisplayMode);
  const setPrefixDisplayMode = useSettingsStore((s) => s.setPrefixDisplayMode);
  const updateChannel = useSettingsStore((s) => s.updateChannel);
  const setUpdateChannel = useSettingsStore((s) => s.setUpdateChannel);
  const notificationsEnabled = useSettingsStore((s) => s.notificationsEnabled);
  const setNotificationsEnabled = useSettingsStore((s) => s.setNotificationsEnabled);
  const notifyPrivateMessages = useSettingsStore((s) => s.notifyPrivateMessages);
  const setNotifyPrivateMessages = useSettingsStore((s) => s.setNotifyPrivateMessages);
  const notifyMentions = useSettingsStore((s) => s.notifyMentions);
  const setNotifyMentions = useSettingsStore((s) => s.setNotifyMentions);
  const notifyConnectionLost = useSettingsStore((s) => s.notifyConnectionLost);
  const setNotifyConnectionLost = useSettingsStore((s) => s.setNotifyConnectionLost);
  const notifyOnlyWhenUnfocused = useSettingsStore((s) => s.notifyOnlyWhenUnfocused);
  const setNotifyOnlyWhenUnfocused = useSettingsStore((s) => s.setNotifyOnlyWhenUnfocused);
  const typingSend = useSettingsStore((s) => s.typingSend);
  const setTypingSend = useSettingsStore((s) => s.setTypingSend);
  const typingReceive = useSettingsStore((s) => s.typingReceive);
  const setTypingReceive = useSettingsStore((s) => s.setTypingReceive);
  const reconnectOnAuthFailure = useSettingsStore((s) => s.reconnectOnAuthFailure);
  const setReconnectOnAuthFailure = useSettingsStore((s) => s.setReconnectOnAuthFailure);
  const unfurlsEnabled = useSettingsStore((s) => s.unfurlsEnabled);
  const setUnfurlsEnabled = useSettingsStore((s) => s.setUnfurlsEnabled);
  const [notifyNotice, setNotifyNotice] = useState<string | null>(null);

  // Invite notification settings — loaded from the backend settings table once
  // on mount; changes written through immediately via SetSetting.
  type InviteAttentionLevel = 'trusted' | 'quiet' | 'all';
  const [inviteAttentionLevel, setInviteAttentionLevelState] = useState<InviteAttentionLevel>('trusted');
  const [inviteTtlHours, setInviteTtlHoursState] = useState<number>(24);

  const setInviteAttentionLevel = (value: InviteAttentionLevel) => {
    setInviteAttentionLevelState(value);
    SetSetting('invites.attentionLevel', value).catch((e) =>
      console.error('Failed to persist invites.attentionLevel:', e),
    );
  };

  const setInviteTtlHours = (value: number) => {
    const clamped = Math.max(1, value);
    setInviteTtlHoursState(clamped);
    SetSetting('invites.ttlHours', String(clamped)).catch((e) =>
      console.error('Failed to persist invites.ttlHours:', e),
    );
  };

  // Theme (appearance + accent)
  const themeMode = useThemeStore((s) => s.mode);
  const accent = useThemeStore((s) => s.accent);
  const setThemeMode = useThemeStore((s) => s.setMode);
  const setAccent = useThemeStore((s) => s.setAccent);

  // Composer formatting toolbar visibility (shared live with the in-composer "Aa" toggle)
  const showFormattingToolbar = usePreferencesStore((s) => s.showFormattingToolbar);
  const setShowFormattingToolbar = usePreferencesStore((s) => s.setShowFormattingToolbar);
  // Help display mode: show /help output as a dialog or in the buffer
  const helpDisplayMode = usePreferencesStore((s) => s.helpDisplayMode);
  const setHelpDisplayMode = usePreferencesStore((s) => s.setHelpDisplayMode);
  // Close buffer on leave: also remove buffer from sidebar when parting a channel
  const closeBufferOnLeave = usePreferencesStore((s) => s.closeBufferOnLeave);
  const setCloseBufferOnLeave = usePreferencesStore((s) => s.setCloseBufferOnLeave);
  const [pluginLoading, setPluginLoading] = useState<Set<string>>(new Set());
  const [expandedPlugins, setExpandedPlugins] = useState<Set<string>>(new Set());
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
  const formSnapshotRef = useRef<string>('');
  // STS policies keyed by hostname, so each server row can show whether TLS is enforced.
  const [stsPolicies, setStsPolicies] = useState<Record<string, storage.STSPolicy>>({});
  const [buildInfo, setBuildInfo] = useState<main.BuildInfo | null>(null);
  // Set when the backend reports the updater is unavailable (dev builds where
  // it was never configured). Shown inline under the "Check for Updates…" button.
  const [updateNotice, setUpdateNotice] = useState<string | null>(null);

  // File-logging config (Advanced section). Persisted in the backend settings
  // table and applied to the global logger live via SetLogConfig.
  const [logConfig, setLogConfig] = useState<main.LogConfig | null>(null);
  const [defaultLogPath, setDefaultLogPath] = useState('');
  const [logError, setLogError] = useState<string | null>(null);
  const [logSaving, setLogSaving] = useState(false);
  // Path edits are held locally and applied on blur, so we don't reconfigure the
  // logger on every keystroke.
  const [logPathDraft, setLogPathDraft] = useState('');
  useEffect(() => {
    if (logConfig) setLogPathDraft(logConfig.path);
  }, [logConfig?.path]);

  useEffect(() => {
    loadNetworks();
    loadPlugins();
    loadStsPolicies();
    GetBuildInfo()
      .then(setBuildInfo)
      .catch((error) => console.error('Failed to load build info:', error));
    GetLogConfig()
      .then(setLogConfig)
      .catch((error) => console.error('Failed to load log config:', error));
    GetDefaultLogPath()
      .then(setDefaultLogPath)
      .catch((error) => console.error('Failed to load default log path:', error));
    // Load invite settings from the backend settings table.
    GetSetting('invites.attentionLevel')
      .then((v) => {
        if (v === 'trusted' || v === 'quiet' || v === 'all') setInviteAttentionLevelState(v);
      })
      .catch((e) => console.error('Failed to load invites.attentionLevel:', e));
    GetSetting('invites.ttlHours')
      .then((v) => {
        const n = parseInt(v, 10);
        if (!isNaN(n) && n >= 1) setInviteTtlHoursState(n);
      })
      .catch((e) => console.error('Failed to load invites.ttlHours:', e));
  }, []);

  // Persist a log-config change and apply it live. The backend rejects an
  // unwritable path before changing anything, so on error we surface it and
  // reload the still-current config rather than leaving the UI out of sync.
  const saveLogConfig = async (next: main.LogConfig) => {
    setLogSaving(true);
    setLogError(null);
    const previous = logConfig;
    setLogConfig(next); // optimistic
    try {
      await SetLogConfig(next.enabled, next.path, next.level);
    } catch (error) {
      setLogConfig(previous);
      setLogError(String(error));
    } finally {
      setLogSaving(false);
    }
  };

  // The backend emits updater:unavailable when "Check for Updates…" is pressed
  // on a dev build (the updater is only configured for installed releases). In
  // a real release build the framework's own updater window takes over instead.
  useEffect(() => {
    const unsubscribe = EventsOn('updater:unavailable', () => {
      setUpdateNotice('Updates are only available in installed release builds.');
    });
    return () => unsubscribe();
  }, []);

  // On Linux, AppImage and system (deb/rpm) installs can't self-update in place,
  // so the backend emits updater:manual-update with a link instead of opening the
  // updater window. Point the user at the releases page to grab the new build.
  useEffect(() => {
    const unsubscribe = EventsOn('updater:manual-update', (data: any) => {
      const url = data?.url || 'https://github.com/matt0x6f/irc-client/releases/latest';
      setUpdateNotice(`A new version may be available — your install type can't update itself. Download the latest build from ${url}`);
    });
    return () => unsubscribe();
  }, []);

  // The backend emits sts-policy whenever a policy is learned, refreshed, or cleared
  // (in this or another window), so the lock badges stay live without a reload.
  useEffect(() => {
    const unsubscribe = EventsOn('sts-policy', () => {
      loadStsPolicies();
    });
    return () => unsubscribe();
  }, []);

  // Deep-link prefill: when a deep link for an unknown host arrives, the backend
  // stores a one-shot NetworkPrefill and emits deeplink:add-network. This window
  // fetches it via GetPendingNetworkPrefill (which clears it atomically) and opens
  // the Add Network form prefilled with host/port/tls. The channel from the URL is
  // not part of NetworkConfig (no autojoin field), so it is dropped here — the user
  // joins the channel after connecting.
  useEffect(() => {
    const applyPrefill = async () => {
      try {
        const p = await GetPendingNetworkPrefill();
        if (!p || !p.host) return;
        setEditingNetwork(null);
        setFormData(main.NetworkConfig.createFrom({
          name: p.host,
          servers: [{ address: p.host, port: p.port, tls: p.tls, order: 0 }],
          nickname: '',
          username: '',
          realname: '',
          password: '',
        }));
        setShowAddForm(true);
        onSectionChange('networks');
      } catch (error) {
        console.error('Failed to load pending network prefill:', error);
      }
    };
    // Cold-open: fetch once on mount in case a prefill was stored before this window opened.
    void applyPrefill();
    // Already-open: the backend re-emits deeplink:add-network on each new deep link.
    const off = EventsOn('deeplink:add-network', () => { void applyPrefill(); });
    return () => { off && off(); };
  }, []);

  const loadStsPolicies = async () => {
    try {
      const policies = await GetSTSPolicies();
      const byHost: Record<string, storage.STSPolicy> = {};
      (policies || []).forEach((p) => { byHost[p.hostname] = p; });
      setStsPolicies(byHost);
    } catch (error) {
      console.error('Failed to load STS policies:', error);
    }
  };

  const handleClearSts = async (hostname: string) => {
    if (!confirm(
      `Clear the STS policy for ${hostname}?\n\n` +
      'This removes enforced TLS for this host. The next connection may use the ' +
      'plaintext port you configured until the server re-advertises STS — a security ' +
      'downgrade. Only do this to recover from a misconfigured server.'
    )) {
      return;
    }
    try {
      await ClearSTSPolicy(hostname);
      await loadStsPolicies();
    } catch (error) {
      console.error('Failed to clear STS policy:', error);
      alert(`Failed to clear STS policy: ${error}`);
    }
  };

  const loadPlugins = async () => {
    try {
      const pluginList = await ListPlugins();
      console.log('Loaded plugins:', pluginList);
      pluginList.forEach(p => {
        console.log(`Plugin ${p.name}: has config_schema:`, !!p.config_schema, 'schema:', p.config_schema);
      });
      setPlugins(pluginList || []);
    } catch (error) {
      console.error('Failed to load plugins:', error);
      setPlugins([]);
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

  const handleReloadPlugin = async (pluginName: string) => {
    setPluginLoading(prev => new Set(prev).add(pluginName));
    try {
      await ReloadPlugin(pluginName);
      await loadPlugins();
    } catch (error) {
      console.error(`Failed to reload plugin:`, error);
      alert(`Failed to reload plugin: ${error}`);
    } finally {
      setPluginLoading(prev => {
        const next = new Set(prev);
        next.delete(pluginName);
        return next;
      });
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
    const built = main.NetworkConfig.createFrom({
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
      identify_as_bot: network.identify_as_bot || false,
    });
    setFormData(built);
    formSnapshotRef.current = serializeNetworkForm(built, (servers || []) as any);
    setShowAddForm(false);
  };

  const handleAdd = () => {
    setEditingNetwork(null);
    const initial = main.NetworkConfig.createFrom({
      name: '',
      address: '',
      port: 6667,
      tls: false,
      servers: [{ address: '', port: 6667, tls: false, order: 0 }],
      nickname: '',
      username: '',
      realname: '',
      password: '',
    });
    setFormData(initial);
    formSnapshotRef.current = serializeNetworkForm(initial, initial.servers as any);
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

  // Servers being edited live in networkServers (existing network) or formData (new).
  const currentEditorServers = (): Array<{ address: string; port: number; tls: boolean }> =>
    (editingNetwork ? (networkServers[editingNetwork.id] ?? []) : (formData.servers ?? [])) as any;

  const isNetworkFormDirty = (): boolean =>
    serializeNetworkForm(formData, currentEditorServers()) !== formSnapshotRef.current;

  const requestEditorExit = () => {
    if (isNetworkFormDirty() && !confirm('Discard changes?')) return;
    handleCancel();
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
        identify_as_bot: (formData as any).identify_as_bot || false,
      });
      
      await SaveNetwork(config);
      await loadNetworks();
      // Reload servers for the updated network
      if (editingNetwork) {
        await loadNetworkServers(editingNetwork.id);
      }
      await loadConnectionStatus();
      handleCancel();    } catch (error) {
      console.error('Failed to save network:', error);
      alert(`Failed to save network: ${error}`);
    }
  };

  const handleDelete = async (networkId: number): Promise<boolean> => {
    if (!confirm(`Are you sure you want to delete this network? This will also delete all associated channels and messages.`)) {
      return false;
    }
    try {
      // Disconnect if connected
      if (connectionStatus[networkId]) {
        await DisconnectNetwork(networkId);
      }
      await DeleteNetwork(networkId);
      await loadNetworks();
      await loadConnectionStatus();
      return true;
    } catch (error) {
      console.error('Failed to delete server:', error);
      alert(`Failed to delete server: ${error}`);
      return false;
    }
  };

  const handleDeleteFromEditor = async () => {
    if (!editingNetwork) return;
    const deleted = await handleDelete(editingNetwork.id);
    if (deleted) handleCancel();
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
      await loadConnectionStatus();    } catch (error) {
      console.error('Failed to connect:', error);
      alert(`Failed to connect: ${error}`);
    }
  };

  const handleDisconnect = async (networkId: number) => {
    try {
      await DisconnectNetwork(networkId);
      await loadNetworks();
      await loadConnectionStatus();    } catch (error) {
      console.error('Failed to disconnect:', error);
      alert(`Failed to disconnect: ${error}`);
    }
  };

  const handleToggleNotifications = async (v: boolean) => {
    if (!v) {
      setNotificationsEnabled(false);
      setNotifyNotice(null);
      return;
    }
    try {
      const granted = await RequestNotificationPermission();
      if (granted) {
        setNotifyNotice(null);
        setNotificationsEnabled(true);
      } else {
        setNotifyNotice(
          'Notifications are blocked. Enable Cascade Chat in System Settings → Notifications, then toggle this again.',
        );
        setNotificationsEnabled(false);
      }
    } catch (e) {
      setNotifyNotice(String(e));
      setNotificationsEnabled(false);
    }
  };

  const renderNetworkList = () => (
    <>
      <div className="flex items-center justify-between mb-4">
        <h3 className="text-md font-semibold">IRC networks</h3>
        <button
          type="button"
          onClick={(e) => { e.preventDefault(); e.stopPropagation(); handleAdd(); }}
          className="px-4 py-2 text-sm border border-border rounded-lg hover:bg-accent transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
          style={{ transition: 'var(--transition-base)' }}
          data-testid="add-network-button"
        >
          + Add network
        </button>
      </div>
      <div data-testid="network-list">
        {networks.length === 0 ? (
          <div className="text-center text-muted-foreground py-8">
            No networks configured. Click "Add network" to get started.
          </div>
        ) : (
          <div className="space-y-2">
            {networks.map((network) => {
              const isConnected = connectionStatus[network.id] || false;

              return (
                <div
                  key={network.id}
                  role="button"
                  tabIndex={0}
                  data-testid={`network-row-${network.id}`}
                  onClick={() => handleEdit(network)}
                  onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); handleEdit(network); } }}
                  className="border border-border rounded-lg p-4 shadow-[var(--shadow-sm)] transition-all hover:shadow-[var(--shadow-md)] cursor-pointer flex items-center justify-between gap-4"
                  style={{ transition: 'var(--transition-base)' }}
                >
                  <div className="min-w-0">
                    <div className="flex items-center gap-2 mb-1">
                      <h4 className="font-semibold truncate">{network.name}</h4>
                      <span
                        className="w-1.5 h-1.5 rounded-full shrink-0"
                        title={isConnected ? 'Connected' : 'Disconnected'}
                        style={{ background: isConnected ? 'var(--presence-online)' : 'var(--presence-offline)' }}
                      />
                    </div>
                    <div className="text-sm text-muted-foreground flex flex-wrap items-center gap-2">
                      {networkServers[network.id] && networkServers[network.id].length > 0 ? (
                        <span className="truncate">
                          {networkServers[network.id][0].address}:{networkServers[network.id][0].port}
                          {networkServers[network.id][0].tls && ' (TLS)'}
                        </span>
                      ) : (
                        <span className="truncate">{network.address}:{network.port} {network.tls && '(TLS)'}</span>
                      )}
                      <StsIndicator
                        policy={stsPolicies[networkServers[network.id]?.[0]?.address ?? network.address]}
                        onClear={() => handleClearSts(networkServers[network.id]?.[0]?.address ?? network.address)}
                      />
                    </div>
                  </div>
                  <div className="flex items-center gap-2 shrink-0" onClick={(e) => e.stopPropagation()}>
                    {isConnected ? (
                      <button
                        onClick={() => handleDisconnect(network.id)}
                        data-testid="network-disconnect-button"
                        className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-destructive hover:text-destructive-foreground transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                      >
                        Disconnect
                      </button>
                    ) : (
                      <button
                        onClick={() => handleConnect(network)}
                        data-testid="network-connect-button"
                        className="px-3 py-1.5 text-xs border border-border rounded-lg hover:bg-primary hover:text-primary-foreground transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                      >
                        Connect
                      </button>
                    )}
                    <span className="text-muted-foreground" aria-hidden="true"><ChevronRight className="w-4 h-4" /></span>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </>
  );

  const renderNetworkEditor = () => {
    const isConnected = editingNetwork ? (connectionStatus[editingNetwork.id] || false) : false;
    return (
    <div data-testid="network-editor" className="p-5 border border-border rounded-lg bg-card/50 shadow-[var(--shadow-sm)]">
      <div className="flex items-center gap-3 mb-5 pb-4 border-b border-border">
        <button
          type="button"
          onClick={requestEditorExit}
          data-testid="network-editor-back"
          aria-label="Back to networks"
          className="p-1.5 -ml-1.5 rounded-lg hover:bg-accent transition-all"
          style={{ transition: 'var(--transition-base)' }}
        >
          <ArrowLeft className="w-4 h-4" />
        </button>
        <h4 className="font-semibold text-lg truncate">
          {editingNetwork ? editingNetwork.name : 'New network'}
        </h4>
        {editingNetwork && (
          <span className={`ml-auto inline-flex items-center gap-1.5 text-xs font-medium px-2 py-0.5 rounded-full ${
            isConnected ? 'bg-green-500/15 text-green-700 dark:text-green-400' : 'bg-muted text-muted-foreground'
          }`}>
            <span className="w-1.5 h-1.5 rounded-full" style={{ background: isConnected ? 'var(--presence-online)' : 'var(--presence-offline)' }} />
            {isConnected ? 'Connected' : 'Disconnected'}
          </span>
        )}
      </div>
      <form id="network-form" onSubmit={handleSave} className="space-y-4">
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Name</label>
                      <input
                        type="text"
                        value={formData.name || ''}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, name: e.target.value }))}
                        className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                        required
                        placeholder="My IRC Server"
                        data-testid="network-name-input"
                      />
                    </div>
                  </div>
                  
                  {/* Server Addresses Section */}
                  <div className="mt-4">
                    <div className="flex items-center justify-between mb-2">
                      <div>
                        <label className="block text-sm font-medium">Server addresses</label>
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
                              className="px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                              style={{ transition: 'var(--transition-base)' }}
                              placeholder="irc.example.com"
                              data-testid="server-address-input"
                            />
                            <input
                              title="Server Port"
                              placeholder="6667"
                              data-testid="server-port-input"
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
                              className="px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                              style={{ transition: 'var(--transition-base)' }}
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
                        className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                        required
                        placeholder="MyNick"
                        data-testid="network-nickname-input"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Username</label>
                      <input
                        type="text"
                        value={formData.username}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, username: e.target.value }))}
                        className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                        placeholder="username"
                        data-testid="network-username-input"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Realname</label>
                      <input
                        type="text"
                        value={formData.realname}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, realname: e.target.value }))}
                        className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
                        placeholder="Real Name"
                        data-testid="network-realname-input"
                      />
                    </div>
                    <div>
                      <label className="block text-sm font-medium mb-1.5">Password (optional)</label>
                      <input
                        type="password"
                        value={formData.password}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, password: e.target.value }))}
                        className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                        style={{ transition: 'var(--transition-base)' }}
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

                  {/* Bot Mode Section */}
                  <div className="mt-4">
                    <label className="flex items-center space-x-2">
                      <input
                        type="checkbox"
                        checked={(formData as any).identify_as_bot || false}
                        onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, identify_as_bot: e.target.checked }))}
                      />
                      <span className="text-sm">Identify as a bot (+B)</span>
                    </label>
                    <p className="text-xs text-muted-foreground mt-1 ml-6">
                      Marks this connection as an automated bot when the server supports IRCv3 bot mode.
                    </p>
                  </div>

                  {/* SASL Configuration Section */}
                  <div className="mt-4 p-4 border border-border rounded bg-muted/30">
                    <div className="flex items-center justify-between mb-3">
                      <h5 className="font-semibold text-sm">SASL authentication</h5>
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
                          <label className="block text-sm font-medium mb-1">SASL mechanism</label>
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
                              <label className="block text-sm font-medium mb-1">SASL username</label>
                              <input
                                type="text"
                                value={formData.sasl_username || ''}
                                onChange={(e) => setFormData(main.NetworkConfig.createFrom({ ...formData, sasl_username: e.target.value }))}
                                className="w-full px-2 py-1 text-sm border border-border rounded"
                                placeholder="SASL username"
                              />
                            </div>
                            <div>
                              <label className="block text-sm font-medium mb-1">SASL password</label>
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

                </form>

      <div className="flex items-center justify-between gap-3 mt-6 pt-4 border-t border-border">
        {editingNetwork ? (
          <button
            type="button"
            onClick={handleDeleteFromEditor}
            data-testid="network-delete-button"
            className="px-3 py-2 text-sm text-destructive border border-border rounded-lg hover:bg-destructive hover:text-destructive-foreground transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
            style={{ transition: 'var(--transition-base)' }}
          >
            Delete
          </button>
        ) : <span />}
        <div className="flex gap-3">
          <button
            type="button"
            onClick={requestEditorExit}
            className="px-4 py-2 text-sm border border-border rounded-lg hover:bg-accent transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
            style={{ transition: 'var(--transition-base)' }}
          >
            Cancel
          </button>
          <button
            type="submit"
            form="network-form"
            data-testid="save-network-button"
            className="px-4 py-2 text-sm bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)] font-medium"
            style={{ transition: 'var(--transition-base)' }}
          >
            {editingNetwork ? 'Save' : 'Add network'}
          </button>
        </div>
      </div>
    </div>
  );
  };

  const renderContent = () => {

    switch (section) {
      case 'networks': {
        const inEditor = showAddForm || editingNetwork !== null;
        return (
          <div className="mb-6">
            <div
              key={inEditor ? 'editor' : 'list'}
              data-testid="network-view"
              className={inEditor ? 'cc-view-enter-forward' : 'cc-view-enter-back'}
            >
              {inEditor ? renderNetworkEditor() : renderNetworkList()}
            </div>
          </div>
        );
      }
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
                {plugins.map((plugin) => {
                  const isExpanded = expandedPlugins.has(plugin.name);
                  const hasConfig = plugin.config_schema && Object.keys(plugin.config_schema).length > 0;
                  console.log(`Plugin ${plugin.name}: hasConfig=${hasConfig}, config_schema:`, plugin.config_schema);
                  
                  return (
                    <div key={plugin.name} className="border border-border rounded p-4">
                      <div className="flex items-center justify-between mb-2">
                        <div className="flex items-center gap-2">
                          <h4 className="font-semibold">{plugin.name}</h4>
                          {hasConfig && (
                            <button
                              onClick={() => {
                                setExpandedPlugins(prev => {
                                  const next = new Set(prev);
                                  if (next.has(plugin.name)) {
                                    next.delete(plugin.name);
                                  } else {
                                    next.add(plugin.name);
                                  }
                                  return next;
                                });
                              }}
                              className="px-2 py-1 text-xs border border-border rounded hover:bg-accent"
                            >
                              {isExpanded ? 'Hide Config' : 'Configure'}
                            </button>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          {plugin.enabled && (
                            <button
                              onClick={() => handleReloadPlugin(plugin.name)}
                              disabled={pluginLoading.has(plugin.name)}
                              className="px-3 py-1 text-xs rounded border border-border hover:bg-accent disabled:opacity-50 disabled:cursor-not-allowed"
                              title="Reload plugin"
                            >
                              {pluginLoading.has(plugin.name) ? '...' : 'Reload'}
                            </button>
                          )}
                          <button
                            onClick={() => handleTogglePlugin(plugin.name, plugin.enabled)}
                            disabled={pluginLoading.has(plugin.name)}
                            className={`px-3 py-1 text-xs rounded border transition-colors ${
                              plugin.enabled
                                ? 'bg-red-500/20 text-red-500 border-red-500/30 hover:bg-red-500/30'
                                : 'bg-green-500/20 text-green-500 border-green-500/30 hover:bg-green-500/30'
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
                    {isExpanded && hasConfig && (
                      <div className="mt-4 border-t border-border pt-4">
                        <PluginConfigForm
                          pluginName={plugin.name}
                          schema={plugin.config_schema as any}
                          onSave={() => {
                            loadPlugins();
                          }}
                        />
                      </div>
                    )}
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        );
      case 'scripts':
        return <ScriptsPanel />;
      case 'display':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Display Settings</h3>
            <div className="space-y-4">
              {/* Theme */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-4">
                <div>
                  <div className="text-sm font-medium mb-2">Appearance</div>
                  <div className="inline-flex rounded-lg border border-border p-0.5 bg-muted/40">
                    {(['light', 'dark', 'system'] as ThemeMode[]).map((m) => (
                      <button
                        key={m}
                        type="button"
                        onClick={() => setThemeMode(m)}
                        className={`px-3 py-1.5 text-sm rounded-md capitalize cursor-pointer transition-colors ${
                          themeMode === m
                            ? 'bg-primary text-primary-foreground font-medium shadow-[var(--shadow-sm)]'
                            : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        {m}
                      </button>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="text-sm font-medium mb-2">Accent</div>
                  <div className="flex items-center gap-3">
                    {ACCENTS.map((a) => (
                      <button
                        key={a.id}
                        type="button"
                        onClick={() => setAccent(a.id)}
                        title={a.label}
                        aria-label={`${a.label} accent`}
                        aria-pressed={accent === a.id}
                        className="w-7 h-7 rounded-full cursor-pointer transition-transform hover:scale-110"
                        style={{
                          background: a.swatch,
                          boxShadow:
                            accent === a.id
                              ? `0 0 0 2px var(--card), 0 0 0 4px ${a.swatch}`
                              : undefined,
                        }}
                      />
                    ))}
                  </div>
                </div>
              </div>

              {/* Messages */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Consolidate join/quit messages</span>
                  <Toggle checked={consolidateJoinQuit} onChange={setConsolidateJoinQuit} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  When enabled, consecutive join, part, or quit messages of the same type will be combined into a single line (e.g., "A, B, C joins" instead of three separate lines).
                </p>
              </div>

              {/* Link previews */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Link previews (unfurls)</span>
                  <Toggle checked={unfurlsEnabled} onChange={setUnfurlsEnabled} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  Show a "Preview" button beside links. Nothing is fetched until you click it —
                  the preview is loaded by the app (not the page), so a link can't see your IP
                  unless you choose to preview it. Off by default.
                </p>
              </div>

              {/* Composer */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Show formatting toolbar</span>
                  <Toggle checked={showFormattingToolbar} onChange={setShowFormattingToolbar} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  Show the bold/italic/underline/colour buttons above the message input. Emoji and mention buttons stay available either way, and you can also toggle this with the "Aa" button in the composer.
                </p>
              </div>

              {/* Typing notifications */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-3">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Send typing notifications</span>
                  <Toggle checked={typingSend} onChange={setTypingSend} />
                </div>
                <p className="text-xs text-muted-foreground">
                  Let others see when you're typing a message (IRCv3 +typing). When off, you still see others' typing but never broadcast your own. Only works on servers that support it.
                </p>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Show others' typing</span>
                  <Toggle checked={typingReceive} onChange={setTypingReceive} />
                </div>
                <p className="text-xs text-muted-foreground">
                  Display a "typing…" line above the message input when people in a channel or PM are composing.
                </p>
              </div>

              {/* Auth failure reconnect */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Automatically reconnect after a failed login</span>
                  <Toggle checked={reconnectOnAuthFailure} onChange={setReconnectOnAuthFailure} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  When off, Cascade stops and shows a banner instead of retrying — recommended, since a wrong password never fixes itself by retrying.
                </p>
              </div>

              {/* Help display mode */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <label className="flex items-center justify-between py-2">
                  <span className="text-sm font-medium">Show /help as</span>
                  <select
                    value={helpDisplayMode}
                    onChange={(e) => setHelpDisplayMode(e.target.value as 'dialog' | 'buffer')}
                    className="border border-border rounded px-2 py-1 bg-background text-sm"
                  >
                    <option value="dialog">Dialog</option>
                    <option value="buffer">Buffer text</option>
                  </select>
                </label>
                <p className="text-xs text-muted-foreground mt-2">
                  Choose whether /help opens a dialog or prints directly into the current buffer.
                </p>
              </div>

              {/* Close buffer on leave */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Close the channel buffer when leaving a channel</span>
                  <Toggle checked={closeBufferOnLeave} onChange={setCloseBufferOnLeave} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  When you choose "Leave Channel", also remove its buffer from the sidebar. Turn off to part the channel but keep the buffer open for scrollback.
                </p>
              </div>

              {/* Nick list — membership prefix display */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Member role display</span>
                  <div className="inline-flex rounded-lg border border-border p-0.5 bg-muted/40">
                    {(['icon', 'text'] as PrefixDisplayMode[]).map((m) => (
                      <button
                        key={m}
                        type="button"
                        onClick={() => setPrefixDisplayMode(m)}
                        className={`px-3 py-1.5 text-sm rounded-md capitalize cursor-pointer transition-colors ${
                          prefixDisplayMode === m
                            ? 'bg-primary text-primary-foreground font-medium shadow-[var(--shadow-sm)]'
                            : 'text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        {m === 'icon' ? 'Icons' : 'Text'}
                      </button>
                    ))}
                  </div>
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  Show a user's channel role in the nick list as an icon (their highest role only) or as text prefixes (e.g. <span className="font-mono">@+</span>, showing every role when the server supports multi-prefix).
                </p>
              </div>
            </div>
          </div>
        );
      case 'notifications':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Notifications</h3>
            <div className="space-y-4">
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)]">
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Enable desktop notifications</span>
                  <Toggle checked={notificationsEnabled} onChange={(v) => void handleToggleNotifications(v)} />
                </div>
                <p className="text-xs text-muted-foreground mt-2">
                  Show native desktop notifications for messages and connection events. Requires granting permission the first time. Notifications only appear in installed builds.
                </p>
                {notifyNotice && (
                  <p className="text-xs text-amber-500 mt-2" data-testid="notify-permission-notice">
                    {notifyNotice}
                  </p>
                )}
              </div>

              <div className={`border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-3 ${notificationsEnabled ? '' : 'opacity-50 pointer-events-none'}`}>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Private messages</span>
                  <Toggle checked={notifyPrivateMessages} onChange={setNotifyPrivateMessages} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Mentions</span>
                  <Toggle checked={notifyMentions} onChange={setNotifyMentions} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Connection lost</span>
                  <Toggle checked={notifyConnectionLost} onChange={setNotifyConnectionLost} />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Only when window is unfocused</span>
                  <Toggle checked={notifyOnlyWhenUnfocused} onChange={setNotifyOnlyWhenUnfocused} />
                </div>
              </div>

              {/* Invite settings */}
              <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-4">
                <div className="text-sm font-semibold">Invites</div>

                <div>
                  <label className="flex items-center justify-between gap-4">
                    <span className="text-sm font-medium">Notify me about invites from</span>
                    <select
                      value={inviteAttentionLevel}
                      onChange={(e) => setInviteAttentionLevel(e.target.value as InviteAttentionLevel)}
                      className="border border-border rounded px-2 py-1 bg-background text-sm"
                      data-testid="invite-attention-level-select"
                    >
                      <option value="trusted">Trusted senders only (default)</option>
                      <option value="quiet">Quiet (no notifications)</option>
                      <option value="all">All invites</option>
                    </select>
                  </label>
                  <p className="text-xs text-muted-foreground mt-2">
                    Controls when receiving a channel invite fires a desktop notification. "Trusted" means the sender is on your buddy (MONITOR) list.
                  </p>
                </div>

                <div>
                  <label className="flex items-center justify-between gap-4">
                    <span className="text-sm font-medium">Keep invites for (hours)</span>
                    <input
                      type="number"
                      min={1}
                      value={inviteTtlHours}
                      onChange={(e) => setInviteTtlHoursState(Math.max(1, parseInt(e.target.value, 10) || 1))}
                      onBlur={(e) => setInviteTtlHours(parseInt(e.target.value, 10) || 1)}
                      className="w-20 px-2 py-1 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)]"
                      style={{ transition: 'var(--transition-base)' }}
                      data-testid="invite-ttl-hours-input"
                    />
                  </label>
                  <p className="text-xs text-muted-foreground mt-2">
                    Received invites are discarded after this many hours. Minimum 1 hour. Default is 24.
                  </p>
                </div>
              </div>
            </div>
          </div>
        );
      case 'advanced':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">Advanced</h3>
            <div className="border border-border rounded-lg p-4 bg-card/50 shadow-[var(--shadow-sm)] space-y-4">
              <div>
                <div className="text-sm font-semibold">Diagnostic logging</div>
                <p className="text-xs text-muted-foreground mt-1">
                  Write a plain-text log file for troubleshooting. Useful when reporting a bug — turn it on, reproduce the issue, then share the file. Rotates at 10&nbsp;MB (keeps 3 compressed backups).
                </p>
              </div>

              {/* Enable toggle */}
              <div className="flex items-center justify-between gap-4">
                <span className="text-sm font-medium">Log to file</span>
                <Toggle
                  checked={logConfig?.enabled ?? false}
                  onChange={(v) => {
                    if (!logConfig) return;
                    void saveLogConfig(main.LogConfig.createFrom({ ...logConfig, path: logPathDraft || logConfig.path, enabled: v }));
                  }}
                />
              </div>

              {/* Path */}
              <div>
                <label className="block text-sm font-medium mb-1.5">Log file path</label>
                <input
                  type="text"
                  value={logPathDraft}
                  placeholder={defaultLogPath}
                  onChange={(e) => setLogPathDraft(e.target.value)}
                  onBlur={() => {
                    if (!logConfig) return;
                    if (logPathDraft.trim() === logConfig.path) return;
                    void saveLogConfig(main.LogConfig.createFrom({ ...logConfig, path: logPathDraft.trim() }));
                  }}
                  className="w-full px-3 py-2 text-sm border border-border rounded-lg bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all shadow-[var(--shadow-sm)] focus:shadow-[var(--shadow-md)] font-mono"
                  style={{ transition: 'var(--transition-base)' }}
                  data-testid="log-path-input"
                />
                <p className="text-xs text-muted-foreground mt-1">
                  Leave blank to use the default location. A leading <code>~</code> expands to your home directory.
                </p>
              </div>

              {/* Level */}
              <div>
                <label className="block text-sm font-medium mb-1.5">Log level</label>
                <Select
                  value={logConfig?.level ?? 'info'}
                  onValueChange={(value) => {
                    if (!logConfig) return;
                    void saveLogConfig(main.LogConfig.createFrom({ ...logConfig, path: logPathDraft || logConfig.path, level: value }));
                  }}
                >
                  <SelectTrigger className="w-full" data-testid="log-level-select">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="debug">Debug (most verbose)</SelectItem>
                    <SelectItem value="info">Info (default)</SelectItem>
                    <SelectItem value="warn">Warn</SelectItem>
                    <SelectItem value="error">Error (least verbose)</SelectItem>
                  </SelectContent>
                </Select>
                <p className="text-xs text-muted-foreground mt-1">
                  Debug captures connection, CAP/SASL, and channel-roster detail — the level to use when reproducing a bug. The level also applies to the on-screen Server log.
                </p>
              </div>

              {logSaving && <p className="text-xs text-muted-foreground">Applying…</p>}
              {logError && (
                <p className="text-xs text-destructive" data-testid="log-config-error">
                  Couldn’t apply: {logError}
                </p>
              )}
            </div>
          </div>
        );
      case 'about':
        return (
          <div className="mb-6">
            <h3 className="text-md font-semibold mb-4">About</h3>
            <div className="border border-border rounded-lg p-5 bg-card/50 shadow-[var(--shadow-sm)] space-y-4">
              <div>
                <div className="text-lg font-semibold">Cascade Chat</div>
                <div className="text-2xl font-bold mt-1" data-testid="about-version">
                  {buildInfo?.version ?? '—'}
                </div>
              </div>
              <dl className="space-y-1 text-sm">
                <div className="flex gap-2">
                  <dt className="text-muted-foreground w-16">Commit</dt>
                  <dd className="font-mono" data-testid="about-commit">{buildInfo?.commit ?? '—'}</dd>
                </div>
                <div className="flex gap-2">
                  <dt className="text-muted-foreground w-16">Built</dt>
                  <dd className="font-mono" data-testid="about-build-date">{buildInfo?.buildDate ?? '—'}</dd>
                </div>
              </dl>
              <div className="pt-3 border-t border-border">
                <button
                  type="button"
                  onClick={() => { void CheckForUpdates(); }}
                  data-testid="check-for-updates-button"
                  className="px-4 py-2 text-sm border border-border rounded-lg hover:bg-accent transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)]"
                  style={{ transition: 'var(--transition-base)' }}
                >
                  Check for Updates…
                </button>
                <p className="text-xs text-muted-foreground mt-2">
                  Checks GitHub for a newer release and walks you through installing it. Available in installed release builds.
                </p>
                {updateNotice && (
                  <p className="text-xs text-amber-500 mt-2" data-testid="update-notice">
                    {updateNotice}
                  </p>
                )}
              </div>
              <div className="pt-3 border-t border-border">
                <label className="flex items-center justify-between gap-4">
                  <span className="text-sm font-medium">Update channel</span>
                  <select
                    value={updateChannel}
                    onChange={(e) => setUpdateChannel(e.target.value as UpdateChannel)}
                    data-testid="update-channel-select"
                    className="border border-border rounded px-2 py-1 bg-background text-sm"
                  >
                    <option value="stable">Stable</option>
                    <option value="prerelease">Prerelease</option>
                  </select>
                </label>
                <p className="text-xs text-muted-foreground mt-2">
                  Stable installs published releases only. Prerelease also picks up the test builds auto-published from <span className="font-mono">main</span> on every merge. Changes take effect on the next update check.
                </p>
              </div>
              <div className="pt-3 border-t border-border space-y-1 text-sm">
                <a
                  href="https://matt0x6f.github.io/irc-client/"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block text-primary underline hover:text-primary/80"
                >
                  Documentation
                </a>
                <a
                  href="https://github.com/matt0x6F/irc-client"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block text-primary underline hover:text-primary/80"
                >
                  View on GitHub
                </a>
                <a
                  href="https://web.libera.chat/#cascade-irc"
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block text-primary underline hover:text-primary/80"
                >
                  Community chat: #cascade-irc on Libera
                </a>
                <p className="text-xs text-muted-foreground">BSD 3-Clause License</p>
              </div>
            </div>
          </div>
        );
      default:
        return null;
    }
  };

  const navItems: { id: SettingsSection; label: string }[] = [
    { id: 'networks', label: 'Networks' },
    { id: 'plugins', label: 'Plugins' },
    { id: 'scripts', label: 'Scripts' },
    { id: 'display', label: 'Display' },
    { id: 'notifications', label: 'Notifications' },
    { id: 'advanced', label: 'Advanced' },
    { id: 'about', label: 'About' },
  ];

  return (
    <div className="flex-1 flex min-h-0 overflow-hidden">
      {/* Left Sidebar Navigation */}
      <div className="w-48 border-r border-border flex-shrink-0 bg-card/30" style={{ backgroundColor: 'var(--card)' }}>
        <nav className="p-2">
          {navItems.map((item) => (
            <button
              key={item.id}
              onClick={() => onSectionChange(item.id)}
              className={`w-full text-left px-3 py-2.5 rounded-md text-sm transition-all ${
                section === item.id
                  ? 'cc-active-pane text-foreground font-medium'
                  : 'hover:bg-accent/70 text-foreground'
              }`}
              style={{ transition: 'var(--transition-base)' }}
            >
              {item.label}
            </button>
          ))}
        </nav>
      </div>

      {/* Right Content Area */}
      <div className="flex-1 overflow-y-auto p-4">
        {renderContent()}
      </div>
    </div>
  );
}

