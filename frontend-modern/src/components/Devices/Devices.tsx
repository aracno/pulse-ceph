import { Component, For, Show, createMemo, createSignal, onCleanup, onMount } from 'solid-js';
import { useNavigate } from '@solidjs/router';
import {
  devicesMonitoringStore,
  type DeviceAccountType,
  type DeviceInventoryItem,
} from '@/stores/devicesMonitoring';
import { DevicesAPI } from '@/api/devices';
import { Card } from '@/components/shared/Card';
import { ScrollableTable } from '@/components/shared/ScrollableTable';
import Network from 'lucide-solid/icons/network';
import Plus from 'lucide-solid/icons/plus';
import Router from 'lucide-solid/icons/router';
import ServerCog from 'lucide-solid/icons/server-cog';
import Settings from 'lucide-solid/icons/settings';
import Trash2 from 'lucide-solid/icons/trash-2';
import Wifi from 'lucide-solid/icons/wifi';
import X from 'lucide-solid/icons/x';

const statusClasses: Record<string, string> = {
  online: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300',
  warning: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
  offline: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
  unknown: 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300',
};

const statusDotClasses: Record<string, string> = {
  online: 'bg-green-500',
  warning: 'bg-amber-500',
  offline: 'bg-red-500',
  unknown: 'bg-gray-500',
};

const sourceClasses: Record<DeviceAccountType, string> = {
  ping: 'bg-slate-100 text-slate-700 dark:bg-slate-800 dark:text-slate-200',
  unifi: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300',
  snmp: 'bg-cyan-100 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300',
};

const typeIcon = (type?: string) => {
  switch ((type || '').toLowerCase()) {
    case 'router':
    case 'gateway':
    case 'modem':
      return Router;
    case 'access_point':
      return Wifi;
    case 'controller':
      return ServerCog;
    default:
      return Network;
  }
};

const percentText = (value?: number) =>
  typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value)}%` : '-';

const latencyText = (device: DeviceInventoryItem) => {
  if (typeof device.latencyMs === 'number') return `${device.latencyMs} ms`;
  if (device.accountType === 'ping') return 'pending';
  return '-';
};

const formatLastSeen = (value?: string) => {
  if (!value) return 'Never';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return 'Never';
  return date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
};

type DeviceSortKey = 'name' | 'type' | 'status' | 'latency' | 'source';

const thClass =
  'px-3 py-2 text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400 cursor-pointer select-none';

export const Devices: Component = () => {
  const navigate = useNavigate();
  const [search, setSearch] = createSignal('');
  const [statusFilter, setStatusFilter] = createSignal<'all' | 'online' | 'warning' | 'offline'>('all');
  const [sortKey, setSortKey] = createSignal<DeviceSortKey>('name');
  const [sortDirection, setSortDirection] = createSignal<'asc' | 'desc'>('asc');
  const [wizardOpen, setWizardOpen] = createSignal(false);
  const [wizardStep, setWizardStep] = createSignal<1 | 2>(1);
  const [selectedAccountId, setSelectedAccountId] = createSignal('account-ping-default');
  const [deviceName, setDeviceName] = createSignal('');
  const [deviceHost, setDeviceHost] = createSignal('');
  const [deviceType, setDeviceType] = createSignal<DeviceInventoryItem['type']>('switch');
  const [deviceVendor, setDeviceVendor] = createSignal('');
  const [deviceModel, setDeviceModel] = createSignal('');
  const [deviceSite, setDeviceSite] = createSignal('');
  const [deviceNotes, setDeviceNotes] = createSignal('');
  const [unifiLoading, setUnifiLoading] = createSignal(false);
  const [unifiError, setUnifiError] = createSignal('');
  const [unifiDevices, setUnifiDevices] = createSignal<DeviceInventoryItem[]>([]);
  const [unifiSearch, setUnifiSearch] = createSignal('');

  const accounts = createMemo(() => devicesMonitoringStore.accounts().filter((account) => account.enabled));
  const devices = devicesMonitoringStore.devices;
  const selectedAccount = createMemo(() =>
    devicesMonitoringStore.accounts().find((account) => account.id === selectedAccountId()),
  );
  const filteredUnifiDevices = createMemo(() => {
    const search = unifiSearch().trim().toLowerCase();
    if (!search) return unifiDevices();
    return unifiDevices().filter((device) =>
      [device.name, device.host, device.model, device.site, device.type]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(search)),
    );
  });

  const filteredDevices = createMemo(() => {
    const term = search().trim().toLowerCase();
    const filtered = devices().filter((device) => {
      const statusMatch = statusFilter() === 'all' || device.status === statusFilter();
      if (!statusMatch) return false;
      if (!term) return true;
      return [device.name, device.host, device.vendor, device.model, device.site, device.accountType, device.type]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(term));
    });

    const direction = sortDirection() === 'asc' ? 1 : -1;
    return [...filtered].sort((a, b) => {
      const key = sortKey();
      if (key === 'latency') {
        return ((a.latencyMs ?? Number.MAX_SAFE_INTEGER) - (b.latencyMs ?? Number.MAX_SAFE_INTEGER)) * direction;
      }
      const left =
        key === 'name' ? a.name :
          key === 'type' ? a.type :
            key === 'status' ? a.status :
              a.accountType;
      const right =
        key === 'name' ? b.name :
          key === 'type' ? b.type :
            key === 'status' ? b.status :
              b.accountType;
      return left.localeCompare(right) * direction;
    });
  });

  const handleSort = (key: DeviceSortKey) => {
    if (sortKey() === key) {
      setSortDirection(sortDirection() === 'asc' ? 'desc' : 'asc');
    } else {
      setSortKey(key);
      setSortDirection('asc');
    }
  };

  const sortIndicator = (key: DeviceSortKey) => sortKey() === key ? (sortDirection() === 'asc' ? '▲' : '▼') : '';

  const resetWizard = () => {
    setWizardStep(1);
    setSelectedAccountId(accounts()[0]?.id || 'account-ping-default');
    setDeviceName('');
    setDeviceHost('');
    setDeviceType('switch');
    setDeviceVendor('');
    setDeviceModel('');
    setDeviceSite('');
    setDeviceNotes('');
    setUnifiLoading(false);
    setUnifiError('');
    setUnifiDevices([]);
    setUnifiSearch('');
  };

  const openWizard = () => {
    resetWizard();
    setWizardOpen(true);
  };

  const addDevice = async () => {
    const account = selectedAccount();
    if (!account || !deviceName().trim() || !deviceHost().trim()) return;
    await devicesMonitoringStore.addDevice({
      accountId: account.id,
      accountType: account.type,
      name: deviceName().trim(),
      host: deviceHost().trim(),
      type: deviceType(),
      vendor: deviceVendor().trim() || (account.type === 'unifi' ? 'Ubiquiti' : undefined),
      model: deviceModel().trim() || undefined,
      site: deviceSite().trim() || account.siteFilter || undefined,
      notes: deviceNotes().trim() || undefined,
    });
    void devicesMonitoringStore.pollDueDevices(true);
    setWizardOpen(false);
  };

  const fetchUnifiDevices = async (accountId = selectedAccountId()) => {
    const account = devicesMonitoringStore.accounts().find((item) => item.id === accountId);
    if (!account || account.type !== 'unifi') return [];
    setUnifiLoading(true);
    setUnifiError('');
    try {
      const payload = await DevicesAPI.discoverUniFi({
        checkId: account.id,
        baseUrl: account.host || 'https://api.ui.com',
      });
      const devices = payload.devices || [];
      setUnifiDevices(devices);
      if (devices.length === 0) {
        setUnifiError('The UniFi API answered, but no devices were returned for this key.');
      }
      return devices;
    } catch (error) {
      const message =
        error instanceof Error
          ? error.message
          : 'Unable to query UniFi Site Manager API through Pulse backend.';
      setUnifiError(message);
      return [];
    } finally {
      setUnifiLoading(false);
    }
  };

  const selectUnifiDevice = (device: DeviceInventoryItem) => {
    setDeviceName(device.name);
    setDeviceHost(device.host || device.id);
    setDeviceType(device.type);
    setDeviceVendor(device.vendor || '');
    setDeviceModel(device.model || '');
    setDeviceSite(device.site || '');
    setDeviceNotes(device.firmwareVersion ? `Firmware ${device.firmwareVersion}` : '');
  };

  onMount(() => {
    void devicesMonitoringStore.initialize().then(() => devicesMonitoringStore.pollDueDevices(true));
    const interval = window.setInterval(() => {
      void devicesMonitoringStore.pollDueDevices();
    }, 5000);
    onCleanup(() => window.clearInterval(interval));
  });

  return (
    <div class="space-y-3">
      <Card padding="sm" class="mb-3">
        <div class="flex flex-col gap-3">
          <div class="relative">
            <input
              type="text"
              placeholder="Search devices by name, address, vendor, model, site, or source..."
              value={search()}
              onInput={(event) => setSearch(event.currentTarget.value)}
              class="w-full rounded-lg border border-gray-300 bg-white py-1.5 pl-9 pr-10 text-sm text-gray-800 outline-none transition-all placeholder:text-gray-400 focus:border-blue-500 focus:ring-2 focus:ring-blue-500/20 dark:border-gray-600 dark:bg-gray-900 dark:text-gray-200 dark:placeholder:text-gray-500 dark:focus:border-blue-400"
            />
            <svg
              class="absolute left-3 top-2 h-4 w-4 text-gray-400 dark:text-gray-500"
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
            >
              <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
            </svg>
            <Show when={search()}>
              <button
                type="button"
                class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 transition-colors hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
                onClick={() => setSearch('')}
                aria-label="Clear search"
                title="Clear search"
              >
                <X class="h-4 w-4" strokeWidth={2} />
              </button>
            </Show>
          </div>

          <div class="flex flex-wrap items-center justify-between gap-2">
            <div class="flex flex-wrap items-center gap-1">
              <FilterButton label="All" active={statusFilter() === 'all'} onClick={() => setStatusFilter('all')} />
              <FilterButton label="Online" active={statusFilter() === 'online'} onClick={() => setStatusFilter('online')} />
              <FilterButton label="Degraded" active={statusFilter() === 'warning'} onClick={() => setStatusFilter('warning')} />
              <FilterButton label="Offline" active={statusFilter() === 'offline'} onClick={() => setStatusFilter('offline')} />
            </div>
            <div class="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={openWizard}
                class="inline-flex h-8 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-xs font-medium text-white transition-colors hover:bg-blue-700"
          >
            <Plus class="h-4 w-4" strokeWidth={2} />
            Add device
          </button>
          <button
            type="button"
            onClick={() => navigate('/settings/devices')}
                class="inline-flex h-8 items-center justify-center gap-2 rounded-md border border-gray-300 bg-white px-3 text-xs font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
          >
            <Settings class="h-4 w-4" strokeWidth={2} />
            Checks
          </button>
            </div>
          </div>
        </div>
      </Card>

      <Show when={filteredDevices().length > 0} fallback={<Card padding="lg"><EmptyInventory onAdd={openWizard} /></Card>}>
        <Card padding="none" tone="glass" class="overflow-hidden">
          <ScrollableTable persistKey="devices-overview" minWidth="1000px" mobileMinWidth="1000px">
            <table class="w-full border-collapse whitespace-nowrap" style={{ 'table-layout': 'fixed', 'min-width': '1000px' }}>
              <thead>
                <tr class="border-b border-gray-200 bg-gray-50 text-gray-600 dark:border-gray-700 dark:bg-gray-700/50 dark:text-gray-300">
                  <th class={`${thClass} text-left pl-4`} style={{ width: '230px' }} onClick={() => handleSort('name')}>
                    Device {sortIndicator('name')}
                  </th>
                  <th class={thClass} style={{ width: '120px' }} onClick={() => handleSort('type')}>Type {sortIndicator('type')}</th>
                  <th class={thClass} style={{ width: '120px' }} onClick={() => handleSort('source')}>Source {sortIndicator('source')}</th>
                  <th class={thClass} style={{ width: '120px' }} onClick={() => handleSort('status')}>Status {sortIndicator('status')}</th>
                  <th class={thClass} style={{ width: '170px' }}>Address</th>
                  <th class={thClass} style={{ width: '160px' }}>Model</th>
                  <th class={thClass} style={{ width: '120px' }}>Site</th>
                  <th class={thClass} style={{ width: '110px' }} onClick={() => handleSort('latency')}>Latency {sortIndicator('latency')}</th>
                  <th class={thClass} style={{ width: '100px' }}>Loss</th>
                  <th class={thClass} style={{ width: '90px' }}>CPU</th>
                  <th class={thClass} style={{ width: '110px' }}>Memory</th>
                  <th class={thClass} style={{ width: '120px' }}>Last seen</th>
                  <th class={thClass} style={{ width: '100px' }}></th>
                </tr>
              </thead>
              <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
                <For each={filteredDevices()}>
                  {(device) => (
                    <DeviceRow device={device} />
                  )}
                </For>
              </tbody>
            </table>
          </ScrollableTable>
        </Card>
      </Show>


      <Show when={wizardOpen()}>
        <div class="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4">
          <div class="w-full max-w-2xl rounded-lg border border-gray-200 bg-white shadow-xl dark:border-gray-700 dark:bg-gray-800">
            <div class="flex items-center justify-between border-b border-gray-200 px-5 py-4 dark:border-gray-700">
              <div>
                <h2 class="text-lg font-semibold text-gray-900 dark:text-gray-100">Add device</h2>
                <p class="text-sm text-gray-500 dark:text-gray-400">
                  Step {wizardStep()} of 2
                </p>
              </div>
              <button
                type="button"
                onClick={() => setWizardOpen(false)}
                class="rounded p-1.5 text-gray-500 transition-colors hover:bg-gray-100 dark:hover:bg-gray-700"
              >
                <X class="h-5 w-5" strokeWidth={2} />
              </button>
            </div>

            <div class="p-5">
              <Show when={wizardStep() === 1}>
                <div class="space-y-4">
                  <div>
                    <h3 class="text-sm font-semibold text-gray-900 dark:text-gray-100">Select monitoring check</h3>
                    <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
                      Checks come from Settings - Platforms - Devices.
                    </p>
                  </div>
                  <div class="grid gap-3">
                    <For each={accounts()}>
                      {(account) => (
                        <button
                          type="button"
                          onClick={() => setSelectedAccountId(account.id)}
                          class={`rounded-lg border p-4 text-left transition-colors ${
                            selectedAccountId() === account.id
                              ? 'border-blue-500 bg-blue-50 dark:bg-blue-900/20'
                              : 'border-gray-200 hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-900/40'
                          }`}
                        >
                          <div class="flex items-center justify-between gap-3">
                            <div>
                              <div class="font-medium text-gray-900 dark:text-gray-100">{account.name}</div>
                              <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                                {account.type.toUpperCase()} - every {account.intervalSeconds}s
                                {account.host ? ` - ${account.host}` : ''}
                              </div>
                            </div>
                            <span class={`rounded px-2 py-1 text-xs font-medium ${sourceClasses[account.type]}`}>
                              {account.type.toUpperCase()}
                            </span>
                          </div>
                        </button>
                      )}
                    </For>
                  </div>
                </div>
              </Show>

              <Show when={wizardStep() === 2}>
                <div class="space-y-4">
                  <Show when={selectedAccount()?.type === 'unifi'}>
                    <div class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-900/60 dark:bg-blue-900/20">
                      <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                        <div>
                          <div class="text-sm font-semibold text-blue-900 dark:text-blue-100">UniFi API discovery</div>
                          <div class="mt-1 text-xs text-blue-800 dark:text-blue-200">
                            Pulse queries `GET /v1/devices` with the selected UniFi check and returns discovered devices.
                          </div>
                        </div>
                        <button
                          type="button"
                          onClick={() => void fetchUnifiDevices()}
                          disabled={unifiLoading()}
                          class="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-wait disabled:bg-blue-400"
                        >
                          {unifiLoading() ? 'Querying...' : 'Fetch UniFi devices'}
                        </button>
                      </div>
                      <Show when={unifiError()}>
                        <div class="mt-3 rounded border border-amber-300 bg-amber-50 px-3 py-2 text-xs text-amber-800 dark:border-amber-900/60 dark:bg-amber-900/20 dark:text-amber-200">
                          {unifiError()}
                        </div>
                      </Show>
                      <Show when={unifiDevices().length > 0}>
                        <div class="mt-3 space-y-2">
                          <div class="flex items-center gap-2">
                            <input
                              type="text"
                              value={unifiSearch()}
                              onInput={(event) => setUnifiSearch(event.currentTarget.value)}
                              placeholder="Search UniFi devices..."
                              class="h-9 min-w-0 flex-1 rounded-md border border-blue-200 bg-white px-3 text-sm text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-none focus:ring-2 focus:ring-blue-500/30 dark:border-blue-900/60 dark:bg-gray-900 dark:text-gray-100"
                            />
                            <span class="whitespace-nowrap text-xs text-blue-800 dark:text-blue-200">
                              {filteredUnifiDevices().length} / {unifiDevices().length}
                            </span>
                          </div>
                          <div class="max-h-56 overflow-y-auto rounded border border-blue-200 bg-white dark:border-blue-900/60 dark:bg-gray-900">
                            <For
                              each={filteredUnifiDevices()}
                              fallback={
                                <div class="px-3 py-4 text-sm text-gray-500 dark:text-gray-400">
                                  No UniFi device matches this search.
                                </div>
                              }
                            >
                              {(device) => (
                                <button
                                  type="button"
                                  onClick={() => selectUnifiDevice(device)}
                                  class="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-3 py-2 text-left last:border-b-0 hover:bg-blue-50 dark:border-gray-700 dark:hover:bg-blue-900/20"
                                >
                                  <span>
                                    <span class="block text-sm font-medium text-gray-900 dark:text-gray-100">{device.name}</span>
                                    <span class="block text-xs text-gray-500 dark:text-gray-400">
                                      {[device.model, device.host, device.site].filter(Boolean).join(' - ')}
                                    </span>
                                  </span>
                                  <span class="rounded bg-blue-100 px-2 py-1 text-xs font-medium text-blue-700 dark:bg-blue-900/40 dark:text-blue-300">
                                    Select
                                  </span>
                                </button>
                              )}
                            </For>
                          </div>
                        </div>
                      </Show>
                    </div>
                  </Show>
                  <div class="grid gap-4 sm:grid-cols-2">
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Name</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceName()} onInput={(event) => setDeviceName(event.currentTarget.value)} placeholder="Core switch" />
                  </label>
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Address</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceHost()} onInput={(event) => setDeviceHost(event.currentTarget.value)} placeholder="192.168.1.2 or device ID" />
                  </label>
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Type</span>
                    <select class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceType()} onChange={(event) => setDeviceType(event.currentTarget.value as DeviceInventoryItem['type'])}>
                      <option value="switch">Switch</option>
                      <option value="router">Router</option>
                      <option value="gateway">Gateway</option>
                      <option value="modem">Modem</option>
                      <option value="access_point">Access point</option>
                      <option value="controller">Controller</option>
                      <option value="other">Other</option>
                    </select>
                  </label>
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Vendor</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceVendor()} onInput={(event) => setDeviceVendor(event.currentTarget.value)} placeholder={selectedAccount()?.type === 'unifi' ? 'Ubiquiti' : 'Optional'} />
                  </label>
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Model</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceModel()} onInput={(event) => setDeviceModel(event.currentTarget.value)} placeholder="USW-24, UDM, CRS..." />
                  </label>
                  <label class="flex flex-col gap-1">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Site</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceSite()} onInput={(event) => setDeviceSite(event.currentTarget.value)} placeholder="Homelab" />
                  </label>
                  <label class="flex flex-col gap-1 sm:col-span-2">
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Notes</span>
                    <input class="rounded-md border border-gray-300 bg-white px-3 py-2 text-sm dark:border-gray-600 dark:bg-gray-900" value={deviceNotes()} onInput={(event) => setDeviceNotes(event.currentTarget.value)} placeholder="Rack, room, uplink, provider..." />
                  </label>
                  </div>
                </div>
              </Show>
            </div>

            <div class="flex justify-between border-t border-gray-200 px-5 py-4 dark:border-gray-700">
              <button
                type="button"
                onClick={() => (wizardStep() === 1 ? setWizardOpen(false) : setWizardStep(1))}
                class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-700 dark:text-gray-200 dark:hover:bg-gray-700"
              >
                {wizardStep() === 1 ? 'Cancel' : 'Back'}
              </button>
              <button
                type="button"
                disabled={wizardStep() === 2 && (!deviceName().trim() || !deviceHost().trim())}
                onClick={() => (wizardStep() === 1 ? setWizardStep(2) : addDevice())}
                class="rounded-md bg-blue-600 px-3 py-2 text-sm font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:bg-gray-400"
              >
                {wizardStep() === 1 ? 'Continue' : 'Add device'}
              </button>
            </div>
          </div>
        </div>
      </Show>
    </div>
  );
};

const FilterButton: Component<{ label: string; active: boolean; onClick: () => void }> = (props) => (
  <button
    type="button"
    onClick={props.onClick}
    class={`rounded-md px-3 py-1.5 text-xs font-medium transition-colors ${props.active
      ? 'bg-gray-700 text-white dark:bg-gray-700 dark:text-gray-100'
      : 'text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-200'
      }`}
  >
    {props.label}
  </button>
);

const DeviceRow: Component<{ device: DeviceInventoryItem }> = (props) => {
  const Icon = typeIcon(props.device.type);
  return (
    <tr class="group hover:bg-gray-50 dark:hover:bg-gray-700/30">
      <td class="px-3 py-2 pl-4">
        <div class="flex min-w-0 items-center gap-2">
          <span class="text-gray-400 dark:text-gray-500">›</span>
          <span class={`h-2 w-2 rounded-full ${statusDotClasses[props.device.status] || statusDotClasses.unknown}`} />
          <Icon class="h-4 w-4 flex-shrink-0 text-gray-400 dark:text-gray-500" strokeWidth={2} />
          <div class="min-w-0">
            <div class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">{props.device.name}</div>
            <div class="truncate text-[11px] text-gray-500 dark:text-gray-400">
              {props.device.vendor || 'Unknown vendor'}
            </div>
          </div>
        </div>
      </td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{props.device.type.replace('_', ' ')}</td>
      <td class="px-3 py-2">
        <span class={`rounded px-2 py-0.5 text-xs font-medium ${sourceClasses[props.device.accountType]}`}>
          {props.device.accountType.toUpperCase()}
        </span>
      </td>
      <td class="px-3 py-2">
        <span class={`rounded px-2 py-0.5 text-xs font-medium ${statusClasses[props.device.status]}`}>
          {props.device.status}
        </span>
      </td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{props.device.host || '-'}</td>
      <td class="truncate px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{props.device.model || '-'}</td>
      <td class="truncate px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{props.device.site || '-'}</td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{latencyText(props.device)}</td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">
        {typeof props.device.packetLoss === 'number' ? `${props.device.packetLoss}%` : '-'}
      </td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{percentText(props.device.cpuUsage)}</td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{percentText(props.device.memoryUsage)}</td>
      <td class="px-3 py-2 text-sm text-gray-700 dark:text-gray-300">{formatLastSeen(props.device.lastSeen)}</td>
      <td class="px-3 py-2">
        <div class="flex items-center justify-end gap-1 opacity-80 group-hover:opacity-100">
          <button
            type="button"
            onClick={() => devicesMonitoringStore.pollDueDevices(true)}
            class="rounded px-2 py-1 text-xs font-medium text-blue-600 transition-colors hover:bg-blue-50 dark:text-blue-300 dark:hover:bg-blue-900/20"
          >
            Check
          </button>
          <button
            type="button"
            onClick={() => void devicesMonitoringStore.removeDevice(props.device.id)}
            class="rounded p-1.5 text-red-600 transition-colors hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-900/20"
            title="Remove device"
          >
            <Trash2 class="h-4 w-4" strokeWidth={2} />
          </button>
        </div>
      </td>
    </tr>
  );
};

const EmptyInventory: Component<{ onAdd: () => void }> = (props) => (
  <div class="px-6 py-12 text-center">
    <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-lg bg-gray-100 text-gray-500 dark:bg-gray-900 dark:text-gray-400">
      <Network class="h-6 w-6" strokeWidth={2} />
    </div>
    <h3 class="mt-4 text-sm font-semibold text-gray-900 dark:text-gray-100">No network devices yet</h3>
    <p class="mx-auto mt-2 max-w-xl text-sm text-gray-500 dark:text-gray-400">
      Add a device and attach it to a Ping, UniFi, or SNMP check.
    </p>
    <button
      type="button"
      onClick={props.onAdd}
      class="mt-4 inline-flex h-9 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-medium text-white transition-colors hover:bg-blue-700"
    >
      <Plus class="h-4 w-4" strokeWidth={2} />
      Add device
    </button>
  </div>
);
