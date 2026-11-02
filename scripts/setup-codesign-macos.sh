#!/usr/bin/env bash
# One-time setup of a local self-signed code-signing identity for xIssue.
#
# macOS TCC keys file-access grants to the app's code signature. Rebuilding an
# ad-hoc-signed Go binary changes its cdhash, so every update looks like a new
# app. This creates a stable certificate named "xIssue Codesign"; afterwards
# `make build` signs bin/xissue with it and permissions survive updates.
#
# The trust step may show a macOS authorization dialog; approve it once.
set -euo pipefail

[ "$(uname -s)" = "Darwin" ] || { echo "macOS only"; exit 0; }

identity="${XISSUE_CODESIGN_IDENTITY:-xIssue Codesign}"
keychain="$HOME/Library/Keychains/login.keychain-db"

if security find-identity -v -p codesigning 2>/dev/null | grep -qF "$identity"; then
  echo "✓ signing identity \"$identity\" already available"
  exit 0
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

cat > "$work/ext.cnf" <<'EOF'
[req]
distinguished_name=dn
x509_extensions=v3
prompt=no
[dn]
CN=xIssue Codesign
O=xIssue Local
[v3]
basicConstraints=critical,CA:false
keyUsage=critical,digitalSignature
extendedKeyUsage=critical,codeSigning
EOF

openssl req -x509 -newkey rsa:2048 -nodes -keyout "$work/key.pem" -out "$work/cert.pem" \
  -days 3650 -config "$work/ext.cnf" >/dev/null 2>&1
# macOS security cannot read modern PKCS#12 encryption; export legacy (SHA1/3DES).
openssl pkcs12 -export -legacy -macalg sha1 -inkey "$work/key.pem" -in "$work/cert.pem" \
  -out "$work/id.p12" -passout pass:xissue >/dev/null 2>&1

security import "$work/id.p12" -k "$keychain" -P xissue -T /usr/bin/codesign -T /usr/bin/security >/dev/null
echo "✓ imported \"$identity\" into login keychain"

# Mark it trusted for code signing (user domain). A GUI auth prompt is expected.
security add-trusted-cert -r trustRoot -p codeSign -k "$keychain" "$work/cert.pem"

if security find-identity -v -p codesigning 2>/dev/null | grep -qF "$identity"; then
  echo "✓ identity ready: $identity"
  echo "  Next build signs automatically; macOS will ask for folder access one last time."
else
  echo "✗ identity still not trusted. Open Keychain Access → find \"$identity\" →" >&2
  echo "  Get Info → Trust → set \"Code Signing\" to \"Always Trust\"." >&2
  exit 1
fi
