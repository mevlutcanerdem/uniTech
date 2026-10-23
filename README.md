# uniTech

Abdullah Gül Üniversitesi için arıza bildirim sistemi. Öğrenciler kampüste gördükleri arızaları
(elektrik, projeksiyon, Wi‑Fi, tesisat, klima…) siteye girer. Bildirim teknik ekibin paneline
**anında** düşer, ekip durumu güncelledikçe öğrenci kendi sayfasından takip eder.

## Özellikler

- **Sadece okul e-postası:** Kayıt yalnızca `@agu.edu.tr` adresleriyle yapılabilir.
- **Arıza bildirme:** Tür, bina, kat/oda, açıklama ve aciliyet seçilir. Her bildirime takip numarası verilir (`AGU-2026-00042`).
- **Bildirimlerim:** Öğrenci kendi arızalarının durumunu (Yeni → İşlemde → Çözüldü) ve ekibin notlarını görür.
- **Teknik ekip paneli:** Yeni arızalar sayfa yenilenmeden gelir (Server-Sent Events). Acil olanlar en üstte durur. Duruma, türe göre filtre ve arama vardır.
- **Geçmiş:** Her arızanın kim tarafından ne zaman güncellendiği kayıt altındadır.
- **Roller:** Öğrenci, Teknik ekip, Yönetici. Yönetici, “Kullanıcılar” sayfasından ekip üyelerine rol verir.

## Mimari

```
Tarayıcı ──► Django (frontend, :8000) ──► Go API (backend, :8080) ──► data/unitech.json
   │                                            ▲
   └──────────── canlı panel akışı (SSE) ───────┘
```

| Klasör | Ne yapar |
|---|---|
| `backend/` | Go API. Kimlik doğrulama, arızalar, roller, canlı akış. Sadece Go standart kütüphanesini kullanır, dış bağımlılık yoktur. |
| `frontend/` | Django arayüzü. Sayfaları çizer, verileri Go API'den alır. Kendi veritabanı yoktur, oturum imzalı çerezde tutulur. |

Veriler `backend` tarafında tek bir JSON dosyasında tutulur (yazmalar kilitli ve atomik). Bir kampüs için
yeterlidir. Kullanıcı sayısı çok büyürse `backend/store.go` içindeki aynı metotlar PostgreSQL ile yeniden yazılabilir.

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
| `POST` | `/api/auth/register`, `/api/auth/login` | Herkes |
| `GET` | `/api/me` | Giriş yapan |
| `POST` | `/api/tickets` | Giriş yapan |
| `GET` | `/api/tickets?status=&category=&q=&scope=mine` | Öğrenci kendi arızalarını, ekip tümünü görür |
| `GET` | `/api/tickets/{id}` | Sahibi veya ekip |
| `PATCH` | `/api/tickets/{id}` `{status, note}` | Teknik ekip |
| `GET` | `/api/stats`, `/api/events` (SSE) | Teknik ekip |
| `GET` / `PATCH` | `/api/users`, `/api/users/{id}` `{role}` | Yönetici |

## Testler

```bash
cd backend && go test ./...
```

## Sonraki adımlar

- **E-posta doğrulama:** Şu an `@agu.edu.tr` adresi kontrol ediliyor ama adresin gerçekten kişiye ait olduğu doğrulanmıyor. Okulun SMTP bilgisi alınınca kayıtta doğrulama kodu gönderilmeli.
- Arızaya fotoğraf ekleme.
- Bina listesini (`frontend/portal/constants.py`) okulun güncel listesiyle güncelleme.
