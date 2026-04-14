import type { Accessor } from 'solid-js';
import { createMemo, Show } from 'solid-js';
import { useLocation } from '@solidjs/router';
import type { VersionInfo } from '@/api/updates';
import type { SecurityStatus } from '@/types/config';
import {
  V6_RC_ANNOUNCEMENT,
  shouldShowV6RcAnnouncement,
} from '@/constants/releaseAnnouncements';
import { createLocalStorageStringSignal, STORAGE_KEYS } from '@/utils/localStorage';
import FlaskConicalIcon from 'lucide-solid/icons/flask-conical';
import ExternalLinkIcon from 'lucide-solid/icons/external-link';
import XIcon from 'lucide-solid/icons/x';

interface ReleaseAnnouncementBannerProps {
  versionInfo: Accessor<VersionInfo | null>;
  securityStatus: Accessor<SecurityStatus | null>;
}

export function ReleaseAnnouncementBanner(props: ReleaseAnnouncementBannerProps) {
  const location = useLocation();
  const [dismissedAnnouncementId, setDismissedAnnouncementId] =
    createLocalStorageStringSignal(STORAGE_KEYS.RELEASE_ANNOUNCEMENT_DISMISSED, '');

  const isDismissed = createMemo(
    () => dismissedAnnouncementId() === V6_RC_ANNOUNCEMENT.id,
  );

  const shouldShow = createMemo(() => {
    if (isDismissed()) {
      return false;
    }

    return shouldShowV6RcAnnouncement({
      version: props.versionInfo()?.version,
      pathname: location.pathname,
      securityStatus: props.securityStatus(),
    });
  });

  const dismiss = () => {
    setDismissedAnnouncementId(V6_RC_ANNOUNCEMENT.id);
  };

  return (
    <Show when={shouldShow()}>
      <div class="border-b border-emerald-200 bg-emerald-50 text-emerald-950 dark:border-emerald-900/70 dark:bg-emerald-950/40 dark:text-emerald-100">
        <div class="px-4 py-3">
          <div class="flex items-start justify-between gap-3">
            <div class="flex min-w-0 flex-1 items-start gap-3">
              <div class="mt-0.5 rounded-full bg-emerald-100 p-2 text-emerald-700 dark:bg-emerald-900/70 dark:text-emerald-200">
                <FlaskConicalIcon class="h-4 w-4" />
              </div>
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span class="text-sm font-semibold">Pulse v6 RC testing</span>
                  <span class="rounded-full bg-emerald-100 px-2 py-0.5 text-[11px] font-medium text-emerald-800 dark:bg-emerald-900/70 dark:text-emerald-200">
                    {V6_RC_ANNOUNCEMENT.tag}
                  </span>
                </div>
                <p class="mt-1 text-sm leading-relaxed text-emerald-900/85 dark:text-emerald-100/85">
                  <code class="rounded bg-emerald-100 px-1 py-0.5 text-[12px] dark:bg-emerald-900/70">
                    5.1.x
                  </code>{' '}
                  remains the current stable line. Pulse v6 changes the runtime,
                  upgrade path, navigation, and product model substantially. If you rely on
                  Pulse today, test v6 in a staging or non-production environment and report
                  any issues before the stable cut.
                </p>
                <div class="mt-3 flex flex-wrap items-center gap-2">
                  <a
                    href={V6_RC_ANNOUNCEMENT.changelogUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex items-center gap-1 rounded-md bg-emerald-700 px-3 py-1.5 text-sm font-medium text-white transition-colors hover:bg-emerald-800 dark:bg-emerald-600 dark:hover:bg-emerald-500"
                  >
                    Read v6 changelog
                    <ExternalLinkIcon class="h-3.5 w-3.5" />
                  </a>
                  <a
                    href={V6_RC_ANNOUNCEMENT.releaseUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex items-center gap-1 rounded-md border border-emerald-300 bg-white px-3 py-1.5 text-sm font-medium text-emerald-900 transition-colors hover:bg-emerald-100 dark:border-emerald-800 dark:bg-emerald-950/20 dark:text-emerald-100 dark:hover:bg-emerald-900/40"
                  >
                    View v6 RC
                    <ExternalLinkIcon class="h-3.5 w-3.5" />
                  </a>
                  <a
                    href={V6_RC_ANNOUNCEMENT.demoUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    class="inline-flex items-center gap-1 rounded-md border border-transparent px-2 py-1.5 text-sm font-medium text-emerald-800 transition-colors hover:bg-emerald-100 dark:text-emerald-200 dark:hover:bg-emerald-900/30"
                  >
                    Open demo
                    <ExternalLinkIcon class="h-3.5 w-3.5" />
                  </a>
                </div>
              </div>
            </div>
            <button
              type="button"
              onClick={dismiss}
              class="rounded-md p-1 text-emerald-700 transition-colors hover:bg-emerald-100 hover:text-emerald-900 dark:text-emerald-300 dark:hover:bg-emerald-900/50 dark:hover:text-emerald-100"
              title="Dismiss"
              aria-label="Dismiss v6 RC announcement"
            >
              <XIcon class="h-4 w-4" />
            </button>
          </div>
        </div>
      </div>
    </Show>
  );
}
