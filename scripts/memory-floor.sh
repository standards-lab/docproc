#!/usr/bin/env bash
# Runs a tool on one input under each RLIMIT_AS bound, set by util-linux's prlimit before exec,
# and prints each bound's passes as a Markdown table and the floor.
# See docs/ci.md
set -euo pipefail

if [ "$#" -lt 4 ]; then
  echo "usage: memory-floor.sh <binary> <input> <runs> <MiB>..." >&2
  exit 2
fi
binary=$1
input=$2
runs=$3
shift 3

command -v prlimit >/dev/null || {
  echo "memory-floor: prlimit is not on PATH" >&2
  exit 2
}
[ -x "$binary" ] || {
  echo "memory-floor: $binary is not an executable" >&2
  exit 2
}
[ -f "$input" ] || {
  echo "memory-floor: $input is not a file" >&2
  exit 2
}

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "| RLIMIT_AS | Passed | Failures |"
echo "| --- | --- | --- |"
floor=
for mib in "$@"; do
  passed=0
  : >"$work/failures"
  for _ in $(seq "$runs"); do
    status=$({
      prlimit --as=$((mib * 1024 * 1024)) -- "$binary" "$input" >/dev/null 2>"$work/stderr"
      echo "$?"
    } 2>/dev/null)
    if [ "$status" -eq 0 ]; then
      passed=$((passed + 1))
    else
      last=$(tail -n 1 "$work/stderr" | tr '|' '/' | cut -c 1-120)
      echo "exit $status, ${last:-no stderr}" >>"$work/failures"
    fi
  done
  failures=$(sort "$work/failures" | uniq -c | sed 's/^ *\([0-9]*\) /\1 × /' | paste -sd ';' - | sed 's/;/; /g')
  echo "| $mib MiB | $passed of $runs | ${failures:-none} |"
  if [ "$passed" -eq "$runs" ]; then
    [ -n "$floor" ] || floor=$mib
  else
    floor=
  fi
done
echo
if [ -n "$floor" ]; then
  echo "Floor: $floor MiB, the lowest bound from which every bound up passed $runs of $runs."
else
  echo "Floor: none; the highest bound did not pass $runs of $runs."
  exit 1
fi
