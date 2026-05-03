import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@solidjs/testing-library';
import { DiagnosticsPanel } from '../DiagnosticsPanel';

const apiFetchJSONMock = vi.fn();
const showErrorMock = vi.fn();
const showSuccessMock = vi.fn();

vi.mock('@/utils/apiClient', () => ({
  apiFetchJSON: (...args: unknown[]) => apiFetchJSONMock(...args),
}));

vi.mock('@/utils/toast', () => ({
  showError: (...args: unknown[]) => showErrorMock(...args),
  showSuccess: (...args: unknown[]) => showSuccessMock(...args),
}));

const readBlobText = (blob: Blob): Promise<string> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ''));
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });

describe('DiagnosticsPanel', () => {
  let createObjectURLMock: ReturnType<typeof vi.fn>;
  let revokeObjectURLMock: ReturnType<typeof vi.fn>;
  let clickMock: ReturnType<typeof vi.fn>;
  let originalCreateObjectURL: typeof URL.createObjectURL | undefined;
  let originalRevokeObjectURL: typeof URL.revokeObjectURL | undefined;
  let originalClick: typeof HTMLAnchorElement.prototype.click;

  beforeEach(() => {
    apiFetchJSONMock.mockReset();
    showErrorMock.mockReset();
    showSuccessMock.mockReset();

    createObjectURLMock = vi.fn(() => 'blob:pulse-diagnostics');
    revokeObjectURLMock = vi.fn();
    clickMock = vi.fn();

    originalCreateObjectURL = URL.createObjectURL;
    originalRevokeObjectURL = URL.revokeObjectURL;
    originalClick = HTMLAnchorElement.prototype.click;

    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: createObjectURLMock,
    });
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: revokeObjectURLMock,
    });
    HTMLAnchorElement.prototype.click = clickMock as unknown as typeof HTMLAnchorElement.prototype.click;
  });

  afterEach(() => {
    cleanup();
    if (originalCreateObjectURL) {
      Object.defineProperty(URL, 'createObjectURL', {
        configurable: true,
        value: originalCreateObjectURL,
      });
    } else {
      delete (URL as unknown as { createObjectURL?: typeof URL.createObjectURL }).createObjectURL;
    }
    if (originalRevokeObjectURL) {
      Object.defineProperty(URL, 'revokeObjectURL', {
        configurable: true,
        value: originalRevokeObjectURL,
      });
    } else {
      delete (URL as unknown as { revokeObjectURL?: typeof URL.revokeObjectURL }).revokeObjectURL;
    }
    HTMLAnchorElement.prototype.click = originalClick;
    vi.restoreAllMocks();
  });

  it('exports GitHub diagnostics when backend arrays arrive as null', async () => {
    apiFetchJSONMock.mockResolvedValue({
      version: 'v5.1.29',
      runtime: 'go',
      uptime: 12,
      nodes: null,
      pbs: null,
      system: {
        os: 'linux',
        arch: 'amd64',
        goVersion: 'go1.25.0',
        numCPU: 2,
        numGoroutine: 10,
        memoryMB: 64,
      },
      metricsStore: null,
      apiTokens: null,
      dockerAgents: null,
      alerts: null,
      aiChat: null,
      discovery: {
        enabled: true,
        configuredSubnet: '10.0.0.0/24',
        activeSubnet: '10.0.0.0/24',
        environmentOverride: '10.0.0.0/24',
      },
      errors: null,
    });

    render(() => <DiagnosticsPanel />);

    fireEvent.click(screen.getAllByRole('button', { name: /run diagnostics/i })[0]);
    await waitFor(() => expect(screen.getByRole('button', { name: 'GitHub' })).toBeInTheDocument());

    fireEvent.click(screen.getByRole('button', { name: 'GitHub' }));

    await waitFor(() => expect(clickMock).toHaveBeenCalledOnce());
    expect(showErrorMock).not.toHaveBeenCalled();
    expect(showSuccessMock).toHaveBeenCalledWith('Diagnostics exported (sanitized)');
    expect(revokeObjectURLMock).toHaveBeenCalledWith('blob:pulse-diagnostics');

    const exportedBlob = createObjectURLMock.mock.calls[0][0] as Blob;
    const payload = JSON.parse(await readBlobText(exportedBlob));

    expect(payload.nodes).toEqual([]);
    expect(payload.pbs).toEqual([]);
    expect(payload.errors).toEqual([]);
    expect(payload.discovery).toEqual(
      expect.objectContaining({
        configuredSubnet: '[REDACTED_SUBNET]',
        activeSubnet: '[REDACTED_SUBNET]',
        environmentOverride: '[REDACTED]',
      }),
    );
  });
});
