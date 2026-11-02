"""Go API ile konuşan ince istemci."""
from datetime import datetime

import requests
from django.conf import settings

from .i18n import tr, translate_api_error  # noqa: F401  (translate_api_error views'da da kullanılır)

TIMEOUT = 15
_session = requests.Session()  # bağlantıları yeniden kullanır


class ApiError(Exception):
    def __init__(self, status, message, code=""):
        super().__init__(message)
        self.status = status
        self.message = message
        self.code = code


class NeedLogin(ApiError):
    pass


def _parse_dates(obj):
    """API'den gelen ISO tarihleri datetime nesnesine çevirir."""
    if isinstance(obj, list):
        return [_parse_dates(x) for x in obj]
    if isinstance(obj, dict):
        out = {}
        for k, v in obj.items():
            if isinstance(v, str) and (k.endswith("_at") or k == "at"):
                try:
                    v = datetime.fromisoformat(v.replace("Z", "+00:00"))
                except ValueError:
                    pass
            out[k] = _parse_dates(v)
        return out
    return obj


def _sentence(msg):
    msg = msg.strip()
    return msg[:1].upper() + msg[1:] + ("" if msg.endswith(".") else ".")


def client_ip(request):
    """Render'ın eklediği X-Forwarded-For'un ilk değeri, yoksa doğrudan bağlantı adresi."""
    fwd = request.META.get("HTTP_X_FORWARDED_FOR", "")
    return fwd.split(",")[0].strip() if fwd else request.META.get("REMOTE_ADDR", "")


def _headers(request, auth):
    lang = getattr(request, "LANG", "tr")
    h = {"Accept-Language": lang}
    if settings.INTERNAL_API_KEY:
        # API hız sınırlarını sunucunun değil gerçek öğrencinin adresine uygulasın.
        h["X-Internal-Key"] = settings.INTERNAL_API_KEY
        h["X-Client-IP"] = client_ip(request)
    if auth:
        token = request.session.get("token")
        if not token:
            raise NeedLogin(401, tr(lang, "Devam etmek için giriş yap."))
        h["Authorization"] = f"Bearer {token}"
    return h


def _send(request, method, path, auth, timeout=TIMEOUT, **kw):
    headers = _headers(request, auth)
    try:
        return _session.request(method, settings.API_URL + path, headers=headers, timeout=timeout, **kw)
    except requests.RequestException:
        raise ApiError(503, tr(getattr(request, "LANG", "tr"), "Sunucuya ulaşılamıyor. Biraz sonra tekrar dene."), "unavailable")


def _raise_for(request, res, auth):
    lang = getattr(request, "LANG", "tr")
    try:
        data = res.json()
    except ValueError:
        data = {}
    err = data.get("error") if isinstance(data, dict) else None
    code = data.get("code", "") if isinstance(data, dict) else ""
    if res.status_code == 401 and auth:
        request.session.flush()
        raise NeedLogin(401, _sentence(translate_api_error(lang, err)) if err else tr(lang, "Oturumun kapandı, tekrar giriş yap."), code)
    raise ApiError(res.status_code, _sentence(translate_api_error(lang, err)) if err else tr(lang, "Bir hata oluştu."), code)


def call(request, method, path, json=None, params=None, files=None, auth=True, timeout=TIMEOUT):
    res = _send(request, method, path, auth, timeout=timeout, json=json, params=params, files=files)
    if res.status_code >= 400:
        _raise_for(request, res, auth)
    try:
        return _parse_dates(res.json())
    except ValueError:
        return {}


def fetch_raw(request, path):
    """Fotoğraf gibi ikili içerik: (bytes, content_type)."""
    res = _send(request, "GET", path, True)
    if res.status_code >= 400:
        _raise_for(request, res, True)
    return res.content, res.headers.get("Content-Type", "application/octet-stream")
