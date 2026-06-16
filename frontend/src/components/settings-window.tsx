import { useEffect, useRef, useState } from 'react';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { GetSetting, SetSetting } from '../../wailsjs/go/main/App';
import { SettingsPanel, isSettingsSection, type SettingsSection } from './settings-panel';

// Key in the backend settings(key, value) table for the last-selected pane.
const SETTINGS_LAST_PANE_KEY = 'settingsLastPane';

/** Read the requested pane from the window URL (?section=…), if valid. */
function sectionFromURL(): SettingsSection | null {
  const raw = new URLSearchParams(window.location.search).get('section');
  return raw && isSettingsSection(raw) ? raw : null;
}

/**
 * Full-page host for the Settings UI in its own native window. The backend opens
 * this window at /?view=settings&section=… (see App.openSettingsSection). The
 * pane is seeded from the URL on a cold open, hydrated from the last-used pane
 * when the URL doesn't pin one, and switched live via the settings:navigate
 * event when the menu re-targets an already-open window.
 */
export function SettingsWindow() {
  const [section, setSection] = useState<SettingsSection>(() => sectionFromURL() ?? 'networks');
  // Gate last-pane persistence until hydration settles, so the default doesn't
  // clobber the stored value on first mount.
  const hydratedRef = useRef(false);

  // Hydrate the last-used pane when the URL didn't pin one. If it did, that wins
  // and we just mark hydration complete so the persist effect can start.
  useEffect(() => {
    if (sectionFromURL()) {
      hydratedRef.current = true;
      return;
    }
    let cancelled = false;
    GetSetting(SETTINGS_LAST_PANE_KEY)
      .then((saved) => {
        if (!cancelled && isSettingsSection(saved)) setSection(saved);
      })
      .catch((e) => console.error('Failed to load last pane preference:', e))
      .finally(() => {
        hydratedRef.current = true;
      });
    return () => {
      cancelled = true;
    };
  }, []);

  // Switch panes when the menu re-targets an already-open window. A plain "Open
  // Settings" emits an empty section (just focus the window), so ignore those.
  useEffect(() => {
    const unsubscribe = EventsOn('settings:navigate', (raw: string) => {
      if (isSettingsSection(raw)) setSection(raw);
    });
    return () => unsubscribe();
  }, []);

  // Persist the selected pane (after hydration) so the next open restores it.
  useEffect(() => {
    if (!hydratedRef.current) return;
    SetSetting(SETTINGS_LAST_PANE_KEY, section).catch((e) =>
      console.error('Failed to save last pane preference:', e),
    );
  }, [section]);

  return (
    <div className="flex flex-col h-screen bg-background text-foreground">
      <header className="px-4 py-3 border-b border-border bg-card/50 flex-shrink-0">
        <h1 className="text-lg font-semibold">Settings</h1>
      </header>
      <SettingsPanel section={section} onSectionChange={setSection} />
    </div>
  );
}
