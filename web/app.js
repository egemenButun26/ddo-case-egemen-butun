"use strict";

(function () {
  const ZAMAN_ASIMI_MS = 5000;
  const YENILEME_MS = 10000;

  const durumKutusu = document.getElementById("durum");
  const durumMetni = document.getElementById("durum-metni");
  const veritabaniMetni = document.getElementById("veritabani-metni");
  const sonKontrolMetni = document.getElementById("son-kontrol-metni");
  const hataMetni = document.getElementById("hata-metni");
  const yenileDugmesi = document.getElementById("yenile-dugmesi");

  let istekNo = 0;
  let aktifDenetleyici = null;
  let sonZamanlayici = null;

  function durumAyarla(sinif, metin) {
    durumKutusu.classList.remove("yukleniyor", "calisiyor", "hata");
    durumKutusu.classList.add(sinif);
    durumMetni.textContent = metin;
  }

  function hataGoster(metin) {
    hataMetni.textContent = metin;
    hataMetni.hidden = false;
  }

  function hataGizle() {
    hataMetni.textContent = "";
    hataMetni.hidden = true;
  }

  function sonKontrolYaz() {
    sonKontrolMetni.textContent = new Date().toLocaleTimeString("tr-TR", {
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
    });
  }

  function zamanla() {
    if (sonZamanlayici !== null) {
      clearTimeout(sonZamanlayici);
    }
    sonZamanlayici = setTimeout(kontrolEt, YENILEME_MS);
  }

  function yukleniyorGoster() {
    durumAyarla("yukleniyor", "Kontrol ediliyor…");
    veritabaniMetni.textContent = "—";
    hataGizle();
  }

  function kullaniciHatasi(status, veri) {
    if (status === 503) {
      return "Veritabanına ulaşılamıyor. API çalışıyor ama veritabanı yanıt vermiyor.";
    }
    if (veri && veri.error && veri.error.code === "unavailable") {
      return "Veritabanına ulaşılamıyor. API çalışıyor ama veritabanı yanıt vermiyor.";
    }
    if (status > 0) {
      return "Sunucu beklenmeyen bir yanıt verdi (HTTP " + status + "). Lütfen tekrar deneyin.";
    }
    return "Sunucuya ulaşılamadı. Bağlantınızı kontrol edip yeniden deneyin.";
  }

  async function kontrolEt() {
    const buIstek = ++istekNo;

    if (aktifDenetleyici !== null) {
      aktifDenetleyici.abort();
    }
    const denetleyici = new AbortController();
    aktifDenetleyici = denetleyici;

    const zamanAsimi = setTimeout(function () {
      denetleyici.abort();
    }, ZAMAN_ASIMI_MS);

    yukleniyorGoster();

    try {
      const yanit = await fetch("/api/health", {
        method: "GET",
        headers: { Accept: "application/json" },
        cache: "no-store",
        signal: denetleyici.signal,
      });

      let veri = null;
      try {
        veri = await yanit.json();
      } catch (ayristirmaHatasi) {
        veri = null;
      }

      if (buIstek !== istekNo) {
        return;
      }

      if (
        yanit.ok &&
        veri !== null &&
        veri.status === "ok" &&
        veri.checks &&
        veri.checks.database === "ok"
      ) {
        durumAyarla("calisiyor", "Sistem çalışıyor");
        veritabaniMetni.textContent = "ok";
        hataGizle();
      } else {
        durumAyarla("hata", "Sistemde sorun var");
        veritabaniMetni.textContent =
          veri && veri.checks && veri.checks.database ? String(veri.checks.database) : "erişilemiyor";
        hataGoster(kullaniciHatasi(yanit.status, veri));
      }
    } catch (hata) {
      if (buIstek !== istekNo) {
        return;
      }
      durumAyarla("hata", "Sistemde sorun var");
      veritabaniMetni.textContent = "erişilemiyor";
      if (hata && hata.name === "AbortError") {
        hataGoster("Sunucu 5 saniye içinde yanıt vermedi. Lütfen tekrar deneyin.");
      } else {
        hataGoster("Sunucuya ulaşılamadı. Bağlantınızı kontrol edip yeniden deneyin.");
      }
    } finally {
      clearTimeout(zamanAsimi);
      if (buIstek === istekNo) {
        sonKontrolYaz();
        zamanla();
      }
    }
  }

  yenileDugmesi.addEventListener("click", function () {
    if (sonZamanlayici !== null) {
      clearTimeout(sonZamanlayici);
      sonZamanlayici = null;
    }
    kontrolEt();
  });

  kontrolEt();
})();
