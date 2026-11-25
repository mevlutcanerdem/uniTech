#!/bin/sh
# Tek servis başlatıcı: Django (iç) + Go API (dış). Biri durursa kapsayıcı yeniden başlar.
set -e
PORT="${PORT:-8080}"
export ADDR=":$PORT"
# Django, API'ye aynı kapsayıcı içinden ulaşır.
export API_URL="http://127.0.0.1:$PORT"

gunicorn unitech_web.wsgi:application \
  --bind 127.0.0.1:8001 --workers "${WEB_WORKERS:-1}" --threads "${WEB_THREADS:-4}" \
  --timeout 60 --access-logfile - --forwarded-allow-ips='*' &
WEB=$!

unitech-api &
API=$!

trap 'kill -TERM $WEB $API 2>/dev/null' TERM INT
# İkisinden biri çıkarsa diğerini de kapat; platform kapsayıcıyı yeniden başlatır.
while kill -0 $WEB 2>/dev/null && kill -0 $API 2>/dev/null; do sleep 2; done
kill -TERM $WEB $API 2>/dev/null || true
wait
exit 1
