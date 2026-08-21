#!/bin/bash

set -euo pipefail

gate_name=${1:?gate name is required}
selected=${2-}
selection_result=${3-}
verification_result=${4-}

if [[ "$selection_result" != "success" ]]; then
  echo "$gate_name selection failed with result: $selection_result" >&2
  exit 1
fi

if [[ "$selected" == "true" ]]; then
  if [[ "$verification_result" != "success" ]]; then
    echo "$gate_name verification failed with result: $verification_result" >&2
    exit 1
  fi
  echo "$gate_name completed successfully."
  exit 0
fi

if [[ "$selected" != "false" ]]; then
  echo "$gate_name selection returned an invalid value: $selected" >&2
  exit 1
fi

if [[ "$verification_result" != "skipped" ]]; then
  echo "$gate_name was not selected but verification result was: $verification_result" >&2
  exit 1
fi

echo "$gate_name did not run because no registered path changed."
