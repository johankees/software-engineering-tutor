#!/usr/bin/env bash
# Settings as code for this repository (see docs/secure-delivery/github-repositories.md).
#   github-settings.sh apply   change GitHub to match this file and .github/rulesets/
#   github-settings.sh check   read GitHub and fail on drift (read-only)
# Needs `gh` logged in as a repository admin.
set -euo pipefail

mode="${1:?usage: github-settings.sh apply|check}"
repo="${GITHUB_REPOSITORY:-$(gh repo view --json nameWithOwner --jq .nameWithOwner)}"
rulesets_dir="$(dirname "$0")/../.github/rulesets"

# The repository settings we want. jq compares them in "check".
want_repo='{
  "visibility": "public",
  "allow_squash_merge": true,
  "allow_merge_commit": false,
  "allow_rebase_merge": false,
  "allow_auto_merge": true,
  "delete_branch_on_merge": true,
  "squash_merge_commit_title": "PR_TITLE",
  "squash_merge_commit_message": "PR_BODY",
  "security_and_analysis": {
    "secret_scanning": { "status": "enabled" },
    "secret_scanning_push_protection": { "status": "enabled" }
  }
}'
# Only GitHub's own actions plus these, each pinned by SHA in the workflows.
want_actions='{
  "github_owned_allowed": true,
  "verified_allowed": false,
  "patterns_allowed": ["jdx/mise-action@*", "ossf/scorecard-action@*", "step-security/harden-runner@*"]
}'

ruleset_id() {
  local all
  all="$(gh api "repos/$repo/rulesets" 2>/dev/null)" || return 0
  printf '%s' "$all" | jq -r --arg name "$1" '.[] | select(.name == $name) | .id'
}

# Sort rules and status checks so that order does not count as drift.
sort_ruleset='.rules |= (sort_by(.type) | map(if .parameters.required_status_checks
  then .parameters.required_status_checks |= sort_by(.context) else . end))'

# Print every scalar of $1 (wanted JSON) whose value differs in $2 (GitHub's JSON).
# Fields that GitHub adds and we don't set are ignored.
differences() {
  jq -n -c --argjson want "$1" --argjson got "$2" '
    [$want | paths(scalars)] | map(. as $p
      | {setting: ($p | map(tostring) | join(".")), want: ($want | getpath($p)), got: ($got | getpath($p))})
    | map(select(.want != .got)) | .[]'
}

apply() {
  # Visibility first: rulesets, secret scanning and Pages need a public repository on this plan.
  gh repo edit "$repo" --visibility public --accept-visibility-change-consequences
  printf '%s' "$want_repo" | jq 'del(.visibility)' | gh api --method PATCH "repos/$repo" --input - >/dev/null

  gh api --method PUT "repos/$repo/vulnerability-alerts" >/dev/null
  gh api --method PUT "repos/$repo/automated-security-fixes" >/dev/null
  gh api --method PUT "repos/$repo/private-vulnerability-reporting" >/dev/null
  gh api --method PUT "repos/$repo/immutable-releases" >/dev/null
  gh api --method PATCH "repos/$repo/code-scanning/default-setup" -f state=configured -f query_suite=default >/dev/null

  gh api --method PUT "repos/$repo/actions/permissions" \
    -F enabled=true -f allowed_actions=selected -F sha_pinning_required=true >/dev/null
  printf '%s' "$want_actions" | gh api --method PUT "repos/$repo/actions/permissions/selected-actions" --input - >/dev/null
  gh api --method PUT "repos/$repo/actions/permissions/workflow" \
    -f default_workflow_permissions=read -F can_approve_pull_request_reviews=false >/dev/null
  gh api --method PUT "repos/$repo/actions/permissions/fork-pr-contributor-approval" \
    -f approval_policy=all_external_contributors >/dev/null

  if ! gh api "repos/$repo/pages" >/dev/null 2>&1; then
    gh api --method POST "repos/$repo/pages" -f build_type=workflow >/dev/null
  fi

  for file in "$rulesets_dir"/*.json; do
    name="$(jq -r .name "$file")"
    id="$(ruleset_id "$name")"
    if [ -n "$id" ]; then
      gh api --method PUT "repos/$repo/rulesets/$id" --input "$file" >/dev/null
    else
      gh api --method POST "repos/$repo/rulesets" --input "$file" >/dev/null
    fi
    echo "ruleset $name: applied"
  done
}

check() {
  local drift=0 diffs got file name id want
  diffs="$(differences "$want_repo" "$(gh api "repos/$repo")")"
  if [ -n "$diffs" ]; then
    printf 'DRIFT repository %s\n' $diffs >&2
    drift=1
  fi
  diffs="$(differences '{"allowed_actions": "selected", "sha_pinning_required": true}' "$(gh api "repos/$repo/actions/permissions")")"
  if [ -n "$diffs" ]; then
    printf 'DRIFT actions %s\n' $diffs >&2
    drift=1
  fi
  for file in "$rulesets_dir"/*.json; do
    name="$(jq -r .name "$file")"
    id="$(ruleset_id "$name")"
    if [ -z "$id" ]; then
      echo "DRIFT ruleset $name: missing" >&2
      drift=1
      continue
    fi
    want="$(jq -c "del(.bypass_actors) | $sort_ruleset" "$file")"
    got="$(gh api "repos/$repo/rulesets/$id" | jq -c "$sort_ruleset")"
    diffs="$(differences "$want" "$got")"
    if [ -n "$diffs" ]; then
      printf "DRIFT ruleset $name %s\n" $diffs >&2
      drift=1
    fi
  done
  if [ "$drift" -eq 0 ]; then
    echo "github settings: no drift"
  fi
  return "$drift"
}

case "$mode" in
  apply) apply ;;
  check) check ;;
  *) echo "usage: github-settings.sh apply|check" >&2; exit 2 ;;
esac
