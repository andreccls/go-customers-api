#!/bin/sh
# Usage: coverage-gate.sh coverage.out
# Fails when total statement coverage < 90% or a core package is below its minimum.
# Core packages must be 100% except internal/auth (98%): its only uncovered statement is
# the `SignedString` error return, which cannot fail with an HS256 key (see README).
set -eu
profile="$1"
TOTAL_MIN=90
CORE="internal/customer:100 internal/validation:100 internal/ratelimit:100 internal/auth:98"

fail=0
total=$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%","",$3); print $3}')
echo "total coverage: ${total}% (minimum ${TOTAL_MIN}%)"
awk -v t="$total" -v m="$TOTAL_MIN" 'BEGIN { exit !(t+0 >= m+0) }' || { echo "FAIL: total below ${TOTAL_MIN}%"; fail=1; }

for entry in $CORE; do
  pkg=${entry%:*}; min=${entry#*:}
  # With -coverpkg every test binary writes its own copy of each block: merge by block
  # position and count a block as covered if ANY binary hit it.
  line=$(grep "/$pkg/" "$profile" | awk '{n[$1]=$2; c[$1]+=$3} END {s=0; h=0; for (k in n) {s+=n[k]; if (c[k]>0) h+=n[k]} printf "%d %d", h, s}')
  hit=${line% *}; stm=${line#* }
  pct=$(awk -v h="$hit" -v s="$stm" 'BEGIN { if (s==0) print 0; else printf "%.1f", h*100/s }')
  echo "$pkg: ${pct}% (${hit}/${stm} statements, minimum ${min}%)"
  awk -v p="$pct" -v m="$min" 'BEGIN { exit !(p+0 >= m+0) }' || { echo "FAIL: $pkg is below ${min}%"; fail=1; }
done
exit $fail
