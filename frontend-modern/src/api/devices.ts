import { apiFetchJSON } from '@/utils/apiClient';
import type { DeviceAccount, DeviceAlertSettings, DeviceInventoryItem } from '@/stores/devicesMonitoring';

export interface DevicesState {
  checks: DeviceAccount[];
  devices: DeviceInventoryItem[];
  alerts: DeviceAlertSettings;
  agent?: { script: string };
  updatedAt?: string;
}

export interface UniFiDiscoveryResponse {
  devices: DeviceInventoryItem[];
}

const baseUrl = '/api/devices';

export const DevicesAPI = {
  getState: () => apiFetchJSON<DevicesState>(`${baseUrl}/state`),
  createCheck: (check: Partial<DeviceAccount>) =>
    apiFetchJSON<DeviceAccount>(`${baseUrl}/checks`, {
      method: 'POST',
      body: JSON.stringify(check),
    }),
  updateCheck: (id: string, check: Partial<DeviceAccount>) =>
    apiFetchJSON<DeviceAccount>(`${baseUrl}/checks/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(check),
    }),
  deleteCheck: (id: string) =>
    apiFetchJSON<{ success: boolean }>(`${baseUrl}/checks/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  createDevice: (device: Partial<DeviceInventoryItem>) =>
    apiFetchJSON<DeviceInventoryItem>(`${baseUrl}/inventory`, {
      method: 'POST',
      body: JSON.stringify(device),
    }),
  updateDevice: (id: string, device: Partial<DeviceInventoryItem>) =>
    apiFetchJSON<DeviceInventoryItem>(`${baseUrl}/inventory/${encodeURIComponent(id)}`, {
      method: 'PUT',
      body: JSON.stringify(device),
    }),
  deleteDevice: (id: string) =>
    apiFetchJSON<{ success: boolean }>(`${baseUrl}/inventory/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
  pollNow: () =>
    apiFetchJSON<DevicesState>(`${baseUrl}/poll`, {
      method: 'POST',
    }),
  updateAlerts: (alerts: DeviceAlertSettings) =>
    apiFetchJSON<DeviceAlertSettings>(`${baseUrl}/alerts`, {
      method: 'PUT',
      body: JSON.stringify(alerts),
    }),
  getAgentScript: () => apiFetchJSON<{ script: string }>(`${baseUrl}/agent/script`),
  updateAgentScript: (script: string) =>
    apiFetchJSON<{ script: string }>(`${baseUrl}/agent/script`, {
      method: 'PUT',
      body: JSON.stringify({ script }),
    }),
  discoverUniFi: (params: { checkId?: string; baseUrl?: string; apiKey?: string }) =>
    apiFetchJSON<UniFiDiscoveryResponse>(`${baseUrl}/unifi/discover`, {
      method: 'POST',
      body: JSON.stringify({
        checkId: params.checkId,
        baseUrl: params.baseUrl || 'https://api.ui.com',
        endpoint: '/v1/devices',
        apiKey: params.apiKey,
      }),
    }),
};
