from functools import wraps
from urllib.parse import urlencode

from django.contrib import messages
from django.http import Http404, HttpResponse, HttpResponseForbidden
from django.shortcuts import redirect, render
from django.urls import reverse
from django.utils.http import url_has_allowed_host_and_scheme
from django.views.decorators.http import require_POST

from . import api
from .constants import BUILDINGS, CATEGORIES, PRIORITIES, ROLES, STATUSES
from .i18n import COOKIE, LANGS, tr
from .templatetags.portal_tags import duration

STAFF_ROLES = ("staff", "admin")
MAX_PHOTOS = 3
MAX_PHOTO_BYTES = 8 * 1024 * 1024
PHOTO_TYPES = ("image/jpeg", "image/png")


# ---- yardımcılar ----

def _(request, text, **kw):
    return tr(request.LANG, text, **kw)


def _session_user(u):
    """Çereze sadece gerekli alanları yaz."""
    return {k: u.get(k) for k in ("id", "name", "email", "role", "verified")}


def _login_redirect(request, staff=False):
    target = reverse("staff_login" if staff else "login")
    return redirect(f"{target}?{urlencode({'next': request.get_full_path()})}")


def _requires(roles=None, login_view_staff=False, need_verified=False):
    """Oturum, rol ve e-posta doğrulama kontrolü; rol değişikliklerini yakalamak için kullanıcıyı her istekte tazeler."""

    def deco(view):
        @wraps(view)
        def wrapper(request, *args, **kwargs):
            if "token" not in request.session:
                return _login_redirect(request, login_view_staff)
            try:
                request.session["user"] = _session_user(api.call(request, "GET", "/api/me"))
            except api.NeedLogin as e:
                messages.info(request, e.message)
                return _login_redirect(request, login_view_staff)
            except api.ApiError as e:
                return _unavailable(request, e)
            user = request.session["user"]
            if roles and user["role"] not in roles:
                msg = "Bu sayfa sadece yöneticiler içindir." if roles == ("admin",) else "Bu sayfa sadece teknik ekip içindir."
                return HttpResponseForbidden(_(request, msg))
            if (need_verified or roles) and not user.get("verified", True):
                messages.info(request, _(request, "Arıza bildirmeden önce e-posta adresini doğrulaman gerekiyor."))
                return redirect(f"{reverse('verify')}?{urlencode({'next': request.get_full_path()})}")
            return view(request, *args, **kwargs)

        return wrapper

    return deco


login_required = _requires()
verified_required = _requires(need_verified=True)
staff_required = _requires(STAFF_ROLES, login_view_staff=True)
admin_required = _requires(("admin",), login_view_staff=True)


def _unavailable(request, e):
    """API uykudaysa (ücretsiz sunucu) kullanıcıyı hata yerine kendiliğinden yenilenen bir bekleme sayfasına al."""
    waking = e.code == "unavailable" or e.status in (502, 503, 504)
    return render(request, "portal/_error_page.html", {"message": e.message, "waking": waking}, status=503)


def _safe_next(request, default):
    nxt = request.POST.get("next") or request.GET.get("next")
    if nxt and url_has_allowed_host_and_scheme(nxt, allowed_hosts={request.get_host()}):
        return nxt
    return default


def _start_session(request, resp):
    request.session.cycle_key()
    request.session["token"] = resp["token"]
    request.session["user"] = _session_user(resp["user"])


def _home_for(user):
    if not user.get("verified", True):
        return "verify"
    return "panel" if user.get("role") in STAFF_ROLES else "report"


def _public_stats(request):
    """Giriş sayfasındaki sayılar. API uykudaysa sayfayı bekletmez, sayıları göstermez."""
    try:
        return api.call(request, "GET", "/api/public/stats", auth=False, timeout=3)
    except api.ApiError:
        return None


# ---- dil ----

def set_language(request, code):
    resp = redirect(_safe_next(request, request.META.get("HTTP_REFERER") or reverse("index")))
    if code in LANGS:
        resp.set_cookie(COOKIE, code, max_age=365 * 24 * 3600, samesite="Lax")
    return resp


# ---- giriş / kayıt ----

def index(request):
    user = request.session.get("user")
    return redirect(_home_for(user) if user else "login")


def _login(request, template, staff_only):
    if request.session.get("user"):
        return redirect(_home_for(request.session["user"]))
    ctx = {"email": "", "next": request.GET.get("next", "")}
    if request.method == "POST":
        ctx["email"] = request.POST.get("email", "")
        try:
            resp = api.call(request, "POST", "/api/auth/login", auth=False,
                            json={"email": ctx["email"], "password": request.POST.get("password", "")})
            if staff_only and resp["user"]["role"] not in STAFF_ROLES:
                ctx["error"] = _(request, "Bu giriş sadece teknik ekip içindir. Öğrenciysen öğrenci girişini kullan.")
            else:
                _start_session(request, resp)
                return redirect(_safe_next(request, reverse(_home_for(resp["user"]))))
        except api.ApiError as e:
            ctx["error"] = e.message
    if not staff_only:
        ctx["pub"] = _public_stats(request)
    return render(request, template, ctx)


def login_view(request):
    return _login(request, "auth/login.html", staff_only=False)


def staff_login_view(request):
    return _login(request, "auth/staff_login.html", staff_only=True)


def register_view(request):
    ctx = {"name": "", "email": ""}
    if request.method == "POST":
        ctx.update(name=request.POST.get("name", ""), email=request.POST.get("email", ""))
        if request.POST.get("password") != request.POST.get("password2"):
            ctx["error"] = _(request, "Şifreler aynı değil.")
        else:
            try:
                resp = api.call(request, "POST", "/api/auth/register", auth=False,
                                json={"name": ctx["name"], "email": ctx["email"], "password": request.POST.get("password", "")})
                _start_session(request, resp)
                if resp.get("verification_required"):
                    if not resp.get("code_sent"):
                        messages.error(request, _(request, "Kod şu an gönderilemedi. Biraz sonra “Yeni kod gönder”e bas."))
                    return redirect("verify")
                messages.success(request, _(request, "Hoş geldin, {name}! Hesabın oluşturuldu.", name=resp["user"]["name"]))
                return redirect(_home_for(resp["user"]))
            except api.ApiError as e:
                ctx["error"] = e.message
    return render(request, "auth/register.html", ctx)


@login_required
def verify(request):
    user = request.session["user"]
    if user.get("verified"):
        return redirect(_safe_next(request, reverse(_home_for(user))))
    ctx = {"next": request.GET.get("next", "")}
    if request.method == "POST":
        try:
            if "resend" in request.POST:
                r = api.call(request, "POST", "/api/auth/verify/resend", json={})
                if r.get("sent"):
                    messages.success(request, _(request, "Yeni kod gönderildi."))
                return redirect(request.get_full_path())
            u = api.call(request, "POST", "/api/auth/verify", json={"code": request.POST.get("code", "").strip()})
            request.session["user"] = _session_user(u)
            messages.success(request, _(request, "E-postan doğrulandı. Artık arıza bildirebilirsin."))
            return redirect(_safe_next(request, reverse(_home_for(u))))
        except api.ApiError as e:
            ctx["error"] = e.message
    return render(request, "auth/verify.html", ctx)


def forgot(request):
    ctx = {"email": request.GET.get("email", "")}
    if request.method == "POST":
        ctx["email"] = request.POST.get("email", "").strip()
        try:
            api.call(request, "POST", "/api/auth/forgot", json={"email": ctx["email"]}, auth=False)
            request.session["reset_email"] = ctx["email"]
            messages.info(request, _(request, "Bu adrese kayıtlı bir hesap varsa sıfırlama kodu gönderildi."))
            return redirect("reset")
        except api.ApiError as e:
            ctx["error"] = e.message
    return render(request, "auth/forgot.html", ctx)


def reset(request):
    ctx = {"email": request.session.get("reset_email", "")}
    if request.method == "POST":
        ctx["email"] = request.POST.get("email", "").strip()
        if request.POST.get("password") != request.POST.get("password2"):
            ctx["error"] = _(request, "Şifreler aynı değil.")
        else:
            try:
                resp = api.call(request, "POST", "/api/auth/reset", auth=False, json={
                    "email": ctx["email"], "code": request.POST.get("code", "").strip(),
                    "password": request.POST.get("password", "")})
                _start_session(request, resp)
                messages.success(request, _(request, "Şifren değiştirildi. Diğer cihazlardaki oturumların kapatıldı."))
                return redirect(_home_for(resp["user"]))
            except api.ApiError as e:
                ctx["error"] = e.message
    return render(request, "auth/reset.html", ctx)


@require_POST
def logout_view(request):
    was_staff = (request.session.get("user") or {}).get("role") in STAFF_ROLES
    request.session.flush()
    messages.info(request, _(request, "Çıkış yaptın."))
    return redirect("staff_login" if was_staff else "login")


# ---- öğrenci ----

def _photo_files(request):
    """Geçerli fotoğraflar ve atlananların sebepleri."""
    files, skipped = [], []
    for f in request.FILES.getlist("photos"):
        if len(files) >= MAX_PHOTOS:
            skipped.append(_(request, "En fazla 3 fotoğraf seçebilirsin."))
            break
        if f.size > MAX_PHOTO_BYTES:
            skipped.append(api.translate_api_error(request.LANG, "fotoğraf çok büyük (en fazla 8 MB)"))
            continue
        if (f.content_type or "").lower() not in PHOTO_TYPES:
            skipped.append(api.translate_api_error(request.LANG, "sadece JPEG veya PNG fotoğraf yüklenebilir"))
            continue
        files.append(f)
    return files, skipped


def _upload_photos(request, ticket_id, files):
    """Fotoğrafları yükler; başarısızların sebeplerini döner."""
    failed = []
    for f in files:
        try:
            api.call(request, "POST", f"/api/tickets/{ticket_id}/photos",
                     files={"photo": (f.name or "photo.jpg", f.read(), f.content_type)})
        except api.ApiError as e:
            failed.append(e.message.rstrip("."))
    return failed


def _report_failures(request, reasons):
    if reasons:
        messages.error(request, _(request, "{n} fotoğraf yüklenemedi: {reason}", n=len(reasons), reason=reasons[0]))


@verified_required
def report(request):
    form = {"category": "", "building": "", "room": "", "title": "", "description": "", "priority": "orta"}
    error = None
    if request.method == "POST":
        form = {k: request.POST.get(k, "").strip() for k in form}
        files, skipped = _photo_files(request)
        try:
            t = api.call(request, "POST", "/api/tickets", json=form)
            messages.success(request, _(request, "Bildirimin teknik ekibe iletildi. Takip numaran: {code}", code=t["code"]))
            _report_failures(request, skipped + _upload_photos(request, t["id"], files))
            return redirect("ticket", ticket_id=t["id"])
        except api.ApiError as e:
            error = e.message
    return render(request, "portal/report.html", {
        "form": form, "error": error, "categories": CATEGORIES, "priorities": PRIORITIES, "buildings": BUILDINGS,
    })


@login_required
def mine(request):
    tickets = []
    try:
        tickets = api.call(request, "GET", "/api/tickets", params={"scope": "mine"})
    except api.ApiError as e:
        messages.error(request, e.message)
    return render(request, "portal/mine.html", {"tickets": tickets})


@login_required
def ticket(request, ticket_id):
    try:
        t = api.call(request, "GET", f"/api/tickets/{ticket_id}")
    except api.ApiError as e:
        if e.status == 404:
            return render(request, "portal/not_found.html", status=404)
        messages.error(request, e.message)
        return redirect("mine")
    me = request.session["user"]
    can_add_photo = (len(t.get("photos") or []) < MAX_PHOTOS and me.get("verified", True)
                     and (me["role"] in STAFF_ROLES or t.get("reporter_id") == me["id"]))
    return render(request, "portal/ticket.html", {"t": t, "statuses": STATUSES, "can_add_photo": can_add_photo})


@verified_required
@require_POST
def ticket_photo(request, ticket_id):
    files, skipped = _photo_files(request)
    failed = skipped + _upload_photos(request, ticket_id, files[:1])
    if failed:
        _report_failures(request, failed)
    elif files:
        messages.success(request, _(request, "Fotoğraf eklendi."))
    return redirect("ticket", ticket_id=ticket_id)


@login_required
def photo(request, photo_id):
    try:
        data, ctype = api.fetch_raw(request, f"/api/photos/{photo_id}")
    except api.ApiError:
        raise Http404
    resp = HttpResponse(data, content_type=ctype)
    resp["Cache-Control"] = "private, max-age=31536000, immutable"
    resp["X-Content-Type-Options"] = "nosniff"
    return resp


@staff_required
@require_POST
def ticket_update(request, ticket_id):
    body = {
        "status": request.POST.get("status", ""),
        "note": request.POST.get("note", "").strip(),
        "assign": request.POST.get("assign", ""),
    }
    try:
        t = api.call(request, "PATCH", f"/api/tickets/{ticket_id}", json=body)
        if body["status"]:
            messages.success(request, _(request, "{code} güncellendi: {status}.", code=t["code"],
                                        status=_(request, STATUSES.get(t["status"], t["status"]))))
        else:
            messages.success(request, _(request, "{code} güncellendi.", code=t["code"]))
    except api.ApiError as e:
        messages.error(request, e.message)
    return redirect(_safe_next(request, reverse("ticket", args=[ticket_id])))


# ---- teknik ekip ----

PANEL_PAGE = 40


def _panel_data(request):
    f = {
        "status": request.GET.get("status", "acik"),
        "category": request.GET.get("category", ""),
        "assignee": request.GET.get("assignee", ""),
        "q": request.GET.get("q", "").strip(),
    }
    try:
        shown = max(PANEL_PAGE, min(int(request.GET.get("n", PANEL_PAGE)), 1000))
    except ValueError:
        shown = PANEL_PAGE
    # Bir fazlasını iste: varsa "daha fazla göster" bağlantısı çıkar.
    tickets = api.call(request, "GET", "/api/tickets", params={**f, "limit": shown + 1})
    stats = api.call(request, "GET", "/api/stats")
    more_url = None
    if len(tickets) > shown:
        tickets = tickets[:shown]
        q = request.GET.copy()
        q["n"] = shown + PANEL_PAGE
        more_url = reverse("panel") + "?" + q.urlencode()
    return {"filters": f, "tickets": tickets, "stats": stats, "categories": CATEGORIES, "statuses": STATUSES,
            "more_url": more_url}


def _tab_url(request, assignee):
    q = request.GET.copy()
    q.pop("assignee", None)
    if assignee:
        q["assignee"] = assignee
    s = q.urlencode()
    return reverse("panel") + (f"?{s}" if s else "")


@staff_required
def panel(request):
    try:
        ctx = _panel_data(request)
    except api.ApiError as e:
        messages.error(request, e.message)
        ctx = {"filters": {}, "tickets": [], "stats": {}, "categories": CATEGORIES, "statuses": STATUSES}
    ctx["stream_token"] = request.session.get("token")
    ctx["tabs"] = [
        ("", _(request, "Tümü"), _tab_url(request, "")),
        ("me", _(request, "Bana atananlar"), _tab_url(request, "me")),
        ("none", _(request, "Atanmamış"), _tab_url(request, "none")),
    ]
    return render(request, "portal/panel.html", ctx)


@staff_required
def panel_list(request):
    """Canlı akıştan sonra panelin yalnızca listesini ve sayaçlarını yeniden çizer."""
    try:
        ctx = _panel_data(request)
    except api.ApiError as e:
        return render(request, "portal/_error.html", {"message": e.message}, status=502)
    qs = request.GET.urlencode()
    ctx["return_to"] = reverse("panel") + (f"?{qs}" if qs else "")
    return render(request, "portal/_panel_body.html", ctx)


@staff_required
def reports(request):
    days = 30 if request.GET.get("days") == "30" else 14
    try:
        r = api.call(request, "GET", "/api/reports", params={"days": days})
    except api.ApiError as e:
        messages.error(request, e.message)
        return redirect("panel")
    daily = r.get("daily") or []
    peak = max([max(d["created"], d["resolved"]) for d in daily] + [1])
    step = 2 if days == 14 else 5
    for i, d in enumerate(daily):
        d["h_created"] = round(100 * d["created"] / peak)
        d["h_resolved"] = round(100 * d["resolved"] / peak)
        d["label"] = d["date"][8:10].lstrip("0") if (i % step == 0 or i == len(daily) - 1) else ""
    max_cat = max([c["count"] for c in r.get("by_category") or []] + [1])
    max_bld = max([c["count"] for c in r.get("by_building") or []] + [1])
    totals = r.get("totals") or {}
    return render(request, "portal/reports.html", {
        "r": r, "days": days, "daily": daily, "peak": peak, "max_cat": max_cat, "max_bld": max_bld,
        "open_count": (totals.get("yeni") or 0) + (totals.get("islemde") or 0),
        "avg": duration(r.get("avg_resolve_hours") or 0, request.LANG) or None,
    })


# ---- yönetici ----

@admin_required
def users(request):
    items = []
    try:
        items = api.call(request, "GET", "/api/users")
    except api.ApiError as e:
        messages.error(request, e.message)
    items.sort(key=lambda u: ({"admin": 0, "staff": 1}.get(u["role"], 2), u["name"].lower()))
    return render(request, "portal/users.html", {"users": items, "roles": ROLES})


@admin_required
@require_POST
def set_role(request, user_id):
    try:
        u = api.call(request, "PATCH", f"/api/users/{user_id}", json={"role": request.POST.get("role", "")})
        messages.success(request, _(request, "{name} artık {role}.", name=u["name"], role=_(request, ROLES.get(u["role"], u["role"]))))
    except api.ApiError as e:
        messages.error(request, e.message)
    return redirect("users")
