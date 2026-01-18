#!/bin/bash
# Generate self-signed TLS certificates for Ergo IRC server

set -e

CERT_DIR="$(dirname "$0")/certs"

# Create certs directory if it doesn't exist
mkdir -p "$CERT_DIR"

# Generate self-signed certificate
echo "Generating self-signed TLS certificate for localhost..."
openssl req -nodes -new -x509 -days 365 \
  -keyout "$CERT_DIR/privkey.pem" \
  -out "$CERT_DIR/fullchain.pem" \
  -subj "/CN=localhost"

echo "Certificate generated successfully!"
echo "Private key: $CERT_DIR/privkey.pem"
echo "Certificate: $CERT_DIR/fullchain.pem"
echo ""
echo "Note: This is a self-signed certificate for testing only."
echo "Your IRC client may show a certificate warning - this is normal."
