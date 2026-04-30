import { createSignal } from 'solid-js';

export type DeviceAccountType = 'ping' | 'unifi' | 'snmp';
export type DeviceStatus = 'online' | 'warning' | 'offline' | 'unknown';

export interface DeviceAccount {
  id: string;
  type: DeviceAccountType;
  name: string;
  enabled: boolean;
  intervalSeconds: number;
  host?: string;
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
  cpuUsage?: number;
  memoryUsage?: number;
  latencyMs?: number;
  packetLoss?: number;
  uptime?: string;
  firmwareVersion?: string;
  lastSeen?: string;
  lastCheckedAt?: string;
  notes?: string;
}

const ACCOUNTS_KEY = 'pulse.devices.accounts.v1';
const DEVICES_KEY = 'pulse.devices.inventory.v1';

const nowIso = () => new Date().toISOString();

const makeId = (prefix: string) => {
  if (typeof crypto !== 'undefined' && 'randomUUID' in crypto) {
    return `${prefix}-${crypto.randomUUID()}`;
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
};

const safeParse = <T,>(key: string, fallback: T): T => {
  if (typeof window === 'undefined') return fallback;
  try {
    const raw = window.localStorage.getItem(key);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw) as T;
    return parsed ?? fallback;
  } catch {
    return fallback;
  }
};

const persist = <T,>(key: string, value: T) => {
  if (typeof window === 'undefined') return;
  window.localStorage.setItem(key, JSON.stringify(value));
};

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

const loadAccounts = () => {
  const stored = safeParse<DeviceAccount[]>(ACCOUNTS_KEY, []);
  if (stored.length > 0) return stored;
  return [defaultPingAccount()];
};

const [accounts, setAccounts] = createSignal<DeviceAccount[]>(loadAccounts());
const [devices, setDevices] = createSignal<DeviceInventoryItem[]>(
  safeParse<DeviceInventoryItem[]>(DEVICES_KEY, []),
);

const saveAccounts = (next: DeviceAccount[]) => {
  setAccounts(next);
  persist(ACCOUNTS_KEY, next);
};

const saveDevices = (next: DeviceInventoryItem[]) => {
  setDevices(next);
  persist(DEVICES_KEY, next);
};

const accountDefaults = (type: DeviceAccountType): Omit<DeviceAccount, 'id' | 'type' | 'createdAt'> => {
  if (type === 'unifi') {
    return {
      name: 'UniFi Site Manager',
      enabled: true,
      intervalSeconds: 60,
      host: 'https://api.ui.com',
      siteFilter: '',
      apiKeyHint: '',
    };
  }
  if (type === 'snmp') {
    return {
      name: 'SNMP v2c',
      enabled: true,
      intervalSeconds: 60,
      snmpVersion: 'v2c',
      communityHint: '',
      timeoutMs: 2000,
      retries: 2,
    };
  }
  return {
    name: 'Ping',
    enabled: true,
    intervalSeconds: 30,
    timeoutMs: 1500,
    retries: 2,
  };
};

export const devicesMonitoringStore = {
  accounts,
  devices,
  addAccount(input: Partial<DeviceAccount> & { type: DeviceAccountType }) {
    const defaults = accountDefaults(input.type);
    const account: DeviceAccount = {
      ...defaults,
      ...input,
      id: makeId('account'),
      type: input.type,
      createdAt: nowIso(),
    };
    saveAccounts([...accounts(), account]);
    return account;
  },
  updateAccount(id: string, patch: Partial<DeviceAccount>) {
    saveAccounts(accounts().map((account) => (account.id === id ? { ...account, ...patch } : account)));
  },
  removeAccount(id: string) {
    if (id === 'account-ping-default') return;
    saveAccounts(accounts().filter((account) => account.id !== id));
    saveDevices(devices().filter((device) => device.accountId !== id));
  },
  addDevice(input: Omit<DeviceInventoryItem, 'id' | 'status' | 'lastSeen'> & { status?: DeviceStatus }) {
    const account = accounts().find((item) => item.id === input.accountId);
    const item: DeviceInventoryItem = {
      ...input,
      accountType: account?.type ?? input.accountType,
      id: makeId('device'),
      status: input.status ?? 'unknown',
      lastSeen: nowIso(),
      lastCheckedAt: undefined,
    };
    saveDevices([...devices(), item]);
    return item;
  },
  updateDevice(id: string, patch: Partial<DeviceInventoryItem>) {
    saveDevices(devices().map((device) => (device.id === id ? { ...device, ...patch } : device)));
  },
  removeDevice(id: string) {
    saveDevices(devices().filter((device) => device.id !== id));
  },
  simulatePoll(id: string) {
    saveDevices(
      devices().map((device) => {
        if (device.id !== id) return device;
        const account = accounts().find((item) => item.id === device.accountId);
        if (!account?.enabled) return device;
        const latency = 4 + Math.round(Math.random() * (device.accountType === 'ping' ? 24 : 14));
        const warning = Math.random() > 0.92;
        return {
          ...device,
          status: warning ? 'warning' : 'online',
          latencyMs: latency,
          packetLoss: warning ? 1 : 0,
          cpuUsage: device.accountType === 'ping' ? undefined : 8 + Math.round(Math.random() * 42),
          memoryUsage: device.accountType === 'ping' ? undefined : 18 + Math.round(Math.random() * 52),
          uptime: device.uptime || 'just now',
          lastSeen: nowIso(),
          lastCheckedAt: nowIso(),
        };
      }),
    );
  },
  pollDueDevices() {
    const now = Date.now();
    devices().forEach((device) => {
      const account = accounts().find((item) => item.id === device.accountId);
      if (!account?.enabled) return;
      const last = device.lastCheckedAt ? new Date(device.lastCheckedAt).getTime() : 0;
      if (now - last >= account.intervalSeconds * 1000) {
        this.simulatePoll(device.id);
      }
    });
  },
};
