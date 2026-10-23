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


# ---- yardımcılar ----

def _session_user(u):
    """Çereze sadece gerekli alanları yaz."""
    return {k: u.get(k) for k in ("id", "name", "email", "role")}


def _login_redirect(request):
    return redirect(f"{reverse('login')}?{urlencode({'next': request.get_full_path()})}")


def login_required(view):
    """Oturum yoksa girişe yollar; rol değişikliklerini yakalamak için kullanıcıyı tazeler."""

    @wraps(view)
    def wrapper(request, *args, **kwargs):
        if "token" not in request.session:
            return _login_redirect(request)
        try:
            request.session["user"] = _session_user(api.call(request, "GET", "/api/me"))
            return view(request, *args, **kwargs)
        except api.NeedLogin as e:
            messages.info(request, e.message)
            return _login_redirect(request)

    return wrapper


def staff_required(view):
    @login_required
    @wraps(view)
    def wrapper(request, *args, **kwargs):
        if request.session["user"]["role"] not in ("staff", "admin"):
            return HttpResponseForbidden("Bu sayfa sadece teknik ekip içindir.")
        return view(request, *args, **kwargs)

    return wrapper


def admin_required(view):
    @login_required
    @wraps(view)
    def wrapper(request, *args, **kwargs):
        if request.session["user"]["role"] != "admin":
            return HttpResponseForbidden("Bu sayfa sadece yöneticiler içindir.")
        return view(request, *args, **kwargs)

    return wrapper


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
    return "panel" if user.get("role") in ("staff", "admin") else "report"


# ---- giriş / kayıt ----

def index(request):
    user = request.session.get("user")
    if user:
        return redirect(_home_for(user))
    return redirect("login")


def login_view(request):
    if request.session.get("user"):
        return redirect(_home_for(request.session["user"]))
    ctx = {"email": "", "next": request.GET.get("next", "")}
    if request.method == "POST":
        ctx["email"] = request.POST.get("email", "")
        try:
            resp = api.call(request, "POST", "/api/auth/login", auth=False,
                            json={"email": ctx["email"], "password": request.POST.get("password", "")})
            _start_session(request, resp)
            return redirect(_safe_next(request, reverse(_home_for(resp["user"]))))
        except api.ApiError as e:
            ctx["error"] = e.message
    return render(request, "auth/login.html", ctx)


def register_view(request):
    ctx = {"name": "", "email": ""}
    if request.method == "POST":
        ctx.update(name=request.POST.get("name", ""), email=request.POST.get("email", ""))
        if request.POST.get("password") != request.POST.get("password2"):
            ctx["error"] = "Şifreler aynı değil."
        else:
            try:
                resp = api.call(request, "POST", "/api/auth/register", auth=False,
                                json={"name": ctx["name"], "email": ctx["email"], "password": request.POST.get("password", "")})
                _start_session(request, resp)
                messages.success(request, f"Hoş geldin, {resp['user']['name']}! Hesabın oluşturuldu.")
                return redirect(_home_for(resp["user"]))
            except api.ApiError as e:
                ctx["error"] = e.message
    return render(request, "auth/register.html", ctx)


@require_POST
def logout_view(request):
    request.session.flush()
    messages.info(request, "Çıkış yaptın.")
    return redirect("login")


# ---- öğrenci ----

@login_required
def report(request):
    form = {"category": "", "building": "", "room": "", "title": "", "description": "", "priority": "orta"}
    error = None
    if request.method == "POST":
        form = {k: request.POST.get(k, "").strip() for k in form}
        try:
            t = api.call(request, "POST", "/api/tickets", json=form)
            messages.success(request, f"Bildirimin teknik ekibe iletildi. Takip numaran: {t['code']}")
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
    body = {"status": request.POST.get("status", ""), "note": request.POST.get("note", "").strip()}
    try:
        t = api.call(request, "PATCH", f"/api/tickets/{ticket_id}", json=body)
        messages.success(request, f"{t['code']} güncellendi: {STATUSES.get(t['status'])}.")
    except api.ApiError as e:
        messages.error(request, e.message)
    return redirect(_safe_next(request, reverse("ticket", args=[ticket_id])))


# ---- teknik ekip ----

def _panel_data(request):
    f = {
        "status": request.GET.get("status", "acik"),
        "category": request.GET.get("category", ""),
        "q": request.GET.get("q", "").strip(),
    }
    tickets = api.call(request, "GET", "/api/tickets", params=f)
    stats = api.call(request, "GET", "/api/stats")
    return {"filters": f, "tickets": tickets, "stats": stats, "categories": CATEGORIES, "statuses": STATUSES}


@staff_required
def panel(request):
    try:
        ctx = _panel_data(request)
    except api.ApiError as e:
        messages.error(request, e.message)
        ctx = {"filters": {}, "tickets": [], "stats": {}, "categories": CATEGORIES, "statuses": STATUSES}
    ctx["stream_token"] = request.session.get("token")
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
        messages.success(request, f"{u['name']} artık {ROLES.get(u['role'])}.")
    except api.ApiError as e:
        messages.error(request, e.message)
    return redirect("users")
