import { createSignal } from 'solid-js';
import { DevicesAPI } from '@/api/devices';

export type DeviceAccountType = 'ping' | 'unifi' | 'snmp' | 'agent';
export type DeviceStatus = 'online' | 'warning' | 'offline' | 'unknown';

export interface DeviceAccount {
  id: string;
  type: DeviceAccountType;
  name: string;
  enabled: boolean;
  intervalSeconds: number;
  host?: string;
  apiProfile?: string;
  apiKey?: string;
  apiKeyHint?: string;
  siteFilter?: string;
  snmpVersion?: 'v2c' | 'v3';
  communityHint?: string;
  credential?: string;
  username?: string;
  authProtocol?: 'none' | 'md5' | 'sha';
  privacyProtocol?: 'none' | 'des' | 'aes';
  timeoutMs?: number;
  retries?: number;
  notes?: string;
  createdAt: string;
  lastCheckedAt?: string;
  lastError?: string;
  installCommand?: string;
}

export interface DeviceInventoryItem {
  id: string;
  accountId: string;
  accountType: DeviceAccountType;
  name: string;
  host: string;
  type: 'switch' | 'router' | 'gateway' | 'modem' | 'access_point' | 'controller' | 'other';
  vendor?: string;
  model?: string;
  site?: string;
  status: DeviceStatus;
  latencyMs?: number;
  packetLoss?: number;
  uptime?: string;
  uptimeSeconds?: number;
  advanced?: DeviceAdvancedMetrics;
  firmwareVersion?: string;
  lastSeen?: string;
  lastCheckedAt?: string;
  notes?: string;
  raw?: Record<string, unknown>;
}

export interface DeviceAdvancedMetrics {
  cpuPercent?: number;
  memoryPercent?: number;
  diskPercent?: number;
  wanRxBps?: number;
  wanTxBps?: number;
  ethThroughputBps?: Record<string, { rx?: number; tx?: number }>;
  securityScore?: number;
  securityChecks?: Array<{ id: string; label: string; passed: boolean; detail?: string }>;
  os?: string;
  kernel?: string;
  hostname?: string;
  collectedAt?: string;
}

export interface DeviceAlertSettings {
  enabled: boolean;
  offlineEnabled: boolean;
  latencyEnabled: boolean;
  latencyWarnMs: number;
  packetLossEnabled: boolean;
  packetLossWarnPct: number;
  uptimeEnabled: boolean;
  uptimeMinSeconds: number;
  checkOverrides?: Record<string, boolean>;
  deviceOverrides?: Record<string, boolean>;
  deviceRules?: Record<string, DeviceAlertRule>;
  advancedEnabled: boolean;
  advancedCpuWarnPct: number;
  advancedMemoryWarnPct: number;
  advancedDiskWarnPct: number;
  advancedSecurityMin: number;
  lastEvaluatedAt?: string;
  lastEvaluationSummary?: Record<string, number>;
}

export interface DeviceAlertRule {
  offlineEnabled?: boolean;
  uptimeEnabled?: boolean;
  uptimeMinSeconds?: number;
  latencyEnabled?: boolean;
  latencyWarnMs?: number;
  packetLossEnabled?: boolean;
  packetLossWarnPct?: number;
}

const ACCOUNTS_KEY = 'pulse.devices.accounts.v1';
const DEVICES_KEY = 'pulse.devices.inventory.v1';

const nowIso = () => new Date().toISOString();

const defaultPingAccount = (): DeviceAccount => ({
  id: 'account-ping-default',
  type: 'ping',
  name: 'Default Ping',
  enabled: true,
  intervalSeconds: 30,
  timeoutMs: 1500,
  retries: 2,
  notes: 'Baseline reachability check used when no API or SNMP source is needed.',
  createdAt: nowIso(),
});

const defaultAlerts = (): DeviceAlertSettings => ({
  enabled: true,
  offlineEnabled: true,
  latencyEnabled: true,
  latencyWarnMs: 150,
  packetLossEnabled: true,
  packetLossWarnPct: 5,
  uptimeEnabled: false,
  uptimeMinSeconds: 300,
  checkOverrides: {},
  deviceOverrides: {},
  deviceRules: {},
  advancedEnabled: true,
  advancedCpuWarnPct: 85,
  advancedMemoryWarnPct: 90,
  advancedDiskWarnPct: 90,
  advancedSecurityMin: 70,
});

const [accounts, setAccounts] = createSignal<DeviceAccount[]>([defaultPingAccount()]);
const [devices, setDevices] = createSignal<DeviceInventoryItem[]>([]);
const [alerts, setAlerts] = createSignal<DeviceAlertSettings>(defaultAlerts());
const [loading, setLoading] = createSignal(false);

const readLegacy = <T,>(key: string, fallback: T): T => {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
};

const applyState = (state: {
  checks?: DeviceAccount[];
  devices?: DeviceInventoryItem[];
  alerts?: DeviceAlertSettings;
}) => {
  setAccounts(state.checks?.length ? state.checks : [defaultPingAccount()]);
  setDevices(state.devices ?? []);
  setAlerts({ ...defaultAlerts(), ...(state.alerts ?? {}) });
};

const migrateLegacyIfNeeded = async () => {
  const legacyChecks = readLegacy<DeviceAccount[]>(ACCOUNTS_KEY, []);
  const legacyDevices = readLegacy<DeviceInventoryItem[]>(DEVICES_KEY, []);
  if (legacyChecks.length === 0 && legacyDevices.length === 0) return;
  if (accounts().length > 1 || devices().length > 0) return;

  const savedChecks = new Map<string, DeviceAccount>();
  for (const check of legacyChecks) {
    const saved = check.id === 'account-ping-default'
      ? await DevicesAPI.updateCheck(check.id, check)
      : await DevicesAPI.createCheck(check);
    savedChecks.set(check.id, saved);
  }
  for (const device of legacyDevices) {
    const mappedCheck = savedChecks.get(device.accountId);
    await DevicesAPI.createDevice({
      ...device,
      accountId: mappedCheck?.id ?? device.accountId,
      accountType: mappedCheck?.type ?? device.accountType,
    });
  }
  const refreshed = await DevicesAPI.getState();
  applyState(refreshed);
};

export const devicesMonitoringStore = {
  accounts,
  devices,
  alerts,
  loading,
  async initialize() {
    setLoading(true);
    try {
      const state = await DevicesAPI.getState();
      applyState(state);
      await migrateLegacyIfNeeded();
    } finally {
      setLoading(false);
    }
  },
  async addAccount(input: Partial<DeviceAccount> & { type: DeviceAccountType }) {
    const account = await DevicesAPI.createCheck(input);
    setAccounts([...accounts().filter((item) => item.id !== account.id), account]);
    return account;
  },
  async updateAccount(id: string, patch: Partial<DeviceAccount>) {
    const existing = accounts().find((account) => account.id === id);
    const account = await DevicesAPI.updateCheck(id, { ...existing, ...patch, id });
    setAccounts(accounts().map((item) => (item.id === id ? account : item)));
  },
  async removeAccount(id: string) {
    await DevicesAPI.deleteCheck(id);
    setAccounts(accounts().filter((account) => account.id !== id));
    setDevices(devices().filter((device) => device.accountId !== id));
  },
  async addDevice(input: Omit<DeviceInventoryItem, 'id' | 'status' | 'lastSeen'> & { status?: DeviceStatus }) {
    const item = await DevicesAPI.createDevice(input);
    setDevices([...devices().filter((device) => device.id !== item.id), item]);
    return item;
  },
  async updateDevice(id: string, patch: Partial<DeviceInventoryItem>) {
    const existing = devices().find((device) => device.id === id);
    const item = await DevicesAPI.updateDevice(id, { ...existing, ...patch, id });
    setDevices(devices().map((device) => (device.id === id ? item : device)));
  },
  async removeDevice(id: string) {
    await DevicesAPI.deleteDevice(id);
    setDevices(devices().filter((device) => device.id !== id));
  },
  async updateAlerts(patch: Partial<DeviceAlertSettings>) {
    const next = { ...alerts(), ...patch };
    const saved = await DevicesAPI.updateAlerts(next);
    setAlerts({ ...defaultAlerts(), ...saved });
  },
  async pollDueDevices(force = false) {
    if (!force) {
      const now = Date.now();
      const due = devices().some((device) => {
        const check = accounts().find((item) => item.id === device.accountId);
        if (!check?.enabled) return false;
        const last = device.lastCheckedAt ? new Date(device.lastCheckedAt).getTime() : 0;
        return now - last >= check.intervalSeconds * 1000;
      });
      if (!due) return;
    }
    const state = await DevicesAPI.pollNow();
    applyState(state);
  },
};
