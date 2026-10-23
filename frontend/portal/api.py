"""Go API ile konuşan ince istemci."""
from datetime import datetime

import requests
from django.conf import settings

from .i18n import tr, translate_api_error

TIMEOUT = 10


class ApiError(Exception):
    def __init__(self, status, message):
        super().__init__(message)
        self.status = status
        self.message = message


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


def call(request, method, path, json=None, params=None, auth=True):
    lang = getattr(request, "LANG", "tr")
    headers = {"Accept-Language": lang}
    if auth:
        token = request.session.get("token")
        if not token:
            raise NeedLogin(401, tr(lang, "Devam etmek için giriş yap."))
        headers["Authorization"] = f"Bearer {token}"
    try:
        res = requests.request(
            method, settings.API_URL + path, json=json, params=params, headers=headers, timeout=TIMEOUT
        )
    except requests.RequestException:
        raise ApiError(503, tr(lang, "Sunucuya ulaşılamıyor. Biraz sonra tekrar dene."))
    try:
        data = res.json()
    except ValueError:
        data = {}
    err = data.get("error") if isinstance(data, dict) else None
    if res.status_code == 401 and auth:
        request.session.flush()
        raise NeedLogin(401, _sentence(translate_api_error(lang, err)) if err else tr(lang, "Oturumun kapandı, tekrar giriş yap."))
    if res.status_code >= 400:
        raise ApiError(res.status_code, _sentence(translate_api_error(lang, err)) if err else tr(lang, "Bir hata oluştu."))
    return _parse_dates(data)
