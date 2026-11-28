#!/usr/bin/env sh
# Usage: verify-signatures.sh <base> <head>
# Fails if any commit in base..head is not signed by a trusted key
# whose principal matches the committer email.
# Explained in docs/secure-delivery/signed-commits.md.
set -eu
base="$1" head="$2"
bad=0
commits="$(git rev-list --no-merges "$base..$head")"
for c in $commits; do
  status="$(git log -1 --no-show-signature --format='%G?' "$c")"
  signer="$(git log -1 --no-show-signature --format='%GS' "$c")"
  email="$(git log -1 --no-show-signature --format='%ce' "$c")"
  if [ "$status" != "G" ]; then
    echo "FAIL $c: signature status $status" >&2
    bad=1
  elif [ "$signer" != "$email" ]; then
    echo "FAIL $c: signed by $signer, committer is $email" >&2
    bad=1
  else
    echo "ok   $c $signer"
  fi
done
exit "$bad"
