import { Component, For, JSX, Show, createMemo, createSignal, onCleanup, onMount } from 'solid-js';
import { useNavigate } from '@solidjs/router';
import {
  devicesMonitoringStore,
  type DeviceAccountType,
  type DeviceInventoryItem,
} from '@/stores/devicesMonitoring';
import { DevicesAPI } from '@/api/devices';
import Activity from 'lucide-solid/icons/activity';
import AlertTriangle from 'lucide-solid/icons/alert-triangle';
import CheckCircle2 from 'lucide-solid/icons/check-circle-2';
import Globe2 from 'lucide-solid/icons/globe-2';
import Network from 'lucide-solid/icons/network';
import Plus from 'lucide-solid/icons/plus';
import Router from 'lucide-solid/icons/router';
import ServerCog from 'lucide-solid/icons/server-cog';
import Settings from 'lucide-solid/icons/settings';
import ShieldCheck from 'lucide-solid/icons/shield-check';
import Trash2 from 'lucide-solid/icons/trash-2';
import Wifi from 'lucide-solid/icons/wifi';
import X from 'lucide-solid/icons/x';

const statusClasses: Record<string, string> = {
  online: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300',
  warning: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
  offline: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
  unknown: 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300',
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

export const Devices: Component = () => {
  const navigate = useNavigate();
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
  const onlineCount = createMemo(() => devices().filter((device) => device.status === 'online').length);
  const warningCount = createMemo(() => devices().filter((device) => device.status === 'warning').length);
  const offlineCount = createMemo(() => devices().filter((device) => device.status === 'offline').length);
  const managedCount = createMemo(() => devices().length);
  const filteredUnifiDevices = createMemo(() => {
    const search = unifiSearch().trim().toLowerCase();
    if (!search) return unifiDevices();
    return unifiDevices().filter((device) =>
      [device.name, device.host, device.model, device.site, device.type]
        .filter(Boolean)
        .some((value) => value?.toLowerCase().includes(search)),
    );
  });

  const sourceSummary = createMemo(() => {
    const sources = devices().reduce<Record<string, number>>((acc, device) => {
      acc[device.accountType] = (acc[device.accountType] || 0) + 1;
      return acc;
    }, {});
    return Object.entries(sources).map(([source, count]) => ({ source, count }));
  });

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
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">
            <Network class="h-5 w-5" strokeWidth={2} />
          </div>
          <div>
            <h1 class="text-2xl font-semibold tracking-normal text-gray-900 dark:text-gray-100">Devices</h1>
            <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
              Network hardware health for switches, routers, modems, gateways, and UniFi equipment.
            </p>
          </div>
        </div>
        <div class="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={openWizard}
            class="inline-flex h-9 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-medium text-white transition-colors hover:bg-blue-700"
          >
            <Plus class="h-4 w-4" strokeWidth={2} />
            Add device
          </button>
          <button
            type="button"
            onClick={() => navigate('/settings/devices')}
            class="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-gray-300 bg-white px-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
          >
            <Settings class="h-4 w-4" strokeWidth={2} />
            Checks
          </button>
        </div>
      </div>

      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <SummaryCard label="Managed" value={managedCount()} detail="Devices in inventory" icon={ShieldCheck} tone="blue" />
        <SummaryCard label="Online" value={onlineCount()} detail="Last check succeeded" icon={CheckCircle2} tone="green" />
        <SummaryCard label="Warnings" value={warningCount()} detail="Metrics or state need attention" icon={AlertTriangle} tone="amber" />
        <SummaryCard label="Offline" value={offlineCount()} detail="Unreachable or missing heartbeat" icon={Activity} tone="red" />
      </div>

      <div class="grid gap-4 xl:grid-cols-[1fr_360px]">
        <div class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
          <div class="border-b border-gray-200 px-4 py-3 dark:border-gray-700">
            <div class="flex items-center justify-between gap-3">
              <div>
                <h2 class="text-sm font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Inventory</h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Devices added through the wizard. State updates follow each check frequency.
                </p>
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{devices().length} devices</div>
            </div>
          </div>

          <Show when={devices().length > 0} fallback={<EmptyInventory onAdd={openWizard} />}>
            <div class="divide-y divide-gray-200 dark:divide-gray-700">
              <For each={devices()}>
                {(device) => {
                  const Icon = typeIcon(device.type);
                  return (
                    <div class="grid gap-4 p-4 lg:grid-cols-[1fr_260px_160px] lg:items-center">
                      <div class="flex min-w-0 items-start gap-3">
                        <div class="flex h-10 w-10 items-center justify-center rounded-md bg-gray-100 text-gray-600 dark:bg-gray-900 dark:text-gray-300">
                          <Icon class="h-5 w-5" strokeWidth={2} />
                        </div>
                        <div class="min-w-0">
                          <div class="flex flex-wrap items-center gap-2">
                            <span class="truncate text-sm font-semibold text-gray-900 dark:text-gray-100">
                              {device.name}
                            </span>
                            <span class={`rounded px-2 py-0.5 text-xs font-medium ${statusClasses[device.status]}`}>
                              {device.status}
                            </span>
                            <span class={`rounded px-2 py-0.5 text-xs font-medium ${sourceClasses[device.accountType]}`}>
                              {device.accountType.toUpperCase()}
                            </span>
                          </div>
                          <div class="mt-1 truncate text-xs text-gray-500 dark:text-gray-400">
                            {[device.vendor, device.model, device.host, device.site].filter(Boolean).join(' - ')}
                          </div>
                          <Show when={device.notes}>
                            <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{device.notes}</div>
                          </Show>
                        </div>
                      </div>

                      <div class="grid grid-cols-2 gap-2 text-xs">
                        <Metric label="Latency" value={latencyText(device)} />
                        <Metric label="Loss" value={typeof device.packetLoss === 'number' ? `${device.packetLoss}%` : '-'} />
                        <Metric label="CPU" value={percentText(device.cpuUsage)} />
                        <Metric label="Memory" value={percentText(device.memoryUsage)} />
                      </div>

                      <div class="flex items-center justify-between gap-2 lg:justify-end">
                        <div class="text-xs text-gray-500 dark:text-gray-400">Seen {formatLastSeen(device.lastSeen)}</div>
                        <button
                          type="button"
                          onClick={() => devicesMonitoringStore.pollDueDevices(true)}
                          class="rounded px-2 py-1 text-xs font-medium text-blue-600 transition-colors hover:bg-blue-50 dark:text-blue-300 dark:hover:bg-blue-900/20"
                        >
                          Check
                        </button>
                        <button
                          type="button"
                          onClick={() => void devicesMonitoringStore.removeDevice(device.id)}
                          class="rounded p-1.5 text-red-600 transition-colors hover:bg-red-50 dark:text-red-300 dark:hover:bg-red-900/20"
                          title="Remove device"
                        >
                          <Trash2 class="h-4 w-4" strokeWidth={2} />
                        </button>
                      </div>
                    </div>
                  );
                }}
              </For>
            </div>
          </Show>
        </div>

        <div class="space-y-4">
          <SidePanel title="Collection Sources">
            <SourceLine icon={Activity} label="Ping" detail="Reachability, latency, packet loss." />
            <SourceLine icon={Globe2} label="UniFi" detail="Sites, device identity, firmware, utilization." />
            <SourceLine icon={Network} label="SNMP" detail="Uptime, interfaces, CPU, memory, sensors." />
          </SidePanel>
          <SidePanel title="Source Breakdown">
            <Show
              when={sourceSummary().length > 0}
              fallback={<p class="text-sm text-gray-500 dark:text-gray-400">No devices reporting yet.</p>}
            >
              <div class="space-y-2">
                <For each={sourceSummary()}>
                  {(item) => (
                    <div class="flex items-center justify-between rounded-md bg-gray-50 px-3 py-2 text-sm dark:bg-gray-900/50">
                      <span class="font-medium text-gray-700 dark:text-gray-300">{item.source.toUpperCase()}</span>
                      <span class="text-gray-500 dark:text-gray-400">{item.count}</span>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          </SidePanel>
        </div>
      </div>

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

const SummaryCard: Component<{
  label: string;
  value: number;
  detail: string;
  icon: Component<{ class?: string; strokeWidth?: number }>;
  tone: 'blue' | 'green' | 'amber' | 'red';
}> = (props) => {
  const Icon = props.icon;
  const tones = {
    blue: 'text-blue-500',
    green: 'text-green-500',
    amber: 'text-amber-500',
    red: 'text-red-500',
  };
  return (
    <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
      <div class="flex items-center justify-between">
        <span class="text-sm font-medium text-gray-500 dark:text-gray-400">{props.label}</span>
        <Icon class={`h-4 w-4 ${tones[props.tone]}`} strokeWidth={2} />
      </div>
      <div class="mt-3 text-2xl font-semibold text-gray-900 dark:text-gray-100">{props.value}</div>
      <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{props.detail}</div>
    </div>
  );
};

const Metric: Component<{ label: string; value: string }> = (props) => (
  <div class="rounded-md bg-gray-50 px-3 py-2 dark:bg-gray-900/50">
    <div class="text-[11px] uppercase tracking-normal text-gray-500 dark:text-gray-400">{props.label}</div>
    <div class="mt-1 font-medium text-gray-900 dark:text-gray-100">{props.value}</div>
  </div>
);

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

const SidePanel: Component<{ title: string; children: JSX.Element }> = (props) => (
  <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
    <h2 class="mb-4 text-sm font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">{props.title}</h2>
    {props.children}
  </div>
);

const SourceLine: Component<{
  icon: Component<{ class?: string; strokeWidth?: number }>;
  label: string;
  detail: string;
}> = (props) => {
  const Icon = props.icon;
  return (
    <div class="mb-3 rounded-md border border-gray-200 p-3 last:mb-0 dark:border-gray-700">
      <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-gray-100">
        <Icon class="h-4 w-4 text-blue-500" strokeWidth={2} />
        {props.label}
      </div>
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{props.detail}</p>
    </div>
  );
};
