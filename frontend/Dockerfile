FROM python:3.12-slim
ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1
WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt
COPY . .
RUN DJANGO_SECRET_KEY=build python manage.py collectstatic --noinput
EXPOSE 8000
# Render gibi platformlar PORT verir; yoksa 8000.
CMD ["sh", "-c", "exec gunicorn unitech_web.wsgi:application --bind 0.0.0.0:${PORT:-8000} --workers 2 --timeout 30"]
