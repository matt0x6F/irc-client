import React from 'react';
import { UnfurlURL } from '../../wailsjs/go/main/App';

type PreviewState =
  | { kind: 'loading' }
  | { kind: 'ok'; title: string; description: string; siteName: string; imageDataUri: string }
  | { kind: 'blocked' }
  | { kind: 'error' };

export function PreviewChip({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="ml-1 align-baseline rounded px-1 text-[10px] uppercase tracking-wide
                 border border-border text-muted-foreground hover:text-foreground hover:bg-muted"
      aria-label="Load link preview"
    >
      Preview
    </button>
  );
}

export function LinkPreviewCard({ url }: { url: string }) {
  const [state, setState] = React.useState<PreviewState>({ kind: 'loading' });

  React.useEffect(() => {
    let alive = true;
    UnfurlURL(url)
      .then((p) => {
        if (!alive) return;
        if (!p) {
          setState({ kind: 'error' });
          return;
        }
        if (p.status === 'ok') {
          setState({
            kind: 'ok',
            title: p.title,
            description: p.description,
            siteName: p.siteName,
            imageDataUri: p.imageDataUri,
          });
        } else if (p.status === 'blocked') {
          setState({ kind: 'blocked' });
        } else {
          setState({ kind: 'error' });
        }
      })
      .catch(() => {
        if (alive) setState({ kind: 'error' });
      });
    return () => {
      alive = false;
    };
  }, [url]);

  const shell = 'mt-1 max-w-md rounded-md border border-border bg-card/50 p-2 text-xs';

  if (state.kind === 'loading') {
    return <div className={shell + ' text-muted-foreground'}>Loading preview…</div>;
  }
  if (state.kind === 'blocked') {
    return (
      <div className={shell + ' text-muted-foreground'}>Preview blocked (private address).</div>
    );
  }
  if (state.kind === 'error') {
    return <div className={shell + ' text-muted-foreground'}>Preview unavailable.</div>;
  }
  return (
    <div className={shell + ' flex gap-2'}>
      {state.imageDataUri ? (
        <img
          src={state.imageDataUri}
          alt=""
          className="h-12 w-12 shrink-0 rounded object-cover"
        />
      ) : null}
      <div className="min-w-0">
        {state.siteName ? (
          <div className="text-muted-foreground">{state.siteName}</div>
        ) : null}
        <div className="font-medium truncate">{state.title || url}</div>
        {state.description ? (
          <div className="text-muted-foreground line-clamp-2">{state.description}</div>
        ) : null}
      </div>
    </div>
  );
}
