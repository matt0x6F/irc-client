import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { NetworkTile } from '../network-tile';
import { storage } from '../../../wailsjs/go/models';

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
});
