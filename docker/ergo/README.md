# Ergo IRC Test Server

This directory contains the configuration for a local Ergo IRC server that can be used to test the Cascade Chat client.

## Quick Start

1. **Generate TLS certificates** (required for TLS port):
   ```bash
   mkdir -p docker/ergo/certs
   openssl req -nodes -new -x509 -days 365 \
     -keyout docker/ergo/certs/privkey.pem \
     -out docker/ergo/certs/fullchain.pem \
     -subj "/CN=localhost"
   ```

2. **Start the server**:
   ```bash
   docker-compose up -d
   ```

3. **View logs**:
   ```bash
   docker-compose logs -f ergo
   ```

4. **Stop the server**:
   ```bash
   docker-compose down
   ```

## Connection Details

- **Plaintext IRC**: `localhost:6667` (no TLS)
- **TLS IRC**: `localhost:6697` (TLS enabled)

## Testing with Cascade Chat

1. Start the Ergo server using the steps above
2. In Cascade Chat, add a new server:
   - **Name**: Test Server
   - **Address**: `localhost`
   - **Port**: `6667` (plaintext) or `6697` (TLS)
   - **TLS**: Enable if using port 6697
   - **Nickname**: Choose any nickname
   - **Username**: Choose any username
   - **Realname**: Your real name

3. Connect and test features:
   - Join channels: `/join #test`
   - Register nickname: `/msg NickServ register <password> <email>`
   - Register channel: `/msg ChanServ register #test`
   - Test SASL authentication (if configured)

## Configuration

The server configuration is in `docker/ergo/config/ircd.yaml`. You can modify this file and restart the container to apply changes:

```bash
docker-compose restart ergo
```

## Data Persistence

Server data (registered nicks, channels, etc.) is stored in a Docker volume (`ergo_data`) and persists across container restarts. To completely reset:

```bash
docker-compose down -v
```

This will remove the volume and all data.

## Notes

- The TLS certificate is self-signed, so your client may show a certificate warning. This is normal for local testing.
- The server uses in-memory history (last 100 messages per channel, 24 hours max age).
- NickServ and ChanServ are enabled for nickname and channel registration.
