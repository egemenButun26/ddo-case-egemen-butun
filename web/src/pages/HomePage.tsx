import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";

type SaglikYaniti = {
  status?: string;
  checks?: { database?: string };
};

type Durum = "yukleniyor" | "calisiyor" | "hata";

export function HomePage() {
  const [durum, setDurum] = useState<Durum>("yukleniyor");
  const [veritabani, setVeritabani] = useState("—");
  const [sonKontrol, setSonKontrol] = useState("—");
  const [hata, setHata] = useState<string | null>(null);
  const istekNo = useRef(0);

  const kontrolEt = useCallback(async () => {
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    setVeritabani("—");

    try {
      const veri = await apiFetch<SaglikYaniti>("/health");
      if (buIstek !== istekNo.current) {
        return;
      }
      if (veri.status === "ok" && veri.checks?.database === "ok") {
        setDurum("calisiyor");
        setVeritabani("ok");
      } else {
        setDurum("hata");
        setVeritabani(veri.checks?.database ?? "erişilemiyor");
        setHata("Sistemde sorun var. Veritabanı yanıt vermiyor olabilir.");
      }
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setDurum("hata");
      setVeritabani("erişilemiyor");
      setHata(
        istekHatasi instanceof Error
          ? istekHatasi.message
          : "Sunucuya ulaşılamadı. Bağlantınızı kontrol edin."
      );
    } finally {
      if (buIstek === istekNo.current) {
        setSonKontrol(
          new Date().toLocaleTimeString("tr-TR", {
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
          })
        );
      }
    }
  }, []);

  useEffect(() => {
    void kontrolEt();
  }, [kontrolEt]);

  return (
    <section className="kart" aria-labelledby="sistem-durumu-baslik">
      <header className="baslik">
        <h1 id="sistem-durumu-baslik">ParkAkıl</h1>
        <p className="altbaslik">Sistem durumu</p>
      </header>

      {durum === "yukleniyor" ? (
        <div className="durum yukleniyor" role="status" aria-live="polite">
          <span className="nokta" aria-hidden="true" />
          <p className="durum-metni">Kontrol ediliyor…</p>
        </div>
      ) : durum === "calisiyor" ? (
        <div className="durum calisiyor" role="status" aria-live="polite">
          <span className="nokta" aria-hidden="true" />
          <p className="durum-metni">Sistem çalışıyor</p>
        </div>
      ) : (
        <div className="durum hata" role="alert">
          <span className="nokta" aria-hidden="true" />
          <p className="durum-metni">Sistemde sorun var</p>
        </div>
      )}

      <dl className="ayrintilar">
        <div className="satir">
          <dt>Veritabanı</dt>
          <dd>{veritabani}</dd>
        </div>
        <div className="satir">
          <dt>Son kontrol</dt>
          <dd>{sonKontrol}</dd>
        </div>
      </dl>

      {hata ? (
        <p className="hata" role="alert">
          {hata}
        </p>
      ) : null}

      <button type="button" className="dugme" onClick={() => void kontrolEt()}>
        Yenile
      </button>
    </section>
  );
}

export function RoleHomePage({
  baslik,
  aciklama,
}: {
  baslik: string;
  aciklama: string;
}) {
  return (
    <section className="kart" aria-labelledby="rol-baslik">
      <header className="baslik">
        <h1 id="rol-baslik">{baslik}</h1>
        <p className="altbaslik">{aciklama}</p>
      </header>
      <AsyncState durum="bos" bosMetni="Bu ekran henüz bağlanmadı." />
    </section>
  );
}
