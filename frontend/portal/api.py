"""Go API ile konuşan ince istemci."""
from datetime import datetime

import requests
from django.conf import settings

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


def call(request, method, path, json=None, params=None, auth=True):
    headers = {}
    if auth:
        token = request.session.get("token")
        if not token:
            raise NeedLogin(401, "Devam etmek için giriş yap.")
        headers["Authorization"] = f"Bearer {token}"
    try:
        res = requests.request(
            method, settings.API_URL + path, json=json, params=params, headers=headers, timeout=TIMEOUT
        )
    except requests.RequestException:
        raise ApiError(503, "Sunucuya ulaşılamıyor. Biraz sonra tekrar dene.")
    try:
        data = res.json()
    except ValueError:
        data = {}
    if res.status_code == 401 and auth:
        request.session.flush()
        raise NeedLogin(401, data.get("error", "Oturumun kapandı, tekrar giriş yap."))
    if res.status_code >= 400:
        msg = data.get("error", "Bir hata oluştu.") if isinstance(data, dict) else "Bir hata oluştu."
        raise ApiError(res.status_code, msg[:1].upper() + msg[1:] + ("" if msg.endswith(".") else "."))
    return _parse_dates(data)
