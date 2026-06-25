import { describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { InputArea } from './input-area';
import { useCommandsStore } from '../stores/commands';
import { main } from '../../wailsjs/go/models';

vi.mock('../../wailsjs/go/main/App', () => ({
  GetChannelInfo: vi.fn().mockResolvedValue({ users: [] }),
  SendTyping: vi.fn().mockResolvedValue(undefined),
}));

const mk = (name: string, usage = '', description = ''): main.CommandInfo =>
  ({ name, aliases: [], category: 'server', usage, description, source: '' } as main.CommandInfo);

describe('InputArea command autocomplete', () => {
  beforeEach(() => {
    useCommandsStore.setState({ commands: [mk('JOIN', '#channel [key]', 'Join a channel'), mk('QUIT', '[reason]', 'Disconnect')] });
  });

  it('shows a popup of matching commands when typing a slash command', () => {
    render(<InputArea onSendMessage={vi.fn()} networkId={1} channelName="#x" />);
    const input = screen.getByTestId('message-input');
    fireEvent.change(input, { target: { value: '/j' } });
    expect(screen.getByText('join')).toBeInTheDocument();
    expect(screen.queryByText('quit')).not.toBeInTheDocument();
  });

  it('shows the usage hint after the command name', () => {
    render(<InputArea onSendMessage={vi.fn()} networkId={1} channelName="#x" />);
    const input = screen.getByTestId('message-input');
    fireEvent.change(input, { target: { value: '/join ' } });
    expect(screen.getByText(/#channel \[key\]/)).toBeInTheDocument();
  });
});
