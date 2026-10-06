#!/bin/sh
# End-to-end tour of the running API with curl. Needs curl and jq.
#   make up && make demo        (API_URL / ADMIN_* override the defaults)
# Safe to re-run: it uses a random suffix for e-mails; CPFs are generated valid ones
# that the script deletes at the end. NOTE: it deliberately exhausts the per-IP auth
# rate limit (10/min) at the end, so wait a minute before running it again.
set -eu
BODYF=$(mktemp); trap 'rm -f "$BODYF"' EXIT
API="${API_URL:-http://localhost:${API_PORT:-8094}}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@example.com}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-dev-only-admin-password}"
S=$(date +%s)
J='Content-Type: application/json'

# status <expected> <label> <curl args...>: prints "<code> <label>", body on stdout of $BODY
call() { # call <label> <curl args...>
  label=$1; shift
  code=$(curl -s -o "$BODYF" -w '%{http_code}' "$@")
  printf '%-4s %s\n' "$code" "$label"
}
body() { cat "$BODYF"; echo; }

echo "== docs"
call "GET /docs/ (Swagger UI)" "$API/docs/"
call "GET /openapi.json" "$API/openapi.json"
call "GET /readyz" "$API/readyz"

echo "== auth"
call "register user" -H "$J" "$API/v1/auth/register" -d "{\"email\":\"demo$S@example.com\",\"password\":\"demo-password-1\"}"; body
call "register same e-mail again (409)" -H "$J" "$API/v1/auth/register" -d "{\"email\":\"demo$S@example.com\",\"password\":\"demo-password-1\"}"; body
call "register invalid (422)" -H "$J" "$API/v1/auth/register" -d '{"email":"nope","password":"x"}'; body
call "login user" -H "$J" "$API/v1/auth/login" -d "{\"email\":\"demo$S@example.com\",\"password\":\"demo-password-1\"}"
USER_TOKEN=$(jq -r .access_token "$BODYF"); REFRESH=$(jq -r .refresh_token "$BODYF")
call "login admin" -H "$J" "$API/v1/auth/login" -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}"
ADMIN_TOKEN=$(jq -r .access_token "$BODYF")
call "refresh (rotation)" -H "$J" "$API/v1/auth/refresh" -d "{\"refresh_token\":\"$REFRESH\"}"
call "refresh with the OLD token again (401, family revoked)" -H "$J" "$API/v1/auth/refresh" -d "{\"refresh_token\":\"$REFRESH\"}"

echo "== authorization"
call "GET /v1/customers without token (401)" "$API/v1/customers"; body
call "GET /v1/customers with garbage token (401)" -H "Authorization: Bearer abc.def.ghi" "$API/v1/customers"

echo "== customers CRUD"
# Valid CPFs are generated from a counter so re-runs do not collide.
cpf() { python3 - "$1" <<'PY'
import sys
b=f"{int(sys.argv[1]) % 1000000000:09d}"
def d(x,s):
    r=sum(int(c)*(s-i) for i,c in enumerate(x))%11
    return 11-r if r>=2 else 0
d1=d(b,10); d2=d(b+str(d1),11); print(b+str(d1)+str(d2))
PY
}
C1=$(cpf "$S"); C2=$(cpf "$((S+1))"); C3=$(cpf "$((S+2))")
mk() { printf '{"name":"%s","email":"%s","document":"%s","phone":"(31) 99999-0000","address":{"street":"Rua das Flores","number":"100","city":"Belo Horizonte","state":"MG","zip_code":"30130-000"}}' "$1" "$2" "$3"; }
AUTH="Authorization: Bearer $USER_TOKEN"
call "POST create Ana" -H "$AUTH" -H "$J" "$API/v1/customers" -d "$(mk "Ana Souza" "ana$S@example.com" "$C1")"; body
ID=$(jq -r .id "$BODYF")
call "POST create Bruno" -H "$AUTH" -H "$J" "$API/v1/customers" -d "$(mk "Bruno Lima" "bruno$S@example.com" "$C2")"; ID2=$(jq -r .id "$BODYF")
call "POST create Carla (inactive)" -H "$AUTH" -H "$J" "$API/v1/customers" -d "$(mk "Carla Dias" "carla$S@example.com" "$C3" | jq -c '. + {status:"inactive"}')"; ID3=$(jq -r .id "$BODYF")
call "POST duplicate e-mail (409)" -H "$AUTH" -H "$J" "$API/v1/customers" -d "$(mk "Outra Ana" "ana$S@example.com" "00000003700")"; body
call "POST duplicate document (409)" -H "$AUTH" -H "$J" "$API/v1/customers" -d "$(mk "Outra Ana" "outra$S@example.com" "$C1")"; body
call "POST invalid body (422)" -H "$AUTH" -H "$J" "$API/v1/customers" -d '{"name":"A","email":"x","document":"111.111.111-11"}'; body
call "POST malformed JSON (400)" -H "$AUTH" -H "$J" "$API/v1/customers" -d '{"name":'; body
call "GET by id" -H "$AUTH" "$API/v1/customers/$ID"
call "GET unknown id (404)" -H "$AUTH" "$API/v1/customers/00000000-0000-4000-8000-000000000000"; body
call "PUT full update" -H "$AUTH" -H "$J" -X PUT "$API/v1/customers/$ID" -d "$(mk "Ana Souza Costa" "ana$S@example.com" "$C1")"; jq -c '{name,status,updated_at}' "$BODYF"
call "PATCH partial update (phone + status)" -H "$AUTH" -H "$J" -X PATCH "$API/v1/customers/$ID" -d '{"phone":"31 3333-4444","status":"inactive"}'; jq -c '{name,phone,status}' "$BODYF"

echo "== list: pagination, filter, search"
call "GET ?page_size=2&page=1" -H "$AUTH" "$API/v1/customers?page_size=2&page=1"; jq -c '{page,page_size,total,names:[.data[].name]}' "$BODYF"
call "GET ?page_size=2&page=2" -H "$AUTH" "$API/v1/customers?page_size=2&page=2"; jq -c '{page,page_size,total,names:[.data[].name]}' "$BODYF"
call "GET ?status=inactive&q=$S" -H "$AUTH" "$API/v1/customers?status=inactive&q=$S"; jq -c '{total,names:[.data[].name]}' "$BODYF"
call "GET ?q=bruno$S" -H "$AUTH" "$API/v1/customers?q=bruno$S"; jq -c '{total,names:[.data[].name]}' "$BODYF"
call "GET ?page_size=1000 (422)" -H "$AUTH" "$API/v1/customers?page_size=1000"; jq -c '{code,errors}' "$BODYF"

echo "== roles: delete"
call "DELETE as plain user (403)" -H "$AUTH" -X DELETE "$API/v1/customers/$ID"; body
for i in "$ID" "$ID2" "$ID3"; do call "DELETE as admin" -H "Authorization: Bearer $ADMIN_TOKEN" -X DELETE "$API/v1/customers/$i"; done
call "GET after delete (404)" -H "$AUTH" "$API/v1/customers/$ID"

echo "== rate limit (auth endpoints: 10/min per IP)"
n=0; while [ $n -lt 14 ]; do
  code=$(curl -s -o "$BODYF" -w '%{http_code}' -H "$J" "$API/v1/auth/login" -d '{"email":"nobody@example.com","password":"wrong-password"}')
  printf '%s ' "$code"; n=$((n+1))
done; echo
curl -si -H "$J" "$API/v1/auth/login" -d '{}' | sed -n '1p;/^Retry-After/p;/^Content-Type/p;$p'
