import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@solidjs/testing-library';
import { createSignal } from 'solid-js';

import { DockerContainerRow, DockerUnifiedTable } from '@/components/Docker/DockerUnifiedTable';

const getAllDockerMetadataMock = vi.fn();
const updateDockerMetadataMock = vi.fn();

vi.mock('@/components/shared/StatusDot', () => ({
  StatusDot: () => <span data-testid="status-dot" />,
}));

vi.mock('@/components/shared/Card', () => ({
  Card: (props: any) => <div>{props.children}</div>,
}));

vi.mock('@/components/shared/EmptyState', () => ({
  EmptyState: (props: any) => <div>{props.title}</div>,
}));

vi.mock('@/components/shared/responsive', () => ({
  ResponsiveMetricCell: () => <div data-testid="responsive-metric-cell" />,
}));

vi.mock('@/components/Dashboard/StackedMemoryBar', () => ({
  StackedMemoryBar: () => <div data-testid="stacked-memory-bar" />,
}));

vi.mock('@/components/Docker/UpdateBadge', () => ({
  UpdateButton: () => <div data-testid="update-button" />,
}));

vi.mock('@/components/Discovery/DiscoveryTab', () => ({
  DiscoveryTab: () => <div data-testid="discovery-tab" />,
}));

vi.mock('@/components/shared/HistoryChart', () => ({
  HistoryChart: () => <div data-testid="history-chart" />,
}));

vi.mock('@/stores/alertsActivation', () => ({
  useAlertsActivation: () => ({
    getMetricThresholds: () => ({ warning: 75, critical: 90 }),
  }),
}));

vi.mock('@/hooks/useBreakpoint', () => ({
  useBreakpoint: () => ({
    isMobile: () => false,
  }),
}));

vi.mock('@/hooks/usePersistentSignal', async () => {
  const solid = await import('solid-js');
  return {
    usePersistentSignal: <T,>(_key: string, initialValue: T) => solid.createSignal(initialValue),
  };
});

vi.mock('@/api/dockerMetadata', () => ({
  DockerMetadataAPI: {
    updateMetadata: (...args: unknown[]) => updateDockerMetadataMock(...args),
    getAllMetadata: (...args: unknown[]) => getAllDockerMetadataMock(...args),
  },
}));

vi.mock('@/utils/toast', () => ({
  showSuccess: vi.fn(),
  showError: vi.fn(),
}));

describe('DockerContainerRow custom URL editor', () => {
  afterEach(() => {
    cleanup();
    getAllDockerMetadataMock.mockReset();
    updateDockerMetadataMock.mockReset();
  });

  it('opens the URL editor using the stable docker metadata id', async () => {
    const host = {
      id: 'docker-host-1',
      hostname: 'docker-host-1.local',
      displayName: 'Docker Host One',
      status: 'online',
      totalMemoryBytes: 8 * 1024 * 1024 * 1024,
    } as any;

    const container = {
      id: 'container-123',
      name: 'app',
      image: 'ghcr.io/example/app:latest',
      state: 'running',
      status: 'running',
      cpuPercent: 0,
      memoryPercent: 0,
      memoryUsageBytes: 0,
      memoryLimitBytes: 0,
      restartCount: 0,
      labels: {},
      ports: [],
      networks: [],
      mounts: [],
    } as any;

    const metadataId = 'docker-host-1:container:container-123';

    render(() => (
      <table>
        <tbody>
          <DockerContainerRow
            row={{ kind: 'container', id: metadataId, host, container } as any}
            isMobile={() => false}
            guestMetadata={{
              [metadataId]: {
                id: metadataId,
                customUrl: 'https://app.internal',
              },
            }}
          />
        </tbody>
      </table>
    ));

    fireEvent.click(screen.getByTitle('Edit URL'));

    expect(await screen.findByDisplayValue('https://app.internal')).toBeInTheDocument();
  });

  it('refreshes migrated metadata when a container runtime id changes', async () => {
    const createHost = (containerId: string, customUrl?: string) => ({
      id: 'docker-host-1',
      hostname: 'docker-host-1.local',
      displayName: 'Docker Host One',
      status: 'online',
      totalMemoryBytes: 8 * 1024 * 1024 * 1024,
      containers: [
        {
          id: containerId,
          name: 'app',
          image: 'ghcr.io/example/app:latest',
          state: 'running',
          status: 'running',
          cpuPercent: 0,
          memoryPercent: 0,
          memoryUsageBytes: 0,
          memoryLimitBytes: 0,
          restartCount: 0,
          labels: {},
          ports: [],
          networks: [],
          mounts: [],
        },
      ],
      services: [],
      ...(customUrl
        ? {
            customUrl,
          }
        : {}),
    }) as any;

    getAllDockerMetadataMock
      .mockResolvedValueOnce({
        'docker-host-1:container:container-old': {
          id: 'docker-host-1:container:container-old',
          customUrl: 'https://old.internal',
        },
      })
      .mockResolvedValueOnce({
        'docker-host-1:container:container-new': {
          id: 'docker-host-1:container:container-new',
          customUrl: 'https://new.internal',
        },
      });

    let setHosts!: (hosts: any[]) => void;

    render(() => {
      const [hosts, setHostsSignal] = createSignal([createHost('container-old')]);
      setHosts = setHostsSignal;
      return <DockerUnifiedTable hosts={hosts()} groupingMode="flat" />;
    });

    expect(await screen.findByTitle('Open https://old.internal')).toBeInTheDocument();

    setHosts([createHost('container-new')]);

    await waitFor(() => {
      expect(getAllDockerMetadataMock).toHaveBeenCalledTimes(2);
    });
    expect(await screen.findByTitle('Open https://new.internal')).toBeInTheDocument();
  });
});
