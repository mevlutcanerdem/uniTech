FROM python:3.14-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 PIP_NO_CACHE_DIR=1 PIP_DISABLE_PIP_VERSION_CHECK=1
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY . .
RUN DJANGO_SECRET_KEY=build DJANGO_DEBUG=0 python manage.py collectstatic --noinput \
    && useradd --uid 10001 --no-create-home app
USER app
EXPOSE 8000
# Render gibi platformlar PORT verir; yoksa 8000. İş parçacıklı işçiler: API beklenirken diğer istekler sırada kalmaz.
CMD ["sh", "-c", "exec gunicorn unitech_web.wsgi:application --bind 0.0.0.0:${PORT:-8000} --workers 2 --threads 4 --timeout 60 --access-logfile - --forwarded-allow-ips='*'"]
