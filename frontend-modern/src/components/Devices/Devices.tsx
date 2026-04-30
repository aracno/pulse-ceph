import { Component, For, Show, createMemo } from 'solid-js';
import { useNavigate } from '@solidjs/router';
import { useWebSocket } from '@/App';
import type { ManagedDevice } from '@/types/api';
import Activity from 'lucide-solid/icons/activity';
import AlertTriangle from 'lucide-solid/icons/alert-triangle';
import CheckCircle2 from 'lucide-solid/icons/check-circle-2';
import Globe2 from 'lucide-solid/icons/globe-2';
import Network from 'lucide-solid/icons/network';
import Router from 'lucide-solid/icons/router';
import ServerCog from 'lucide-solid/icons/server-cog';
import Settings from 'lucide-solid/icons/settings';
import ShieldCheck from 'lucide-solid/icons/shield-check';
import Wifi from 'lucide-solid/icons/wifi';

const fallbackDevices: ManagedDevice[] = [];

const statusClasses: Record<string, string> = {
  online: 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300',
  warning: 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300',
  offline: 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300',
  unknown: 'bg-gray-100 text-gray-700 dark:bg-gray-800 dark:text-gray-300',
};

const typeIcon = (type?: string) => {
  switch ((type || '').toLowerCase()) {
    case 'router':
    case 'gateway':
    case 'modem':
      return Router;
    case 'access_point':
    case 'ap':
      return Wifi;
    case 'controller':
    case 'console':
      return ServerCog;
    default:
      return Network;
  }
};

const percentText = (value?: number) =>
  typeof value === 'number' && Number.isFinite(value) ? `${Math.round(value)}%` : '-';

export const Devices: Component = () => {
  const navigate = useNavigate();
  const { state } = useWebSocket();

  const devices = createMemo(() => state.devices ?? fallbackDevices);
  const onlineCount = createMemo(() => devices().filter((device) => device.status === 'online').length);
  const warningCount = createMemo(() => devices().filter((device) => device.status === 'warning').length);
  const offlineCount = createMemo(() => devices().filter((device) => device.status === 'offline').length);
  const managedCount = createMemo(() => devices().filter((device) => device.managed !== false).length);

  const sourceSummary = createMemo(() => {
    const sources = devices().reduce<Record<string, number>>((acc, device) => {
      const source = device.source || 'manual';
      acc[source] = (acc[source] || 0) + 1;
      return acc;
    }, {});
    return Object.entries(sources).map(([source, count]) => ({ source, count }));
  });

  return (
    <div class="space-y-6">
      <div class="flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
        <div>
          <div class="flex items-center gap-3">
            <div class="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300">
              <Network class="h-5 w-5" strokeWidth={2} />
            </div>
            <div>
              <h1 class="text-2xl font-semibold tracking-normal text-gray-900 dark:text-gray-100">
                Devices
              </h1>
              <p class="mt-1 text-sm text-gray-600 dark:text-gray-400">
                Network hardware health for switches, routers, modems, gateways, and UniFi equipment.
              </p>
            </div>
          </div>
        </div>
        <button
          type="button"
          onClick={() => navigate('/settings/devices')}
          class="inline-flex h-9 items-center justify-center gap-2 rounded-md border border-gray-300 bg-white px-3 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-200 dark:hover:bg-gray-700"
        >
          <Settings class="h-4 w-4" strokeWidth={2} />
          Configure
        </button>
      </div>

      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-500 dark:text-gray-400">Managed</span>
            <ShieldCheck class="h-4 w-4 text-blue-500" strokeWidth={2} />
          </div>
          <div class="mt-3 text-2xl font-semibold text-gray-900 dark:text-gray-100">{managedCount()}</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">Devices under active monitoring</div>
        </div>
        <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-500 dark:text-gray-400">Online</span>
            <CheckCircle2 class="h-4 w-4 text-green-500" strokeWidth={2} />
          </div>
          <div class="mt-3 text-2xl font-semibold text-gray-900 dark:text-gray-100">{onlineCount()}</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">Reachable and reporting</div>
        </div>
        <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-500 dark:text-gray-400">Warnings</span>
            <AlertTriangle class="h-4 w-4 text-amber-500" strokeWidth={2} />
          </div>
          <div class="mt-3 text-2xl font-semibold text-gray-900 dark:text-gray-100">{warningCount()}</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">Health, firmware, or metrics drift</div>
        </div>
        <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
          <div class="flex items-center justify-between">
            <span class="text-sm font-medium text-gray-500 dark:text-gray-400">Offline</span>
            <Activity class="h-4 w-4 text-red-500" strokeWidth={2} />
          </div>
          <div class="mt-3 text-2xl font-semibold text-gray-900 dark:text-gray-100">{offlineCount()}</div>
          <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">Missing heartbeat or unreachable</div>
        </div>
      </div>

      <div class="grid gap-4 xl:grid-cols-[1fr_360px]">
        <div class="overflow-hidden rounded-lg border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
          <div class="border-b border-gray-200 px-4 py-3 dark:border-gray-700">
            <div class="flex items-center justify-between gap-3">
              <div>
                <h2 class="text-sm font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">
                  Inventory
                </h2>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Basic health indicators collected from UniFi API and SNMP targets.
                </p>
              </div>
              <div class="text-xs text-gray-500 dark:text-gray-400">{devices().length} devices</div>
            </div>
          </div>

          <Show
            when={devices().length > 0}
            fallback={
              <div class="px-6 py-12 text-center">
                <div class="mx-auto flex h-12 w-12 items-center justify-center rounded-lg bg-gray-100 text-gray-500 dark:bg-gray-900 dark:text-gray-400">
                  <Network class="h-6 w-6" strokeWidth={2} />
                </div>
                <h3 class="mt-4 text-sm font-semibold text-gray-900 dark:text-gray-100">
                  No network devices yet
                </h3>
                <p class="mx-auto mt-2 max-w-xl text-sm text-gray-500 dark:text-gray-400">
                  Configure UniFi Site Manager or SNMP in Settings to start collecting real device health.
                </p>
                <button
                  type="button"
                  onClick={() => navigate('/settings/devices')}
                  class="mt-4 inline-flex h-9 items-center justify-center gap-2 rounded-md bg-blue-600 px-3 text-sm font-medium text-white transition-colors hover:bg-blue-700"
                >
                  <Settings class="h-4 w-4" strokeWidth={2} />
                  Open Devices settings
                </button>
              </div>
            }
          >
            <div class="overflow-x-auto">
              <table class="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
                <thead class="bg-gray-50 dark:bg-gray-900/60">
                  <tr>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Device</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Status</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">CPU</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Memory</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Uptime</th>
                    <th class="px-4 py-3 text-left text-xs font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">Source</th>
                  </tr>
                </thead>
                <tbody class="divide-y divide-gray-200 dark:divide-gray-700">
                  <For each={devices()}>
                    {(device) => {
                      const Icon = typeIcon(device.type);
                      return (
                        <tr class="hover:bg-gray-50 dark:hover:bg-gray-900/40">
                          <td class="px-4 py-3">
                            <div class="flex items-center gap-3">
                              <div class="flex h-9 w-9 items-center justify-center rounded-md bg-gray-100 text-gray-600 dark:bg-gray-900 dark:text-gray-300">
                                <Icon class="h-4 w-4" strokeWidth={2} />
                              </div>
                              <div class="min-w-0">
                                <div class="truncate text-sm font-medium text-gray-900 dark:text-gray-100">
                                  {device.name || device.hostname || device.ip || device.id}
                                </div>
                                <div class="truncate text-xs text-gray-500 dark:text-gray-400">
                                  {[device.model, device.ip].filter(Boolean).join(' · ') || device.type || 'network device'}
                                </div>
                              </div>
                            </div>
                          </td>
                          <td class="px-4 py-3">
                            <span class={`inline-flex rounded px-2 py-1 text-xs font-medium ${statusClasses[device.status || 'unknown'] || statusClasses.unknown}`}>
                              {device.status || 'unknown'}
                            </span>
                          </td>
                          <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{percentText(device.cpuUsage)}</td>
                          <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{percentText(device.memoryUsage)}</td>
                          <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{device.uptime || '-'}</td>
                          <td class="px-4 py-3 text-sm text-gray-700 dark:text-gray-300">{device.source || 'manual'}</td>
                        </tr>
                      );
                    }}
                  </For>
                </tbody>
              </table>
            </div>
          </Show>
        </div>

        <div class="space-y-4">
          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
            <h2 class="text-sm font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">
              Collection Sources
            </h2>
            <div class="mt-4 space-y-3">
              <div class="rounded-md border border-gray-200 p-3 dark:border-gray-700">
                <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-gray-100">
                  <Globe2 class="h-4 w-4 text-blue-500" strokeWidth={2} />
                  UniFi Site Manager API
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Preferred source for UniFi consoles, sites, and UniFi network devices.
                </p>
              </div>
              <div class="rounded-md border border-gray-200 p-3 dark:border-gray-700">
                <div class="flex items-center gap-2 text-sm font-medium text-gray-900 dark:text-gray-100">
                  <Network class="h-4 w-4 text-cyan-500" strokeWidth={2} />
                  SNMP
                </div>
                <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                  Generic fallback for managed switches, routers, modems, and UPS-like appliances.
                </p>
              </div>
            </div>
          </div>

          <div class="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-800">
            <h2 class="text-sm font-semibold uppercase tracking-normal text-gray-500 dark:text-gray-400">
              Source Breakdown
            </h2>
            <Show
              when={sourceSummary().length > 0}
              fallback={<p class="mt-4 text-sm text-gray-500 dark:text-gray-400">No sources reporting yet.</p>}
            >
              <div class="mt-4 space-y-2">
                <For each={sourceSummary()}>
                  {(item) => (
                    <div class="flex items-center justify-between rounded-md bg-gray-50 px-3 py-2 text-sm dark:bg-gray-900/50">
                      <span class="font-medium text-gray-700 dark:text-gray-300">{item.source}</span>
                      <span class="text-gray-500 dark:text-gray-400">{item.count}</span>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          </div>
        </div>
      </div>
    </div>
  );
};
