from django import template

from portal.constants import CATEGORIES, PRIORITIES, ROLES, STATUSES

register = template.Library()


@register.filter
def category_label(v):
    return CATEGORIES.get(v, v)


@register.filter
def priority_label(v):
    return PRIORITIES.get(v, v)


@register.filter
def status_label(v):
    return STATUSES.get(v, v)


@register.filter
def role_label(v):
    return ROLES.get(v, v)


@register.filter
def split(v):
    return str(v).split()
