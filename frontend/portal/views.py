from functools import wraps
from urllib.parse import urlencode

from django.contrib import messages
from django.http import HttpResponseForbidden
from django.shortcuts import redirect, render
from django.urls import reverse
from django.utils.http import url_has_allowed_host_and_scheme
from django.views.decorators.http import require_POST

from . import api
from .constants import BUILDINGS, CATEGORIES, PRIORITIES, ROLES, STATUSES
from .i18n import COOKIE, LANGS, tr

STAFF_ROLES = ("staff", "admin")


# ---- yardımcılar ----

def _(request, text, **kw):
    return tr(request.LANG, text, **kw)


def _session_user(u):
    """Çereze sadece gerekli alanları yaz."""
    return {k: u.get(k) for k in ("id", "name", "email", "role")}


def _login_redirect(request, staff=False):
    target = reverse("staff_login" if staff else "login")
    return redirect(f"{target}?{urlencode({'next': request.get_full_path()})}")


def _requires(roles=None, login_view_staff=False):
    """Oturum ve rol kontrolü; rol değişikliklerini yakalamak için kullanıcıyı her istekte tazeler."""

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
                return render(request, "portal/_error_page.html", {"message": e.message}, status=503)
            role = request.session["user"]["role"]
            if roles and role not in roles:
                msg = "Bu sayfa sadece yöneticiler içindir." if roles == ("admin",) else "Bu sayfa sadece teknik ekip içindir."
                return HttpResponseForbidden(_(request, msg))
            return view(request, *args, **kwargs)

        return wrapper

    return deco


login_required = _requires()
staff_required = _requires(STAFF_ROLES, login_view_staff=True)
admin_required = _requires(("admin",), login_view_staff=True)


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
    return "panel" if user.get("role") in STAFF_ROLES else "report"


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
                messages.success(request, _(request, "Hoş geldin, {name}! Hesabın oluşturuldu.", name=resp["user"]["name"]))
                return redirect(_home_for(resp["user"]))
            except api.ApiError as e:
                ctx["error"] = e.message
    return render(request, "auth/register.html", ctx)


@require_POST
def logout_view(request):
    was_staff = (request.session.get("user") or {}).get("role") in STAFF_ROLES
    request.session.flush()
    messages.info(request, _(request, "Çıkış yaptın."))
    return redirect("staff_login" if was_staff else "login")


# ---- öğrenci ----

@login_required
def report(request):
    form = {"category": "", "building": "", "room": "", "title": "", "description": "", "priority": "orta"}
    error = None
    if request.method == "POST":
        form = {k: request.POST.get(k, "").strip() for k in form}
        try:
            t = api.call(request, "POST", "/api/tickets", json=form)
            messages.success(request, _(request, "Bildirimin teknik ekibe iletildi. Takip numaran: {code}", code=t["code"]))
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
    return render(request, "portal/ticket.html", {"t": t, "statuses": STATUSES})


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

def _panel_data(request):
    f = {
        "status": request.GET.get("status", "acik"),
        "category": request.GET.get("category", ""),
        "assignee": request.GET.get("assignee", ""),
        "q": request.GET.get("q", "").strip(),
    }
    tickets = api.call(request, "GET", "/api/tickets", params=f)
    stats = api.call(request, "GET", "/api/stats")
    return {"filters": f, "tickets": tickets, "stats": stats, "categories": CATEGORIES, "statuses": STATUSES}


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


def _duration(request, hours):
    if hours <= 0:
        return None
    if hours < 1:
        return _(request, "1 saatten az")
    if hours < 48:
        return _(request, "{n} sa", n=f"{hours:.1f}".replace(".", "," if request.LANG == "tr" else "."))
    return _(request, "{n} gün", n=f"{hours / 24:.1f}".replace(".", "," if request.LANG == "tr" else "."))


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
        "avg": _duration(request, r.get("avg_resolve_hours") or 0),
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
