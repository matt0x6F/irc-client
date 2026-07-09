import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { NetworkTile } from '../network-tile';
import { storage } from '../../../wailsjs/go/models';
import { GetNetworkIcon } from '../../../wailsjs/go/main/App';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetNetworkIcon: vi.fn().mockResolvedValue(''), // no custom icon -> monogram
}));

function net(over: Partial<storage.Network> = {}): storage.Network {
  return storage.Network.createFrom({ id: 1, name: 'Libera Chat', ...over });
}

describe('NetworkTile', () => {
  it('renders the monogram when there is no custom icon', async () => {
    render(
      <NetworkTile network={net()} selected={false} connected connecting={false}
        unread={0} onSelect={() => {}} onContextMenu={() => {}} />,
    );
    expect(await screen.findByText('LC')).toBeInTheDocument();
  });

  it('shows an unread badge and fires onSelect', () => {
    const onSelect = vi.fn();
    render(
      <NetworkTile network={net()} selected={false} connected connecting={false}
        unread={7} onSelect={onSelect} onContextMenu={() => {}} />,
    );
    expect(screen.getByText('7')).toBeInTheDocument();
    fireEvent.click(screen.getByTestId('network-tile'));
    expect(onSelect).toHaveBeenCalledWith(1);
  });

  it('caps unread at 99+', () => {
    render(
      <NetworkTile network={net()} selected connected connecting={false}
        unread={150} onSelect={() => {}} onContextMenu={() => {}} />,
    );
    expect(screen.getByText('99+')).toBeInTheDocument();
  });

  it('renders an img when the network has a custom icon that resolves', async () => {
    vi.mocked(GetNetworkIcon).mockResolvedValueOnce('data:image/png;base64,AAAA');
    render(
      <NetworkTile network={net({ iconPath: '/icons/1.png' })} selected={false} connected connecting={false}
        unread={0} onSelect={() => {}} onContextMenu={() => {}} />,
    );
    const tile = screen.getByTestId('network-tile');
    await waitFor(() => {
      expect(tile.querySelector('img')).toHaveAttribute('src', 'data:image/png;base64,AAAA');
    });
    expect(screen.queryByText('LC')).not.toBeInTheDocument();
  });

  it('falls back to the monogram when the custom icon fetch rejects', async () => {
    vi.mocked(GetNetworkIcon).mockRejectedValueOnce(new Error('icon fetch failed'));
    render(
      <NetworkTile network={net({ iconPath: '/icons/1.png' })} selected={false} connected connecting={false}
        unread={0} onSelect={() => {}} onContextMenu={() => {}} />,
    );
    expect(await screen.findByText('LC')).toBeInTheDocument();
    expect(screen.getByTestId('network-tile').querySelector('img')).toBeNull();
  });
});
