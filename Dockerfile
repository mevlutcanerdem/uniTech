# Tek servis: Go API ön kapıda ($PORT), Django aynı kapsayıcıda 127.0.0.1:8001'de.
# /api/* isteklerini API karşılar, geri kalanı Django'ya iletilir.
# Ayrı servisler için backend/Dockerfile ve frontend/Dockerfile hâlâ kullanılabilir.

FROM golang:1.27-alpine AS api
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download && go mod verify
COPY backend/*.go ./
COPY backend/migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /unitech-api .

FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
WORKDIR /app
COPY frontend/requirements.txt .
RUN pip install -r requirements.txt
COPY frontend/ .
RUN DJANGO_SECRET_KEY=build DJANGO_DEBUG=0 python manage.py collectstatic --noinput \
    && useradd --uid 10001 --no-create-home app
COPY --from=api /unitech-api /usr/local/bin/unitech-api
COPY start.sh /usr/local/bin/start.sh
USER app
ENV FRONTEND_PROXY=http://127.0.0.1:8001 API_PUBLIC_URL= DATA_PATH=/tmp/unitech.gob
EXPOSE 8080
CMD ["sh", "/usr/local/bin/start.sh"]
