import { describe, expect, it } from 'vitest';
import { main } from '../../wailsjs/go/models';
import { serializeNetworkForm } from './settings-network-form';

describe('serializeNetworkForm', () => {
  it('normalizes optional network and server fields for dirty-form comparisons', () => {
    const form = main.NetworkConfig.createFrom({ name: 'Libera', nickname: 'matt' });

    expect(JSON.parse(serializeNetworkForm(form, [{ address: 'irc.libera.chat', port: 6697, tls: true }]))).toEqual({
      name: 'Libera',
      nickname: 'matt',
      username: '',
      realname: '',
      password: '',
      sasl_enabled: false,
      sasl_mechanism: '',
      sasl_username: '',
      sasl_password: '',
      sasl_external_cert: '',
      auto_connect: false,
      identify_as_bot: false,
      servers: [{ address: 'irc.libera.chat', port: 6697, tls: true }],
    });
  });
});
