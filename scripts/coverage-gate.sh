#!/bin/sh
# Usage: coverage-gate.sh coverage.out
# Fails when total coverage < 90% or a core package (customer, auth, validation,
# ratelimit) is below 100%. Needs only Go + awk.
set -eu
profile="$1"
TOTAL_MIN=90
CORE="internal/customer internal/auth internal/validation internal/ratelimit"

fail=0
total=$(go tool cover -func="$profile" | awk '/^total:/ {gsub("%","",$3); print $3}')
echo "total coverage: ${total}% (minimum ${TOTAL_MIN}%)"
awk -v t="$total" -v m="$TOTAL_MIN" 'BEGIN { exit !(t+0 >= m+0) }' || { echo "FAIL: total below ${TOTAL_MIN}%"; fail=1; }

for pkg in $CORE; do
  # statements covered / total per package, straight from the profile (mode: atomic/count/set)
  line=$(grep "/$pkg/" "$profile" | awk '{n[$1]=$2; c[$1]+=$3} END {s=0; h=0; for (k in n) {s+=n[k]; if (c[k]>0) h+=n[k]} printf "%d %d", h, s}')
  hit=${line% *}; stm=${line#* }
  pct=$(awk -v h="$hit" -v s="$stm" 'BEGIN { if (s==0) print 0; else printf "%.1f", h*100/s }')
  echo "$pkg: ${pct}% (${hit}/${stm} statements)"
  [ "$hit" = "$stm" ] && [ "$stm" != 0 ] || { echo "FAIL: $pkg is below 100%"; fail=1; }
done
exit $fail
