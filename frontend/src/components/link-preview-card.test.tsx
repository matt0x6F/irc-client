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
