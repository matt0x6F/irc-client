import { lazy, Suspense } from 'react';
import App from './App';

const SettingsWindow = lazy(() =>
  import('./components/settings-window').then((module) => ({ default: module.SettingsWindow })),
);

export type RootViewName = 'chat' | 'settings';

export function rootViewForSearch(search: string): RootViewName {
  return new URLSearchParams(search).get('view') === 'settings' ? 'settings' : 'chat';
}

export function RootView({ search }: { search: string }) {
  if (rootViewForSearch(search) === 'settings') {
    return (
      <Suspense fallback={<div className="h-screen bg-background" aria-label="Loading settings" />}>
        <SettingsWindow />
      </Suspense>
    );
  }
  return <App />;
}
