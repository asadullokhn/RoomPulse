#!/bin/bash
# seed-academy.sh <db-path> — academy-scale demo data for local verification:
# 230 accounts (200 students + 30 staff) and a fully booked day
# (10 rooms x 10 bookings across 07:00-19:00). Never point this at production.
set -euo pipefail
DB="$1"
TODAY=$(date +%Y-%m-%d)
DAY_START=$(date -j -f "%Y-%m-%d %H:%M" "$TODAY 07:00" +%s)
WS=(ws-agung ws-bedugul ws-mengwi ws-nusadua ws-petang ws-sanur ws-ubud ws-ceningan ws-lembongan ws-penida)

{
  echo "BEGIN;"
  for i in $(seq 1 200); do
    printf "INSERT OR IGNORE INTO users (user_id, apple_sub, email, name, created_at) VALUES ('usr_s%03d','sub-s%03d','student%03d@academy.test','Student %03d',strftime('%%s','now'));\n" "$i" "$i" "$i" "$i"
  done
  for i in $(seq 1 30); do
    printf "INSERT OR IGNORE INTO users (user_id, apple_sub, email, name, created_at) VALUES ('usr_t%03d','sub-t%03d','staff%03d@academy.test','Staff %03d',strftime('%%s','now'));\n" "$i" "$i" "$i" "$i"
  done
  n=0
  for ws in "${WS[@]}"; do
    for h in 0 1 2 3 4 5 6 7 8 9; do
      n=$((n + 1))
      start=$((DAY_START + h * 4320)) # 72-minute pitch: 60-min booking + 12-min gap
      end=$((start + 3600))
      u=$(((n % 200) + 1))
      printf "INSERT OR REPLACE INTO app_reservations (reservation_id, room_id, zoom_workspace_id, booked_by_user_id, user_email, start_time, end_time, status, check_in_status) VALUES ('seed-%04d','room-%s','%s','usr_s%03d','student%03d@academy.test',%d,%d,'booked','not_checked_in');\n" "$n" "$ws" "$ws" "$u" "$u" "$start" "$end"
    done
  done
  echo "COMMIT;"
} | sqlite3 "$DB"

sqlite3 "$DB" "SELECT 'users: ' || count(*) FROM users; SELECT 'reservations: ' || count(*) FROM app_reservations;"
