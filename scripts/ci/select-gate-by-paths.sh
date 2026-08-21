#!/bin/bash

set -euo pipefail

gate_name=${1:?gate name is required}
shift

if [ "$#" -eq 0 ]; then
  echo "$gate_name gate has no registered paths." >&2
  exit 2
fi

event_name=${ADDP_CI_EVENT:?ADDP_CI_EVENT is required}
head_revision=${ADDP_CI_HEAD:?ADDP_CI_HEAD is required}
github_output=${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}

select_gate() {
  echo "run=true" >> "$github_output"
  echo "$gate_name gate selected: $1."
  exit 0
}

if [[ "$event_name" == "schedule" || "$event_name" == "workflow_dispatch" ]]; then
  select_gate "$event_name event"
fi

if [[ "$event_name" == "pull_request" ]]; then
  diff_base=${ADDP_CI_PR_BASE:?ADDP_CI_PR_BASE is required for pull requests}
elif [[ "$event_name" == "push" && -n "${ADDP_CI_BEFORE:-}" && "$ADDP_CI_BEFORE" != "0000000000000000000000000000000000000000" ]]; then
  diff_base=$ADDP_CI_BEFORE
elif git rev-parse --verify "$head_revision^" >/dev/null 2>&1; then
  diff_base="$head_revision^"
else
  select_gate "no parent revision is available"
fi

while IFS= read -r changed_path; do
  for registered_pattern in "$@"; do
    if [[ "$changed_path" == $registered_pattern ]]; then
      select_gate "$changed_path matches $registered_pattern"
    fi
  done
done < <(git diff --name-only "$diff_base...$head_revision")

echo "run=false" >> "$github_output"
echo "$gate_name gate skipped: no registered path changed."
