from django.conf import settings


def current_user(request):
    user = request.session.get("user")
    return {
        "me": user,
        "is_staff": bool(user and user.get("role") in ("staff", "admin")),
        "is_admin": bool(user and user.get("role") == "admin"),
        "API_PUBLIC_URL": settings.API_PUBLIC_URL,
    }
