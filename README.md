# uniTech

**Canlı:** https://unitech-web.onrender.com · **Ekip girişi:** https://unitech-web.onrender.com/ekip/giris/ · **Demo (örnek verili):** https://unitech-agu.onrender.com

![CI](https://github.com/mevlutcanerdem/uniTech/actions/workflows/ci.yml/badge.svg)

Abdullah Gül Üniversitesi için arıza bildirim sistemi. Öğrenciler kampüste gördükleri arızaları
(elektrik, projeksiyon, Wi‑Fi, tesisat, klima…) siteye girer. Bildirim teknik ekibin paneline
**anında** düşer, ekip durumu güncelledikçe öğrenci kendi sayfasından takip eder.

## Özellikler

- **Sadece okul e-postası:** Kayıt yalnızca `@agu.edu.tr` adresleriyle yapılabilir. E-posta göndericisi tanımlıysa adrese 6 haneli doğrulama kodu gider; doğrulanmadan arıza bildirilemez.
- **Şifre sıfırlama:** “Şifremi unuttum” ile e-postaya kod gelir; şifre değişince diğer cihazlardaki oturumlar kapanır.
- **Fotoğraf:** Arızaya en fazla 3 fotoğraf eklenebilir. Sunucu fotoğrafı küçültür, telefonun döndürme bilgisini uygular ve konum gibi EXIF verilerini siler.
- **Arıza bildirme:** Tür, bina, kat/oda, açıklama ve aciliyet seçilir. Her bildirime takip numarası verilir (`AGU-2026-00042`).
- **Bildirimlerim:** Öğrenci kendi arızalarının durumunu (Yeni → İşlemde → Çözüldü) ve ekibin notlarını görür.
- **Teknik ekip paneli:** Yeni arızalar sayfa yenilenmeden gelir (Server-Sent Events). Acil olanlar en üstte durur. Duruma, türe göre filtre ve arama vardır.
- **Geçmiş:** Her arızanın kim tarafından ne zaman güncellendiği kayıt altındadır.
- **Roller:** Öğrenci, Teknik ekip, Yönetici. Yönetici, “Kullanıcılar” sayfasından ekip üyelerine rol verir.
- **Ayrı ekip girişi:** Teknik ekip `/ekip/giris/` adresinden girer ve kendine özel menüyü görür (Panel, Bana atananlar, Raporlar).
- **Arızayı üzerine alma:** Teknisyen arızayı üstlenir, kimde olduğu herkese görünür. “İşlemde” yapılan arıza otomatik olarak işlemi yapana atanır.
- **Raporlar:** Günlük bildirilen/çözülen grafiği, türe ve binaya göre dağılım, ortalama çözüm süresi, ekip performansı.
- **Gizlilik:** Arızayı bildiren öğrencinin adı ve e-postası her yerde maskelenir (`M*** C*** E***`).
- **Türkçe / İngilizce:** Üst bardaki TR / EN seçiciyle dil değişir. Yeni metin eklerken `frontend/portal/i18n.py` içine İngilizcesini ekle; `python frontend/check_i18n.py` eksikleri listeler.

## Mimari

```
Tarayıcı ──► Django (frontend, :8000) ──► Go API (backend, :8080) ──► PostgreSQL
   │                                            ▲
   └──────────── canlı panel akışı (SSE) ───────┘
```

| Klasör | Ne yapar |
|---|---|
| `backend/` | Go 1.27 API. Kimlik doğrulama, e-posta kodları, arızalar, fotoğraflar, roller, raporlar, canlı akış, hız sınırları. Tek dış bağımlılık PostgreSQL sürücüsü (pgx). |
| `backend/migrations/` | Veritabanı şeması. API açılışta eksik migration'ları sırayla uygular. |
| `frontend/` | Django 6.1 arayüzü. Sayfaları çizer, verileri Go API'den alır. Kendi veritabanı yoktur, oturum imzalı çerezde tutulur. İçerik Güvenliği Politikası (CSP) açıktır. |

`DATABASE_URL` tanımlıysa veriler PostgreSQL tablolarında, değilse (geliştirme için) yerel bir dosyada tutulur.
Önceki sürümün tek satırlık JSON deposu varsa API ilk açılışta onu yeni tablolara taşır.

## Hızlı başlangıç (Docker)

```bash
cp .env.example .env        # içindeki gizli değerleri ve ADMIN_EMAILS'i düzenle
docker compose up --build
```

Ardından http://localhost:8000 adresini aç. `ADMIN_EMAILS` içine yazdığın e-postayla kayıt olan hesap
otomatik olarak **yönetici** olur. Diğer ekip üyelerine rolü “Kullanıcılar” sayfasından ver.

## Docker olmadan çalıştırma

İki ayrı terminal aç.

**1. Go API**

```bash
cd backend
JWT_SECRET=gizli-bir-deger ADMIN_EMAILS=ad.soyad@agu.edu.tr go run .
```

**2. Django arayüzü**

```bash
cd frontend
python -m venv .venv && source .venv/bin/activate   # Windows: .venv\Scripts\activate
pip install -r requirements.txt
python manage.py runserver
```

## Ortam değişkenleri

| Değişken | Servis | Açıklama |
|---|---|---|
| `JWT_SECRET` | API | Oturum imzalama anahtarı. Boş kalırsa her yeniden başlatmada herkesin oturumu kapanır. |
| `ADMIN_EMAILS` | API | Virgülle ayrılmış e-postalar. Bunlarla kayıt olanlar yönetici olur. |
| `FRONTEND_ORIGIN` | API | Canlı akışa bağlanabilecek site adresi (CORS). Varsayılan `http://localhost:8000`. |
| `DATA_PATH` | API | Veri dosyasının yolu. Varsayılan `data/unitech.json`. |
| `API_URL` | Web | Django'nun API'ye eriştiği adres. |
| `API_PUBLIC_URL` | Web | Tarayıcının canlı akış için bağlandığı API adresi. |
| `DJANGO_SECRET_KEY`, `DJANGO_DEBUG`, `DJANGO_ALLOWED_HOSTS`, `DJANGO_CSRF_TRUSTED_ORIGINS` | Web | Standart Django ayarları. Sunucuda `DJANGO_DEBUG=0` yap. |

## API uç noktaları

| Yöntem | Yol | Kim |
|---|---|---|
| `GET` | `/api/health`, `/api/meta`, `/api/public/stats` | Herkes |
| `POST` | `/api/auth/register`, `/api/auth/login`, `/api/auth/forgot`, `/api/auth/reset` | Herkes |
| `POST` | `/api/auth/verify` `{code}`, `/api/auth/verify/resend` | Giriş yapan |
| `GET` | `/api/me` | Giriş yapan |
| `POST` | `/api/tickets`, `/api/tickets/{id}/photos` (multipart `photo`) | Doğrulanmış kullanıcı |
| `GET` | `/api/tickets?status=&category=&assignee=me\|none&q=&scope=mine` | Öğrenci kendi arızalarını, ekip tümünü görür |
| `GET` | `/api/tickets/{id}`, `/api/photos/{id}` | Sahibi veya ekip |
| `PATCH` | `/api/tickets/{id}` `{status, note, assign}` | Teknik ekip |
| `GET` | `/api/stats`, `/api/reports?days=14\|30`, `/api/events` (SSE) | Teknik ekip |
| `GET` / `PATCH` | `/api/users`, `/api/users/{id}` `{role}` | Yönetici |

Hatalar `{"error": "...", "code": "..."}` biçiminde döner; hız sınırına takılan isteklerde `Retry-After` başlığı vardır.

## Testler

```bash
cd backend && go test ./...                                   # bellek deposu
TEST_DATABASE_URL=postgres://... go test -p 1 ./...           # + PostgreSQL
cd frontend && python check_i18n.py                           # eksik İngilizce metin
```

GitHub Actions her gönderimde bu testleri, `manage.py check --deploy`'u ve iki Docker derlemesini çalıştırır.

## Sonraki adımlar

- **E-posta göndericisini aç:** Render'da API servisine `BREVO_API_KEY` ve `MAIL_FROM` (veya okulun `SMTP_*` bilgileri) ekle; doğrulama ve şifre sıfırlama kendiliğinden devreye girer.
- Bina listesini (`frontend/portal/constants.py`) okulun güncel listesiyle güncelleme.
