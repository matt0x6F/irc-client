import { main } from '../../wailsjs/go/models';

export interface NetworkServerForm {
  address: string;
  port: number;
  tls: boolean;
}

// Canonical form snapshot used to detect unsaved network edits. Wails models
// may omit optional values, so normalize them before comparing snapshots.
export function serializeNetworkForm(
  form: main.NetworkConfig,
  servers: NetworkServerForm[],
): string {
  return JSON.stringify({
    name: form.name ?? '',
    nickname: form.nickname ?? '',
    username: form.username ?? '',
    realname: form.realname ?? '',
    password: form.password ?? '',
    sasl_enabled: form.sasl_enabled ?? false,
    sasl_mechanism: form.sasl_mechanism ?? '',
    sasl_username: form.sasl_username ?? '',
    sasl_password: form.sasl_password ?? '',
    sasl_external_cert: form.sasl_external_cert ?? '',
    auto_connect: form.auto_connect ?? false,
    identify_as_bot: form.identify_as_bot ?? false,
    servers: (servers ?? []).map((server) => ({
      address: server.address ?? '',
      port: server.port ?? 6667,
      tls: server.tls ?? false,
    })),
  });
}
