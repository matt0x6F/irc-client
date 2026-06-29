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
  return <OkCard url={url} {...state} />;
}

function OkCard({
  url,
  title,
  description,
  siteName,
  imageDataUri,
}: {
  url: string;
  title: string;
  description: string;
  siteName: string;
  imageDataUri: string;
}) {
  // Measure the loaded image so we can pick a layout: large/landscape art renders
  // as a full-width hero (the natural shape for article/video thumbnails), while a
  // small logo/favicon falls back to a left-aligned thumbnail instead of being
  // stretched. Before the image loads we assume hero — that covers most og:images.
  const [dims, setDims] = React.useState<{ w: number; h: number } | null>(null);
  const measure = (e: React.SyntheticEvent<HTMLImageElement>) =>
    setDims({ w: e.currentTarget.naturalWidth, h: e.currentTarget.naturalHeight });
  const isSmallLogo = dims !== null && (dims.w < 200 || dims.h > dims.w * 1.2);

  const shell = 'mt-1 max-w-md rounded-md border border-border bg-card/50 p-2 text-xs';
  const text = (
    <div className="min-w-0">
      {siteName ? <div className="text-muted-foreground">{siteName}</div> : null}
      <div className="font-medium truncate">{title || url}</div>
      {description ? (
        <div className="text-muted-foreground line-clamp-3">{description}</div>
      ) : null}
    </div>
  );

  if (!imageDataUri) {
    return <div className={shell}>{text}</div>;
  }
  if (isSmallLogo) {
    return (
      <div className={shell + ' flex gap-2'}>
        <img
          src={imageDataUri}
          alt=""
          onLoad={measure}
          className="h-14 w-14 shrink-0 rounded object-cover"
        />
        {text}
      </div>
    );
  }
  return (
    <div className={shell}>
      <img
        src={imageDataUri}
        alt=""
        onLoad={measure}
        className="mb-2 w-full max-h-72 rounded object-contain bg-muted/40"
      />
      {text}
    </div>
  );
}
