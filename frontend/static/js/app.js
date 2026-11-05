// Sitenin genel davranışları. Satır içi betik yok (İçerik Güvenliği Politikası).
(function () {
  // API'yi uyandır: ücretsiz sunucuda API, sunucudan gelen isteklerle uyanmaz; tarayıcıdan gelen bir istekle uyanır.
  // Öğrenci formu doldururken API arka planda hazır hale gelir. Yanıt okunmaz (no-cors).
  var api = document.body.dataset.api;
  if (api && window.fetch) {
    try { fetch(api + "/api/health", { mode: "no-cors", cache: "no-store", keepalive: true }).catch(function () {}); } catch (e) {}
  }

  // Değişince formu gönderen seçim kutuları
  document.querySelectorAll("[data-autosubmit]").forEach(function (el) {
    el.addEventListener("change", function () { el.form && el.form.submit(); });
  });

  // Fotoğraf seçimi: önizleme, en fazla N dosya, boyut kontrolü
  document.querySelectorAll("[data-photo-input]").forEach(function (input) {
    var max = parseInt(input.dataset.max || "3", 10);
    var maxBytes = 8 * 1024 * 1024;
    var box = document.getElementById(input.dataset.preview);
    var err = document.getElementById(input.dataset.error);
    var urls = [];
    input.addEventListener("change", function () {
      urls.forEach(URL.revokeObjectURL); urls = [];
      if (box) box.innerHTML = "";
      if (err) { err.hidden = true; err.textContent = ""; }
      var files = Array.prototype.slice.call(input.files || []);
      var problem = "";
      if (files.length > max) problem = input.dataset.msgCount;
      files.forEach(function (f) {
        if (f.size > maxBytes) problem = input.dataset.msgSize;
        else if (!/^image\/(jpeg|png)$/i.test(f.type)) problem = input.dataset.msgType;
      });
      if (problem) {
        input.value = "";
        if (err) { err.textContent = problem; err.hidden = false; }
        return;
      }
      files.forEach(function (f) {
        var u = URL.createObjectURL(f); urls.push(u);
        var img = document.createElement("img");
        img.src = u; img.alt = ""; img.className = "thumb";
        box && box.appendChild(img);
      });
    });
  });

  // Gönderilen formlarda butonu kilitle (çift tıklamayla iki kayıt oluşmasın)
  document.querySelectorAll("form[data-lock]").forEach(function (form) {
    form.addEventListener("submit", function () {
      var b = form.querySelector("button[type=submit]");
      if (b) { b.disabled = true; b.classList.add("busy"); }
    });
  });

  // 6 haneli kod alanı: sadece rakam
  document.querySelectorAll("input[data-code]").forEach(function (el) {
    el.addEventListener("input", function () { el.value = el.value.replace(/\D/g, "").slice(0, 6); });
  });
})();
