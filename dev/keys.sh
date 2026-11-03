#!/bin/sh
# Makes the local stand-in for PARADOX's key pair, once. The private half
# stays in dev/keys/ (gitignored); ROSETTA gets the public half from
# dev/keys/paradox.env through dev/compose.yaml.
set -eu
cd "$(dirname "$0")/.."
mkdir -p dev/keys
[ -f dev/keys/paradox.key ] && { echo "dev/keys/paradox.key exists"; exit 0; }
pub=$(go run ./cmd/rosetta-paradoxcall keygen -out dev/keys/paradox.key)
printf 'PARADOX_PUBLIC_KEY=%s\n' "$pub" > dev/keys/paradox.env
echo "made dev/keys/paradox.key and dev/keys/paradox.env"
