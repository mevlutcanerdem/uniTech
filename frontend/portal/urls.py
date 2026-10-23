from django.urls import path

from . import views

urlpatterns = [
    path("", views.index, name="index"),
    path("dil/<str:code>/", views.set_language, name="set_language"),
    path("giris/", views.login_view, name="login"),
    path("ekip/giris/", views.staff_login_view, name="staff_login"),
    path("kayit/", views.register_view, name="register"),
    path("cikis/", views.logout_view, name="logout"),
    path("bildir/", views.report, name="report"),
    path("bildirimlerim/", views.mine, name="mine"),
    path("ariza/<int:ticket_id>/", views.ticket, name="ticket"),
    path("ariza/<int:ticket_id>/guncelle/", views.ticket_update, name="ticket_update"),
    path("panel/", views.panel, name="panel"),
    path("panel/liste/", views.panel_list, name="panel_list"),
    path("raporlar/", views.reports, name="reports"),
    path("kullanicilar/", views.users, name="users"),
    path("kullanicilar/<int:user_id>/rol/", views.set_role, name="set_role"),
]
