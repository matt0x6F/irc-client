import { render, screen, waitFor } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { LinkPreviewCard } from './link-preview-card';
import { IRCFormattedText } from './irc-formatted-text';
import { useSettingsStore } from '../stores/settings';

vi.mock('../../wailsjs/go/main/App', () => ({
  UnfurlURL: vi.fn(),
}));

import { UnfurlURL } from '../../wailsjs/go/main/App';

describe('LinkPreviewCard', () => {
  beforeEach(() => vi.clearAllMocks());

  it('renders title/description on an ok preview', async () => {
    (UnfurlURL as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'https://x.example',
      status: 'ok',
      title: 'Hello',
      description: 'World',
      siteName: 'Example',
      imageDataUri: '',
      fetchedAt: '',
    });
    render(<LinkPreviewCard url="https://x.example" />);
    await waitFor(() => expect(screen.getByText('Hello')).toBeInTheDocument());
    expect(screen.getByText('World')).toBeInTheDocument();
  });

  it('renders the preview image as a full-width hero, not a tiny crop', async () => {
    (UnfurlURL as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'https://x.example',
      status: 'ok',
      title: 'Video',
      description: '',
      siteName: 'YouTube',
      imageDataUri: 'data:image/png;base64,iVBORw0KGgo=',
      fetchedAt: '',
    });
    const { container } = render(<LinkPreviewCard url="https://x.example" />);
    const img = await waitFor(() => {
      const el = container.querySelector('img');
      if (!el) throw new Error('no image rendered');
      return el as HTMLImageElement;
    });
    expect(img.getAttribute('src')).toBe('data:image/png;base64,iVBORw0KGgo=');
    // Default (pre-measure) layout must be the wide hero, never the legacy h-12 w-12 crop.
    expect(img.className).toContain('w-full');
    expect(img.className).not.toContain('h-12');
  });

  it('shows a blocked notice for a blocked status', async () => {
    (UnfurlURL as ReturnType<typeof vi.fn>).mockResolvedValue({
      url: 'http://10.0.0.1',
      status: 'blocked',
      title: '',
      description: '',
      siteName: '',
      imageDataUri: '',
      fetchedAt: '',
    });
    render(<LinkPreviewCard url="http://10.0.0.1" />);
    await waitFor(() => expect(screen.getByText(/private address/i)).toBeInTheDocument());
  });
});

describe('IRCFormattedText chip behaviour', () => {
  beforeEach(() => vi.clearAllMocks());

  it('renders no Preview chip when unfurls are disabled', () => {
    useSettingsStore.setState({ unfurlsEnabled: false });
    render(<IRCFormattedText text="see https://x.example now" />);
    expect(screen.queryByRole('button', { name: /load link preview/i })).toBeNull();
  });

  it('renders a Preview chip when unfurls are enabled', () => {
    useSettingsStore.setState({ unfurlsEnabled: true });
    render(<IRCFormattedText text="see https://x.example now" enableUnfurls />);
    expect(screen.getByRole('button', { name: /load link preview/i })).toBeInTheDocument();
  });
});
