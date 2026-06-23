import { useEffect, useState } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { CheckForUpdates, SkipUpdateVersion } from '../../wailsjs/go/main/App';

// Shape of the updater:update-available payload emitted by the Go backend's
// surfaceUpdateIfAvailable (see app_updater.go). currentVersion is the running
// build; version is the newer release found; notes is the release body.
interface UpdateInfo {
  currentVersion: string;
  version: string;
  notes: string;
}

// UpdateAvailableDialog is the in-app prompt shown when a background check finds
// a newer release. It replaces the framework's auto-download flow for the
// *decision* step: the user picks Install, Remind Me Later, or Skip This Version
// before anything is downloaded.
//
// Why this exists: the Wails builtin updater window auto-downloads on a found
// update and, once in the "Update Ready" state, offers only "Close" (which
// records nothing) — so a dismissed version kept re-prompting every check. Here
// the decision is ours: "Skip This Version" persists to the settings table via
// SkipUpdateVersion so the background check stops surfacing that exact version,
// even across restarts. "Install" hands the download/verify/restart flow back to
// the framework window via CheckForUpdates (which re-checks and stages the
// release). "Remind Me Later" persists nothing, so the next poll re-prompts.
export function UpdateAvailableDialog() {
  const [info, setInfo] = useState<UpdateInfo | null>(null);

  useEffect(() => {
    const unsubscribe = EventsOn('updater:update-available', (data: any) => {
      if (!data?.version) return;
      setInfo({
        currentVersion: String(data.currentVersion ?? ''),
        version: String(data.version),
        notes: String(data.notes ?? ''),
      });
    });
    return () => unsubscribe();
  }, []);

  if (!info) return null;

  const dismiss = () => setInfo(null);

  // Install hands off to the framework's updater window (download → verify →
  // Restart & Apply). We close our prompt immediately; that window takes over.
  const handleInstall = () => {
    void CheckForUpdates();
    dismiss();
  };

  // Skip persists the version so the background check won't surface it again.
  const handleSkip = () => {
    void SkipUpdateVersion(info.version);
    dismiss();
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={dismiss}>
      <div
        className="w-[28rem] max-h-[80vh] overflow-hidden rounded-lg border border-border bg-background shadow-[var(--shadow-lg)] flex flex-col"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-5 border-b border-border">
          <h2 className="text-lg font-semibold">Update available</h2>
          <p className="mt-1 text-sm text-muted-foreground">
            {info.currentVersion ? (
              <>
                <span className="line-through">{info.currentVersion}</span>
                {' → '}
                <span className="font-medium text-foreground">{info.version}</span>
              </>
            ) : (
              <span className="font-medium text-foreground">{info.version}</span>
            )}
          </p>
        </div>

        {info.notes ? (
          <div className="overflow-y-auto px-5 py-4 text-sm text-foreground/90 whitespace-pre-wrap break-words">
            {info.notes}
          </div>
        ) : (
          <div className="px-5 py-4 text-sm text-muted-foreground">A new version is ready to install.</div>
        )}

        <div className="flex items-center justify-between gap-2 p-4 border-t border-border">
          <button
            type="button"
            onClick={handleSkip}
            className="px-3 py-1.5 text-xs text-muted-foreground rounded-lg hover:bg-accent/60 transition-all"
          >
            Skip This Version
          </button>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={dismiss}
              className="px-3 py-1.5 text-sm border border-border rounded-lg hover:bg-accent/60 transition-all"
            >
              Remind Me Later
            </button>
            <button
              type="button"
              onClick={handleInstall}
              className="px-4 py-1.5 text-sm bg-primary text-primary-foreground rounded-lg hover:bg-primary/90 transition-all shadow-[var(--shadow-sm)] hover:shadow-[var(--shadow-md)] font-medium"
            >
              Install
            </button>
          </div>
        </div>
      </div>
    </div>
  );
}
