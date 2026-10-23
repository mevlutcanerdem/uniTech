"""Türkçe / İngilizce arayüz metinleri.

Kaynak dil Türkçe. Şablonlarda {% t "Türkçe metin" %} kullanılır; İngilizce seçiliyse
EN sözlüğündeki karşılığı gösterilir. Yeni bir metin eklerken EN'e de ekle
(frontend/check_i18n.py eksikleri listeler).
"""
from django.utils import translation

LANGS = ("tr", "en")
COOKIE = "lang"


def get_lang(request):
    lang = request.COOKIES.get(COOKIE)
    if lang in LANGS:
        return lang
    accept = request.META.get("HTTP_ACCEPT_LANGUAGE", "").lower()
    if accept.startswith("en"):
        return "en"
    return "tr"


def tr(lang, text, **kw):
    s = EN.get(text, text) if lang == "en" else text
    return s.format(**kw) if kw else s


class LanguageMiddleware:
    def __init__(self, get_response):
        self.get_response = get_response

    def __call__(self, request):
        request.LANG = get_lang(request)
        translation.activate(request.LANG)  # tarih ve "timesince" biçimleri için
        response = self.get_response(request)
        response["Content-Language"] = request.LANG
        return response


# ---- API'den gelen hata mesajları (Go tarafı Türkçe döner) ----
API_ERRORS_EN = {
    "geçerli bir e-posta adresi gir": "Enter a valid email address",
    "sadece @agu.edu.tr uzantılı okul e-postasıyla kayıt olunabilir": "You can only sign up with an @agu.edu.tr school email",
    "bu e-posta ile zaten bir hesap var": "An account with this email already exists",
    "ad soyad gerekli (en fazla 80 karakter)": "Full name is required (max 80 characters)",
    "şifre en az 8 karakter olmalı": "Password must be at least 8 characters",
    "e-posta veya şifre hatalı": "Incorrect email or password",
    "çok fazla hatalı deneme, 15 dakika sonra tekrar dene": "Too many failed attempts. Try again in 15 minutes",
    "geçersiz oturum": "Your session is invalid. Please sign in again",
    "oturumun süresi doldu, tekrar giriş yap": "Your session expired. Please sign in again",
    "hesap bulunamadı": "Account not found",
    "bu işlem sadece teknik ekip içindir": "Only the maintenance team can do this",
    "bu işlem sadece yöneticiler içindir": "Only administrators can do this",
    "arıza bulunamadı": "Issue not found",
    "kullanıcı bulunamadı": "User not found",
    "geçersiz durum": "Invalid status",
    "geçersiz atama": "Invalid assignment",
    "geçersiz rol": "Invalid role",
    "not en fazla 500 karakter olabilir": "Notes can be at most 500 characters",
    "durum, not veya atama gerekli": "Choose a status, add a note or change the assignee",
    "kendi yönetici yetkini kaldıramazsın": "You can't remove your own administrator role",
    "istek gövdesi okunamadı": "The request couldn't be read",
    "arıza kaydedilemedi": "The issue couldn't be saved",
    "hesap oluşturulamadı": "The account couldn't be created",
    "güncellenemedi": "The update couldn't be saved",
    "eksik veya hatalı alan": "Missing or invalid fields",
}
FIELDS_EN = {
    "arıza türü": "issue type", "aciliyet": "urgency", "bina": "building", "kat / oda": "floor / room",
    "kısa başlık": "short title", "açıklama": "description",
}


def translate_api_error(lang, msg):
    if lang != "en":
        return msg
    if ":" in msg:
        head, rest = msg.split(":", 1)
        if head.strip() in API_ERRORS_EN:
            fields = ", ".join(FIELDS_EN.get(f.strip(), f.strip()) for f in rest.split(","))
            return f"{API_ERRORS_EN[head.strip()]}: {fields}"
    return API_ERRORS_EN.get(msg, msg)


# ---- Arayüz metinleri ----
EN = {
    # genel
    "AGÜ Teknik Destek": "AGÜ Facilities Support",
    "Ekip": "Team",
    "Ana menü": "Main menu",
    "Arıza bildir": "Report an issue",
    "Bildirimlerim": "My reports",
    "Teknik ekip paneli": "Team dashboard",
    "Panel": "Dashboard",
    "Bana atananlar": "Assigned to me",
    "Raporlar": "Reports",
    "Kullanıcılar": "Users",
    "Çıkış yap": "Sign out",
    "Dil": "Language",
    "Abdullah Gül Üniversitesi": "Abdullah Gül University",
    "Acil durumlarda kampüs güvenliğini arayın.": "In an emergency, call campus security.",
    "Sunucuya ulaşılamıyor. Biraz sonra tekrar dene.": "Can't reach the server. Try again in a moment.",
    "Devam etmek için giriş yap.": "Sign in to continue.",
    "Oturumun kapandı, tekrar giriş yap.": "You were signed out. Please sign in again.",
    "Bir hata oluştu.": "Something went wrong.",
    "Listeye dön": "Back to list",
    "{x} önce": "{x} ago",
    "Sen": "You",
    "Kaydet": "Save",

    # roller, durumlar, aciliyet
    "Öğrenci": "Student",
    "Teknik ekip": "Maintenance team",
    "Yönetici": "Administrator",
    "Yeni": "New",
    "İşlemde": "In progress",
    "Çözüldü": "Resolved",
    "Düşük": "Low",
    "Orta": "Medium",
    "Acil": "Urgent",

    # giriş / kayıt
    "Giriş": "Sign in",
    "Kayıt ol": "Sign up",
    "Kampüsteki arızalar, tek tıkla teknik ekipte.": "Campus issues, straight to the maintenance team.",
    "Bozuk projeksiyon, kopan Wi‑Fi, damlayan musluk. Gördüğün sorunu bildir, çözülene kadar adım adım takip et.":
        "A broken projector, dropping Wi‑Fi, a leaking tap. Report what you see and follow it until it's fixed.",
    "Saniyeler içinde bildir": "Report in seconds",
    "Arıza türünü, yerini ve aciliyetini seç, gönder.": "Pick the type, place and urgency, then send.",
    "Ekip anında görür": "The team sees it instantly",
    "Bildirimin teknik ekibin paneline canlı olarak düşer.": "Your report appears live on the team's dashboard.",
    "Durumu takip et": "Track the status",
    "Yeni, işlemde, çözüldü. Ekibin notlarını da gör.": "New, in progress, resolved. See the team's notes too.",
    "Tekrar hoş geldin": "Welcome back",
    "Okul e-postanla giriş yap.": "Sign in with your school email.",
    "Okul e-postası": "School email",
    "Şifre": "Password",
    "Giriş yap": "Sign in",
    "Hesabın yok mu?": "Don't have an account?",
    "Okul e-postanla kayıt ol": "Sign up with your school email",
    "Teknik ekipten misin?": "On the maintenance team?",
    "Ekip girişi": "Team sign-in",
    "Hesap oluştur": "Create an account",
    "Sadece @agu.edu.tr uzantılı okul e-postasıyla kayıt olunabilir.": "Only @agu.edu.tr school emails can sign up.",
    "Ad soyad": "Full name",
    "En az 8 karakter": "At least 8 characters",
    "Şifre tekrar": "Repeat password",
    "Zaten hesabın var mı?": "Already have an account?",
    "Şifreler aynı değil.": "The passwords don't match.",
    "Hoş geldin, {name}! Hesabın oluşturuldu.": "Welcome, {name}! Your account is ready.",
    "Çıkış yaptın.": "You signed out.",

    # ekip girişi
    "Teknik Ekip Girişi": "Maintenance Team Sign-in",
    "Teknik ekip": "Maintenance team",
    "Arızaları tek panelden yönet.": "Manage every issue from one dashboard.",
    "Öğrencilerin bildirdiği arızalar anında burada. Üzerine al, durumunu güncelle, raporları izle.":
        "Student reports land here instantly. Take them on, update their status, follow the reports.",
    "Canlı panel": "Live dashboard",
    "Yeni arızalar sayfa yenilenmeden gelir, acil olanlar en üstte.": "New issues arrive without refreshing, urgent ones on top.",
    "Üzerine al": "Take it",
    "Arızayı üstlen, kimde olduğu herkese görünsün.": "Claim an issue so everyone can see who has it.",
    "Bina ve türe göre dağılım, ortalama çözüm süresi.": "Breakdown by building and type, average time to resolve.",
    "Ekip hesabınla giriş yap.": "Sign in with your team account.",
    "Öğrenci misin?": "Are you a student?",
    "Öğrenci girişi": "Student sign-in",
    "Bu giriş sadece teknik ekip içindir. Öğrenciysen öğrenci girişini kullan.":
        "This sign-in is for the maintenance team only. Students should use the student sign-in.",

    # arıza bildir
    "Kampüste bir arıza mı gördün?": "Spotted an issue on campus?",
    "Formu doldur, bildirimin teknik ekibin paneline anında düşsün.": "Fill in the form and it goes straight to the maintenance team.",
    "Türü ve yeri seç": "Pick type and place",
    "Kısaca anlat": "Describe it briefly",
    "Durumunu takip et": "Track its status",
    "Arıza türü": "Issue type",
    "Bina": "Building",
    "Bina seç": "Choose a building",
    "Kat / oda": "Floor / room",
    "İsteğe bağlı": "Optional",
    "Örn. 2. kat, B-204": "e.g. 2nd floor, B-204",
    "Kısa başlık": "Short title",
    "Örn. Projeksiyon görüntü vermiyor": "e.g. Projector shows no image",
    "Açıklama": "Description",
    "Ne oldu, ne zamandır böyle?": "What happened, and since when?",
    "Cihaz açılıyor ama görüntü gelmiyor. Kabloyu değiştirdik, sonuç aynı.":
        "The device turns on but there's no picture. We swapped the cable; no change.",
    "Aciliyet": "Urgency",
    "Bekleyebilir": "Can wait",
    "Bugün": "Today",
    "Güvenlik riski": "Safety risk",
    "Teknik ekibe gönder": "Send to the team",
    "Yangın, su baskını veya elektrik kaçağında bildirimin yanında kampüs güvenliğini de ara.":
        "For fire, flooding or electrical hazards, also call campus security.",
    "Bildirimin teknik ekibe iletildi. Takip numaran: {code}": "Your report was sent to the team. Tracking number: {code}",

    # bildirimlerim
    "Gönderdiğin arızalar ve teknik ekibin güncel durumu.": "Issues you reported and their current status.",
    "Yeni bildirim": "New report",
    "Henüz bir bildirimin yok": "You haven't reported anything yet",
    "Bir arıza gördüğünde": "When you spot an issue,",
    "buradan bildir": "report it here",
    "; durumu bu sayfada görünür.": "; its status will show on this page.",

    # arıza detayı
    "Durumu güncelle": "Update status",
    "Öğrenci yaptığın değişikliği ve notunu kendi sayfasında görür.": "The student sees your change and note on their page.",
    "Öğrenciye not": "Note to student",
    "Örn. Teknisyen yolda, 30 dakika içinde orada.": "e.g. A technician is on the way, there in 30 minutes.",
    "Geçmiş": "History",
    "Konum": "Location",
    "Bildirilme": "Reported",
    "Son güncelleme": "Last update",
    "Bildiren": "Reported by",
    "Sorumlu": "Assignee",
    "Atanmadı": "Unassigned",
    "Arıza bildirildi": "Issue reported",
    "Arızayı üzerine aldı": "Took the issue",
    "Arızayı bıraktı": "Released the issue",
    "Bu arıza bulunamadı": "Issue not found",
    "Silinmiş olabilir ya da görüntüleme yetkin yok.": "It may have been removed, or you don't have access.",
    "Bildirimlerime dön": "Back to my reports",
    "{code} güncellendi: {status}.": "{code} updated: {status}.",
    "{code} güncellendi.": "{code} updated.",

    # panel
    "Gelen arızalar": "Incoming issues",
    "Acil olanlar en üstte. Yeni bildirimler sayfa yenilenmeden buraya düşer.": "Urgent issues on top. New reports appear here without refreshing.",
    "Bağlanıyor…": "Connecting…",
    "Canlı": "Live",
    "Yeniden bağlanıyor…": "Reconnecting…",
    "Canlı güncelleme desteklenmiyor": "Live updates not supported",
    "Yeni arıza": "New issue",
    "ACİL": "URGENT",
    "Tümü": "All",
    "Atanmamış": "Unassigned",
    "Açık olanlar": "Open",
    "Hepsi": "All statuses",
    "Tüm türler": "All types",
    "Duruma göre": "By status",
    "Türe göre": "By type",
    "Ara": "Search",
    "Bina, oda veya takip no ara": "Search building, room or tracking no.",
    "Açık acil": "Open urgent",
    "Bu filtrede bildirim yok": "No issues match this filter",
    "Yeni bir arıza bildirildiğinde burada anında görünür.": "New reports will appear here instantly.",
    "Öğrenciye not yaz": "Write a note to the student",
    "Not": "Note",
    "Not ekle": "Add note",
    "Sende": "Yours",
    "{name} üzerinde": "With {name}",
    "Bırak": "Release",
    "Devral": "Take over",

    # raporlar
    "Son {n} gün": "Last {n} days",
    "Toplam arıza": "Total issues",
    "Açık arıza": "Open issues",
    "Ortalama çözüm süresi": "Average time to resolve",
    "48 saatten uzun açık": "Open longer than 48h",
    "Henüz çözülen yok": "Nothing resolved yet",
    "{n} sa": "{n} h",
    "{n} gün": "{n} days",
    "1 saatten az": "Under 1 hour",
    "Günlük arızalar": "Daily issues",
    "Bildirilen": "Reported",
    "Çözülen": "Resolved",
    "Türe göre dağılım": "By type",
    "Binaya göre dağılım": "By building",
    "Ekip performansı": "Team performance",
    "Kişi": "Person",
    "Açık": "Open",
    "Henüz veri yok": "No data yet",
    "Arızalar bildirildikçe burada grafikler oluşacak.": "Charts will appear here as issues are reported.",
    "Bina ve türe göre dağılım, çözüm süreleri ve ekibin iş yükü.": "Breakdown by building and type, resolution times and team workload.",

    # kullanıcılar
    "Kullanıcılar ve roller": "Users and roles",
    "Teknik ekip üyelerine “Teknik ekip” rolünü ver; panele erişip arıza durumlarını güncelleyebilirler.":
        "Give maintenance staff the “Maintenance team” role so they can use the dashboard and update issues.",
    "E-posta": "Email",
    "Kayıt": "Joined",
    "Rol": "Role",
    "Değiştir": "Change",
    "{name} artık {role}.": "{name} is now {role}.",
    "Bu sayfa sadece teknik ekip içindir.": "This page is for the maintenance team only.",
    "Bu sayfa sadece yöneticiler içindir.": "This page is for administrators only.",
}

CATEGORIES_EN = {
    "elektrik": "Electrical", "bilgisayar": "Computer / Projector", "internet": "Internet / Wi‑Fi",
    "tesisat": "Plumbing", "iklim": "Heating / AC", "kapi": "Doors / Furniture", "temizlik": "Cleaning", "diger": "Other",
}
BUILDINGS_EN = {"Açık alan / Kampüs": "Outdoors / Campus", "Diğer": "Other", "Yemekhane": "Dining Hall",
                "Kütüphane": "Library", "Yurtlar": "Dormitories", "Spor Salonu": "Sports Hall"}
