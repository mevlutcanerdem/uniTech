from django import template

from portal.constants import CATEGORIES, PRIORITIES, ROLES, STATUSES
from portal.i18n import BUILDINGS_EN, CATEGORIES_EN, tr

register = template.Library()


def _lang(context):
    req = context.get("request")
    return getattr(req, "LANG", "tr")


@register.simple_tag(takes_context=True)
def t(context, text, **kw):
    """{% t "Türkçe metin" %} veya {% t "{x} önce" x=deger %}"""
    return tr(_lang(context), text, **kw)


@register.filter
def category_label(v, lang="tr"):
    return CATEGORIES_EN.get(v, v) if lang == "en" else CATEGORIES.get(v, v)


@register.filter
def building_label(v, lang="tr"):
    return BUILDINGS_EN.get(v, v) if lang == "en" else v


@register.filter
def priority_label(v, lang="tr"):
    return tr(lang, PRIORITIES.get(v, v))


@register.filter
def status_label(v, lang="tr"):
    return tr(lang, STATUSES.get(v, v))


@register.filter
def role_label(v, lang="tr"):
    return tr(lang, ROLES.get(v, v))


@register.filter
def split(v):
    return str(v).split()


@register.filter
def pct(v, total):
    try:
        return round(100 * float(v) / float(total)) if float(total) else 0
    except (TypeError, ValueError):
        return 0


@register.filter
def duration(hours, lang="tr"):
    """Saat cinsinden süreyi okunur hale getirir: '1 saatten az', '5,3 sa', '2,1 gün'."""
    try:
        h = float(hours)
    except (TypeError, ValueError):
        return ""
    if h <= 0:
        return ""
    sep = "," if lang == "tr" else "."
    if h < 1:
        return tr(lang, "1 saatten az")
    if h < 48:
        return tr(lang, "{n} sa", n=f"{h:.1f}".replace(".", sep))
    return tr(lang, "{n} gün", n=f"{h / 24:.1f}".replace(".", sep))
