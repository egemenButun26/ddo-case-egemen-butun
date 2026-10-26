import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import { kurus, metin, saat, tarihSaat } from "../format";
import type { Borc, Doluluk, Inceleme, Olay, Sayfali } from "../api/tipler";

type Durum = "yukleniyor" | "hazir" | "hata";

function hataMesaji(hata: unknown, yedek: string): string {
  return hata instanceof Error ? hata.message : yedek;
}

export function GorevliPage() {
  const [durum, setDurum] = useState<Durum>("yukleniyor");
  const [hata, setHata] = useState<string | null>(null);
  const [doluluklar, setDoluluklar] = useState<Doluluk[]>([]);
  const [incelemeler, setIncelemeler] = useState<Inceleme[]>([]);
  const [olaylar, setOlaylar] = useState<Olay[]>([]);
  const [sonGuncelleme, setSonGuncelleme] = useState("—");
  const [canliHatasi, setCanliHatasi] = useState<string | null>(null);
  const [oturumNumaralari, setOturumNumaralari] = useState<Record<number, string>>({});
  const [islemde, setIslemde] = useState<string | null>(null);
  const [islemHatasi, setIslemHatasi] = useState<string | null>(null);
  const [sorguPlaka, setSorguPlaka] = useState("");
  const [borc, setBorc] = useState<Borc | null>(null);
  const [borcSorgulandi, setBorcSorgulandi] = useState(false);
  const istekNo = useRef(0);

  const olaylariYukle = useCallback(async () => {
    try {
      const yanit = await apiFetch<Sayfali<Olay>>("/events/recent?limit=20");
      setOlaylar(Array.isArray(yanit.items) ? yanit.items : []);
      setSonGuncelleme(saat());
      setCanliHatasi(null);
    } catch (istekHatasi) {
      setCanliHatasi(hataMesaji(istekHatasi, "Canlı akış güncellenemedi."));
    }
  }, []);

  const yukle = useCallback(async () => {
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    try {
      const [dolulukYaniti, incelemeYaniti, olayYaniti] = await Promise.all([
        apiFetch<Doluluk[]>("/occupancy"),
        apiFetch<Sayfali<Inceleme>>("/reviews?status=open&limit=20"),
        apiFetch<Sayfali<Olay>>("/events/recent?limit=20"),
      ]);
      if (buIstek !== istekNo.current) {
        return;
      }
      setDoluluklar(Array.isArray(dolulukYaniti) ? dolulukYaniti : []);
      setIncelemeler(Array.isArray(incelemeYaniti.items) ? incelemeYaniti.items : []);
      setOlaylar(Array.isArray(olayYaniti.items) ? olayYaniti.items : []);
      setSonGuncelleme(saat());
      setCanliHatasi(null);
      setDurum("hazir");
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(hataMesaji(istekHatasi, "Görevli ekranı yüklenemedi."));
      setDurum("hata");
    }
  }, []);

  useEffect(() => {
    void yukle();
  }, [yukle]);

  useEffect(() => {
    const zamanlayici = setInterval(() => {
      void olaylariYukle();
    }, 10000);
    return () => clearInterval(zamanlayici);
  }, [olaylariYukle]);

  const incelemeCoz = async (inceleme: Inceleme, eylem: "dismiss" | "link_session") => {
    const kimlik = inceleme.id;
    if (kimlik === undefined) {
      return;
    }
    const govde: { action: "dismiss" | "link_session"; session_id?: number } = {
      action: eylem,
    };
    if (eylem === "link_session") {
      const ham = (oturumNumaralari[kimlik] ?? "").trim();
      if (!ham) {
        setIslemHatasi("Oturum numarası gerekli.");
        return;
      }
      govde.session_id = Number(ham);
    }
    setIslemHatasi(null);
    setIslemde(`${eylem}-${kimlik}`);
    try {
      await apiFetch<void>(`/reviews/${kimlik}/resolve`, {
        method: "POST",
        body: govde,
      });
      setIncelemeler((oncekiler) => oncekiler.filter((kayit) => kayit.id !== kimlik));
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "İnceleme çözülemedi."));
    } finally {
      setIslemde(null);
    }
  };

  const borcSorgula = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    const temiz = sorguPlaka.trim();
    if (!temiz) {
      setIslemHatasi("Plaka gerekli.");
      return;
    }
    setIslemHatasi(null);
    setIslemde("borc-sorgu");
    try {
      const yanit = await apiFetch<Borc>(`/debts?plate=${encodeURIComponent(temiz)}`);
      setBorc(yanit);
      setBorcSorgulandi(true);
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Borç sorgulanamadı."));
    } finally {
      setIslemde(null);
    }
  };

  if (durum === "yukleniyor") {
    return (
      <section className="kart kart--genis" aria-labelledby="gorevli-baslik">
        <header className="baslik">
          <h1 id="gorevli-baslik">Görevli</h1>
          <p className="altbaslik">İnceleme kuyruğu ve saha işlemleri</p>
        </header>
        <AsyncState durum="yukleniyor" />
      </section>
    );
  }

  if (durum === "hata") {
    return (
      <section className="kart kart--genis" aria-labelledby="gorevli-baslik">
        <header className="baslik">
          <h1 id="gorevli-baslik">Görevli</h1>
          <p className="altbaslik">İnceleme kuyruğu ve saha işlemleri</p>
        </header>
        <AsyncState durum="hata" mesaj={hata ?? undefined} onTekrar={() => void yukle()} />
      </section>
    );
  }

  const borcKalemleri = borc?.items ?? [];

  return (
    <div className="ekran">
      <section className="kart kart--genis" aria-labelledby="gorevli-baslik">
        <header className="baslik">
          <h1 id="gorevli-baslik">Görevli</h1>
          <p className="altbaslik">İnceleme kuyruğu ve saha işlemleri</p>
        </header>

        <p className="canli-gosterge" role="status">
          Sistem canlı · son güncelleme {sonGuncelleme}
        </p>
        {canliHatasi ? (
          <p className="hata" role="alert">
            {canliHatasi}
          </p>
        ) : null}
        {islemHatasi ? (
          <p className="hata" role="alert">
            {islemHatasi}
          </p>
        ) : null}

        <h2>Doluluk özeti</h2>
        {doluluklar.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {doluluklar.map((kayit, sira) => (
              <li key={kayit.lot_id ?? sira} className="kayit-satiri">
                <div>
                  <p className="kayit-basligi">{metin(kayit.code)}</p>
                  <p className="soluk">
                    {metin(kayit.occupied)} / {metin(kayit.capacity)} dolu ·{" "}
                    {metin(kayit.available)} boş
                  </p>
                </div>
                <span className="rol-etiketi">
                  {metin(kayit.active_reservations)} rezervasyon
                </span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="inceleme-baslik">
        <header className="baslik">
          <h2 id="inceleme-baslik">İnceleme kuyruğu</h2>
          <p className="altbaslik">Okuyamadığı plakaları çöz</p>
        </header>
        {incelemeler.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {incelemeler.map((inceleme, sira) => {
              const kimlik = inceleme.id;
              return (
                <li key={kimlik ?? sira} className="kayit-satiri kayit-satiri--dikey">
                  <div className="kayit-icerik">
                    <p className="kayit-basligi">{metin(inceleme.read_plate)}</p>
                    <p className="soluk">
                      {metin(inceleme.reason)} · {tarihSaat(inceleme.captured_at)}
                    </p>
                    <p className="soluk">
                      Güven: {metin(inceleme.confidence)} · Olay {metin(inceleme.event_id)}
                    </p>
                  </div>
                  <div className="satir-islemleri">
                    <button
                      type="button"
                      className="dugme dugme--ikincil dugme--dar"
                      disabled={kimlik === undefined || islemde !== null}
                      onClick={() => {
                        if (kimlik !== undefined) {
                          void incelemeCoz(inceleme, "dismiss");
                        }
                      }}
                    >
                      Yok say
                    </button>
                    <div className="alan alan--satir">
                      <label htmlFor={`oturum-${kimlik ?? sira}`}>
                        Oturum no ({metin(kimlik)})
                      </label>
                      <input
                        id={`oturum-${kimlik ?? sira}`}
                        value={kimlik !== undefined ? oturumNumaralari[kimlik] ?? "" : ""}
                        onChange={(olay) => {
                          if (kimlik === undefined) {
                            return;
                          }
                          setOturumNumaralari((oncekiler) => ({
                            ...oncekiler,
                            [kimlik]: olay.target.value,
                          }));
                        }}
                      />
                    </div>
                    <button
                      type="button"
                      className="dugme dugme--ikincil dugme--dar"
                      disabled={kimlik === undefined || islemde !== null}
                      onClick={() => {
                        if (kimlik !== undefined) {
                          void incelemeCoz(inceleme, "link_session");
                        }
                      }}
                    >
                      Oturuma bağla
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="olaylar-baslik">
        <header className="baslik">
          <h2 id="olaylar-baslik">Son olaylar</h2>
          <p className="altbaslik">10 saniyede bir otomatik yenilenir</p>
        </header>
        {olaylar.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {olaylar.map((olay, sira) => (
              <li key={olay.id ?? sira} className="kayit-satiri">
                <div>
                  <p className="kayit-basligi">
                    {metin(olay.plate_norm ?? olay.plate_raw)}
                  </p>
                  <p className="soluk">
                    {metin(olay.direction)} · {tarihSaat(olay.captured_at)}
                  </p>
                </div>
                <span className="rol-etiketi">{metin(olay.outcome)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="borc-sorgu-baslik">
        <header className="baslik">
          <h2 id="borc-sorgu-baslik">Plaka ile borç sorgusu</h2>
          <p className="altbaslik">Sürücünün açık bakiyesini gör</p>
        </header>
        <form className="form" onSubmit={borcSorgula} noValidate>
          <div className="alan">
            <label htmlFor="borc-sorgu-plaka">Plaka</label>
            <input
              id="borc-sorgu-plaka"
              value={sorguPlaka}
              onChange={(olay) => setSorguPlaka(olay.target.value)}
            />
          </div>
          <button type="submit" className="dugme" disabled={islemde !== null}>
            {islemde === "borc-sorgu" ? "Sorgulanıyor…" : "Sorgula"}
          </button>
        </form>

        {borcSorgulandi ? (
          <div>
            <p className="buyuk-tutar">Toplam: {kurus(borc?.total_kurus)}</p>
            {borcKalemleri.length === 0 ? (
              <p className="durum-bildirimi" role="status">
                Kayıt yok.
              </p>
            ) : (
              <ul className="kayit-listesi">
                {borcKalemleri.map((kalem, sira) => (
                  <li key={kalem.id ?? sira} className="kayit-satiri">
                    <div>
                      <p className="kayit-basligi">{kurus(kalem.amount_kurus)}</p>
                      <p className="soluk">
                        Kalan {kurus(kalem.remaining_kurus)} · {metin(kalem.status)}
                      </p>
                    </div>
                  </li>
                ))}
              </ul>
            )}
          </div>
        ) : null}
      </section>
    </div>
  );
}
