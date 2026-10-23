"""uniTech arayüzü (Django). Veritabanı kullanmaz; tüm veri Go API'den gelir."""
import os
from pathlib import Path

BASE_DIR = Path(__file__).resolve().parent.parent

SECRET_KEY = os.environ.get("DJANGO_SECRET_KEY", "dev-only-degistir")
DEBUG = os.environ.get("DJANGO_DEBUG", "1") == "1"
ALLOWED_HOSTS = [h.strip() for h in os.environ.get("DJANGO_ALLOWED_HOSTS", "localhost,127.0.0.1").split(",") if h.strip()]
CSRF_TRUSTED_ORIGINS = [o.strip() for o in os.environ.get("DJANGO_CSRF_TRUSTED_ORIGINS", "").split(",") if o.strip()]

# Sunucunun Go API'ye eriştiği adres (Docker'da http://api:8080).
API_URL = os.environ.get("API_URL", "http://localhost:8080").rstrip("/")
# Tarayıcının canlı panel akışı için bağlandığı adres.
API_PUBLIC_URL = os.environ.get("API_PUBLIC_URL", API_URL).rstrip("/")

INSTALLED_APPS = [
    "django.contrib.sessions",
    "django.contrib.messages",
    "django.contrib.staticfiles",
    "portal",
]

MIDDLEWARE = [
    "django.middleware.security.SecurityMiddleware",
    "whitenoise.middleware.WhiteNoiseMiddleware",
    "django.contrib.sessions.middleware.SessionMiddleware",
    "django.middleware.common.CommonMiddleware",
    "django.middleware.csrf.CsrfViewMiddleware",
    "portal.i18n.LanguageMiddleware",
    "django.contrib.messages.middleware.MessageMiddleware",
    "django.middleware.clickjacking.XFrameOptionsMiddleware",
]

ROOT_URLCONF = "unitech_web.urls"
WSGI_APPLICATION = "unitech_web.wsgi.application"

TEMPLATES = [
    {
        "BACKEND": "django.template.backends.django.DjangoTemplates",
        "DIRS": [BASE_DIR / "templates"],
        "APP_DIRS": True,
        "OPTIONS": {
            "context_processors": [
                "django.template.context_processors.request",
                "django.contrib.messages.context_processors.messages",
                "portal.context.current_user",
            ],
        },
    },
]

DATABASES = {}

# Oturum imzalı çerezde tutulur (veritabanı gerekmez).
SESSION_ENGINE = "django.contrib.sessions.backends.signed_cookies"
SESSION_COOKIE_HTTPONLY = True
SESSION_COOKIE_AGE = 7 * 24 * 3600
SESSION_COOKIE_SECURE = not DEBUG
CSRF_COOKIE_SECURE = not DEBUG
MESSAGE_STORAGE = "django.contrib.messages.storage.session.SessionStorage"

LANGUAGE_CODE = "tr"
LANGUAGES = [("tr", "Türkçe"), ("en", "English")]
TIME_ZONE = "Europe/Istanbul"
USE_I18N = True
USE_TZ = True

STATIC_URL = "static/"
STATICFILES_DIRS = [BASE_DIR / "static"]
STATIC_ROOT = BASE_DIR / "staticfiles"

try:  # whitenoise yoksa (ör. sadece geliştirme) sessizce devre dışı bırak
    import whitenoise  # noqa: F401
except ImportError:
    MIDDLEWARE.remove("whitenoise.middleware.WhiteNoiseMiddleware")

# Render/Vercel gibi HTTPS sonlandıran bir proxy arkasında çalışırken.
SECURE_PROXY_SSL_HEADER = ("HTTP_X_FORWARDED_PROTO", "https")
