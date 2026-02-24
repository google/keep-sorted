#!/usr/bin/env bash
# Copyright 2023 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Regenerates the golden .out file for the provided .in files.

set -euo pipefail
[[ -n "${DEBUG:-}" ]] && set -x

dir="$(dirname "$(realpath "${BASH_SOURCE[0]}")")"
git_dir="$(git -C "${dir}" rev-parse --show-toplevel)"

for i in "$@"; do
  out="${i%%in}out"
  err="${i%%in}err"
  diff="${i%%in}diff"

  go run "${git_dir}" --id=keep-sorted-test --omit-timestamps - <"${i}" >"${out}" 2>"${err}" \
    || true  # Ignore any non-zero exit codes.
  if (( $(wc -l < "${err}") == 0 )); then
    rm "${err}"
  fi

  go run "${git_dir}" --id=keep-sorted-test --mode=diff --omit-timestamps - <"${i}" >"${diff}" 2>/dev/null \
    || true  # Ignore any non-zero exit codes.
done
