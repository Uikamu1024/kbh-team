#!/usr/bin/env bash
# 起動済みのバックエンドAPIサーバーに対する簡易スモークテスト。
# 実装を大きく変更したときの手動確認代わりに使う（tests/README.md参照）。
#
# 前提：
#   - go run ./cmd/server が別途起動済みであること
#   - jq がインストールされていること
#
# 使い方：
#   ./tests/smoke.sh
#   BASE_URL=http://localhost:9090 ./tests/smoke.sh

set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:8080}"
FAILURES=0

pass() { echo "OK   $1"; }
fail() { echo "FAIL $1"; FAILURES=$((FAILURES + 1)); }

expect_status() {
  local label="$1" expected="$2" actual="$3"
  if [ "$actual" = "$expected" ]; then
    pass "$label ($actual)"
  else
    fail "$label (expected $expected, got $actual)"
  fi
}

echo "== smoke test against $BASE_URL =="

# 1. health
health_status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/health")
if [ "$health_status" = "200" ] || [ "$health_status" = "503" ]; then
  pass "GET /api/health responded ($health_status; 503はVOICEVOX未起動時の想定内)"
else
  fail "GET /api/health unexpected status $health_status"
fi

# 2. create user
create_resp=$(curl -s -X POST "$BASE_URL/api/users")
user_id=$(echo "$create_resp" | jq -r '.userId')
if [ -n "$user_id" ] && [ "$user_id" != "null" ]; then
  pass "POST /api/users -> userId=$user_id"
else
  fail "POST /api/users did not return userId: $create_resp"
  echo "$FAILURES failure(s)"
  exit 1
fi

# 3. tags
tags_status=$(curl -s -o /dev/null -w "%{http_code}" -X PUT "$BASE_URL/api/users/$user_id/tags" \
  -H "Content-Type: application/json" -d '{"tags":["AI"]}')
expect_status "PUT tags" 204 "$tags_status"

# 4. settings
settings_status=$(curl -s -o /dev/null -w "%{http_code}" -X PUT "$BASE_URL/api/users/$user_id/settings" \
  -H "Content-Type: application/json" -d '{"deliveryTime":"06:00","lengthMinutes":5}')
expect_status "PUT settings" 204 "$settings_status"

# 5. invalid tags -> 400
invalid_status=$(curl -s -o /dev/null -w "%{http_code}" -X PUT "$BASE_URL/api/users/$user_id/tags" \
  -H "Content-Type: application/json" -d '{"tags":[]}')
expect_status "PUT tags (invalid, expect 400)" 400 "$invalid_status"

# 6. profile
profile_status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/users/$user_id")
expect_status "GET profile" 200 "$profile_status"

# 7. demo/generate（DB非永続化。記事キャッシュ（cmd/ingestで事前投入）とマッチする
#    タグが必要。タグはconfig/tags.jsonの許可集合を使うこと（frontend/src/config/tags.json
#    と1文字違わず一致させる必要がある。詳細はdocs/api-contract.yamlのx-open-questions参照）
demo_resp=$(curl -s -X POST "$BASE_URL/api/demo/generate" \
  -H "Content-Type: application/json" -d '{"tags":["生成AI"]}')
demo_program_id=$(echo "$demo_resp" | jq -r '.id')
demo_chapter_id=$(echo "$demo_resp" | jq -r '.chapters[0].id // empty')
if [ -n "$demo_program_id" ] && [ "$demo_program_id" != "null" ]; then
  pass "POST /api/demo/generate -> programId=$demo_program_id"
else
  fail "POST /api/demo/generate unexpected response: $demo_resp"
fi

if [ -n "$demo_chapter_id" ]; then
  audio_status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/audio/$demo_program_id/$demo_chapter_id")
  expect_status "GET audio (demo)" 200 "$audio_status"
fi

# 8. 存在しないユーザー -> 404
notfound_status=$(curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/api/users/00000000-0000-0000-0000-000000000000")
expect_status "GET profile (not found)" 404 "$notfound_status"

echo "=================================="
if [ "$FAILURES" -eq 0 ]; then
  echo "all checks passed"
  exit 0
else
  echo "$FAILURES check(s) failed"
  exit 1
fi
