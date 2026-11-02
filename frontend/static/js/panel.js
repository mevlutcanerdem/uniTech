// Teknik ekip paneli: Go API'nin canlı akışını dinler, olay gelince listeyi tazeler.
(function () {
  const cfg = window.UNITECH;
  const body = document.getElementById("panel-body");
  const live = document.getElementById("live");
  const toast = document.getElementById("toast");
  const toastText = document.getElementById("toast-text");
  let toastTimer, refreshTimer, busy = false, pending = false, lastRun = 0, lastId = null;
  const MIN_GAP = 2000; // arıza yağmurunda listeyi en fazla 2 saniyede bir yenile

  function showToast(text) {
    toastText.textContent = text;
    toast.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => (toast.hidden = true), 5000);
  }

  // Not yazılırken listeyi ezmemek için, kullanıcı bir alana yazıyorsa bekle.
  function typing() {
    const a = document.activeElement;
    return a && body.contains(a) && a.tagName === "INPUT" && a.value;
  }

  // Olayları birleştir: art arda gelen olaylar tek bir yenilemeye dönüşür.
  function schedule(highlightId) {
    if (highlightId) lastId = highlightId;
    if (pending) return;
    pending = true;
    const wait = Math.max(300, MIN_GAP - (Date.now() - lastRun));
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => { pending = false; refresh(lastId); }, wait);
  }

  async function refresh(highlightId) {
    if (busy || document.hidden) { schedule(highlightId); return; }
    if (typing()) { clearTimeout(refreshTimer); pending = true; refreshTimer = setTimeout(() => { pending = false; refresh(highlightId); }, 3000); return; }
    busy = true; lastRun = Date.now();
    try {
      const res = await fetch(cfg.listUrl + location.search, { credentials: "same-origin" });
      if (res.redirected) { location.reload(); return; }
      if (res.ok) {
        body.innerHTML = await res.text();
        const el = highlightId && document.getElementById("t-" + highlightId);
        if (el) el.classList.add("flash-new");
      }
    } catch (e) { /* bağlantı yok; bir sonraki olayda tekrar denenir */ }
    finally { busy = false; }
  }

  const T = cfg.text || {};
  if (!("EventSource" in window)) { live.textContent = T.unsupported; return; }
  const es = new EventSource(cfg.streamUrl);
  es.onopen = () => { live.textContent = T.live; live.classList.add("on"); };
  es.onerror = () => { live.textContent = T.reconnecting; live.classList.remove("on"); };
  es.addEventListener("ticket", (ev) => {
    let d = {};
    try { d = JSON.parse(ev.data); } catch (e) {}
    if (d.type === "created") {
      showToast((d.priority === "acil" ? T.urgent + " · " : "") + T.newIssue + ": " + (d.title || d.code));
    }
    schedule(d.ticket_id);
  });
  // Sekme arka plandayken birikenleri, öne gelince tek seferde getir.
  document.addEventListener("visibilitychange", () => { if (!document.hidden && lastId) schedule(lastId); });
})();
