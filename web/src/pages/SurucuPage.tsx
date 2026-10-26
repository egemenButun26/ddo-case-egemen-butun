import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import { useAuthOpsiyonel } from "../auth/AuthContext";
import { kurus, metin, tarih, tarihSaat } from "../format";
import type {
  Abonelik,
  Borc,
  Fatura,
  Lot,
  Plan,
  Rezervasyon,
  Sayfali,
} from "../api/tipler";

type Durum = "yukleniyor" | "hazir" | "hata";

function hataMesaji(hata: unknown, yedek: string): string {
  return hata instanceof Error ? hata.message : yedek;
}

export function SurucuPage({ plaka: plakaProp }: { plaka?: string }) {
  const auth = useAuthOpsiyonel();
  const [plaka, setPlaka] = useState(
    (plakaProp ?? auth?.kullanici?.plate ?? "").trim()
  );
  const [plakaGirdi, setPlakaGirdi] = useState(plaka);
  const [durum, setDurum] = useState<Durum>(plaka ? "yukleniyor" : "hazir");
  const [hata, setHata] = useState<string | null>(null);
  const [borclar, setBorclar] = useState<Borc | null>(null);
  const [abonelikler, setAbonelikler] = useState<Abonelik[]>([]);
  const [planlar, setPlanlar] = useState<Plan[]>([]);
  const [faturalar, setFaturalar] = useState<Fatura[]>([]);
  const [lotlar, setLotlar] = useState<Lot[]>([]);
  const [yeniPlan, setYeniPlan] = useState("");
  const [planSecimleri, setPlanSecimleri] = useState<Record<number, string>>({});
  const [rezervasyonLot, setRezervasyonLot] = useState("");
  const [rezervasyon, setRezervasyon] = useState<Rezervasyon | null>(null);
  const [islemde, setIslemde] = useState<string | null>(null);
  const [islemHatasi, setIslemHatasi] = useState<string | null>(null);
  const [basari, setBasari] = useState<string | null>(null);
  const istekNo = useRef(0);

  useEffect(() => {
    if (!plaka && auth?.kullanici?.plate) {
      const gelen = auth.kullanici.plate.trim();
      setPlaka(gelen);
      setPlakaGirdi(gelen);
    }
  }, [auth?.kullanici?.plate, plaka]);

  const yukle = useCallback(async () => {
    if (!plaka) {
      return;
    }
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    const plakaSorgu = encodeURIComponent(plaka);
    try {
      const [lotYaniti, borcYaniti, abonelikYaniti, planYaniti, faturaYaniti] =
        await Promise.all([
          apiFetch<Lot[]>("/lots"),
          apiFetch<Borc>(`/debts?plate=${plakaSorgu}`),
          apiFetch<Sayfali<Abonelik>>(
            `/subscriptions?plate=${plakaSorgu}&limit=20`
          ),
          apiFetch<Plan[]>("/plans"),
          apiFetch<Sayfali<Fatura>>("/invoices?limit=20"),
        ]);
      if (buIstek !== istekNo.current) {
        return;
      }
      setLotlar(Array.isArray(lotYaniti) ? lotYaniti : []);
      setBorclar(borcYaniti ?? null);
      setAbonelikler(Array.isArray(abonelikYaniti.items) ? abonelikYaniti.items : []);
      setPlanlar(Array.isArray(planYaniti) ? planYaniti : []);
      setFaturalar(Array.isArray(faturaYaniti.items) ? faturaYaniti.items : []);
      setDurum("hazir");
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(hataMesaji(istekHatasi, "Sürücü bilgileri yüklenemedi."));
      setDurum("hata");
    }
  }, [plaka]);

  useEffect(() => {
    void yukle();
  }, [yukle]);

  const islemYap = async (islemAdi: string, is: () => Promise<void>) => {
    setIslemHatasi(null);
    setBasari(null);
    setIslemde(islemAdi);
    try {
      await is();
      await yukle();
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "İşlem tamamlanamadı."));
    } finally {
      setIslemde(null);
    }
  };

  const plakaKaydet = (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    const temiz = plakaGirdi.trim();
    if (!temiz) {
      setIslemHatasi("Plaka gerekli.");
      return;
    }
    setPlaka(temiz);
    setDurum("yukleniyor");
    setIslemHatasi(null);
  };

  const abonelikOlustur = (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    if (!yeniPlan) {
      setIslemHatasi("Plan seçin.");
      return;
    }
    void islemYap("olustur", async () => {
      await apiFetch<Abonelik>("/subscriptions", {
        method: "POST",
        body: { plate: plaka, plan_code: yeniPlan },
      });
      setBasari("Abonelik oluşturuldu.");
    });
  };

  if (!plaka) {
    return (
      <section className="kart" aria-labelledby="surucu-plaka-baslik">
        <header className="baslik">
          <h1 id="surucu-plaka-baslik">Sürücü</h1>
          <p className="altbaslik">Borç, abonelik ve rezervasyon işlemleri</p>
        </header>
        <form className="form" onSubmit={plakaKaydet} noValidate>
          <div className="alan">
            <label htmlFor="surucu-plaka">Plaka</label>
            <input
              id="surucu-plaka"
              name="plaka"
              value={plakaGirdi}
              onChange={(olay) => setPlakaGirdi(olay.target.value)}
            />
          </div>
          {islemHatasi ? (
            <p className="hata" role="alert">
              {islemHatasi}
            </p>
          ) : null}
          <button type="submit" className="dugme">
            Devam et
          </button>
        </form>
      </section>
    );
  }

  if (durum === "yukleniyor") {
    return (
      <section className="kart kart--genis" aria-labelledby="surucu-baslik">
        <header className="baslik">
          <h1 id="surucu-baslik">Sürücü</h1>
          <p className="altbaslik">{plaka}</p>
        </header>
        <AsyncState durum="yukleniyor" />
      </section>
    );
  }

  if (durum === "hata") {
    return (
      <section className="kart kart--genis" aria-labelledby="surucu-baslik">
        <header className="baslik">
          <h1 id="surucu-baslik">Sürücü</h1>
          <p className="altbaslik">{plaka}</p>
        </header>
        <AsyncState durum="hata" mesaj={hata ?? undefined} onTekrar={() => void yukle()} />
      </section>
    );
  }

  const borcKalemleri = borclar?.items ?? [];

  return (
    <div className="ekran">
      <section className="kart kart--genis" aria-labelledby="surucu-baslik">
        <header className="baslik">
          <h1 id="surucu-baslik">Sürücü</h1>
          <p className="altbaslik">{plaka}</p>
        </header>

        {islemHatasi ? (
          <p className="hata" role="alert">
            {islemHatasi}
          </p>
        ) : null}
        {basari ? (
          <p className="basari" role="status">
            {basari}
          </p>
        ) : null}

        <h2>Borçlar</h2>
        <p className="buyuk-tutar">Toplam: {kurus(borclar?.total_kurus)}</p>
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
                  <p className="soluk">{tarihSaat(kalem.created_at)}</p>
                </div>
                <span className="rol-etiketi">Lot {metin(kalem.lot_id)}</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="abonelikler-baslik">
        <header className="baslik">
          <h2 id="abonelikler-baslik">Abonelikler</h2>
          <p className="altbaslik">Aylık planlar ve dönemler</p>
        </header>

        {abonelikler.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {abonelikler.map((abonelik, sira) => {
              const kimlik = abonelik.id;
              const seciliPlan =
                (kimlik !== undefined ? planSecimleri[kimlik] : undefined) ??
                abonelik.plan_code ??
                "";
              return (
                <li key={kimlik ?? sira} className="kayit-satiri kayit-satiri--dikey">
                  <div className="kayit-icerik">
                    <p className="kayit-basligi">
                      {metin(abonelik.plan_code)} · {metin(abonelik.status)}
                    </p>
                    <p className="soluk">
                      {tarih(abonelik.current_period_start)} –{" "}
                      {tarih(abonelik.current_period_end)}
                    </p>
                    <p className="soluk">{metin(abonelik.plate)}</p>
                  </div>
                  <div className="satir-islemleri">
                    <div className="alan alan--satir">
                      <label htmlFor={`plan-${kimlik ?? sira}`}>Plan ({metin(kimlik)})</label>
                      <select
                        id={`plan-${kimlik ?? sira}`}
                        value={seciliPlan}
                        onChange={(olay) => {
                          if (kimlik === undefined) {
                            return;
                          }
                          setPlanSecimleri((oncekiler) => ({
                            ...oncekiler,
                            [kimlik]: olay.target.value,
                          }));
                        }}
                      >
                        {planlar.map((plan) => (
                          <option key={plan.code} value={plan.code}>
                            {metin(plan.code)} · {kurus(plan.monthly_price_kurus)}
                          </option>
                        ))}
                      </select>
                    </div>
                    <button
                      type="button"
                      className="dugme dugme--ikincil dugme--dar"
                      disabled={kimlik === undefined || islemde !== null || !seciliPlan}
                      onClick={() => {
                        if (kimlik === undefined) {
                          return;
                        }
                        void islemYap(`plan-${kimlik}`, async () => {
                          await apiFetch<Abonelik>(
                            `/subscriptions/${kimlik}/change-plan`,
                            { method: "POST", body: { plan_code: seciliPlan } }
                          );
                          setBasari("Plan değiştirildi.");
                        });
                      }}
                    >
                      Planı değiştir
                    </button>
                    <button
                      type="button"
                      className="dugme dugme--ikincil dugme--dar"
                      disabled={kimlik === undefined || islemde !== null}
                      onClick={() => {
                        if (kimlik === undefined) {
                          return;
                        }
                        void islemYap(`iptal-${kimlik}`, async () => {
                          await apiFetch<void>(`/subscriptions/${kimlik}/cancel`, {
                            method: "POST",
                          });
                          setBasari("Abonelik iptal edildi.");
                        });
                      }}
                    >
                      İptal et
                    </button>
                  </div>
                </li>
              );
            })}
          </ul>
        )}

        <form className="form" onSubmit={abonelikOlustur} noValidate>
          <div className="alan">
            <label htmlFor="yeni-abonelik-plani">Yeni abonelik planı</label>
            <select
              id="yeni-abonelik-plani"
              value={yeniPlan}
              onChange={(olay) => setYeniPlan(olay.target.value)}
            >
              <option value="">Plan seçin</option>
              {planlar.map((plan) => (
                <option key={plan.code} value={plan.code}>
                  {metin(plan.code)} · {kurus(plan.monthly_price_kurus)}
                </option>
              ))}
            </select>
          </div>
          <button type="submit" className="dugme" disabled={islemde !== null}>
            {islemde === "olustur" ? "Oluşturuluyor…" : "Abonelik oluştur"}
          </button>
        </form>
      </section>

      <section className="kart kart--genis" aria-labelledby="faturalar-baslik">
        <header className="baslik">
          <h2 id="faturalar-baslik">Faturalar</h2>
          <p className="altbaslik">Abonelik dönem faturaları</p>
        </header>
        {faturalar.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {faturalar.map((fatura, sira) => (
              <li key={fatura.id ?? sira} className="kayit-satiri">
                <div>
                  <p className="kayit-basligi">
                    {metin(fatura.kind)} · {kurus(fatura.amount_kurus)}
                  </p>
                  <p className="soluk">
                    {tarih(fatura.period_start)} · {metin(fatura.status)}
                  </p>
                </div>
                <span className="rol-etiketi">{metin(fatura.attempts)} deneme</span>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="rezervasyon-baslik">
        <header className="baslik">
          <h2 id="rezervasyon-baslik">Rezervasyon</h2>
          <p className="altbaslik">Yer ayırt ve gerektiğinde iptal et</p>
        </header>

        {rezervasyon ? (
          <div className="durum-bildirimi" role="status">
            <p>
              Rezervasyon {metin(rezervasyon.id)} · {metin(rezervasyon.status)}
            </p>
            <p className="soluk">
              {tarihSaat(rezervasyon.reserved_at)} · son geçerlilik{" "}
              {tarihSaat(rezervasyon.expires_at)}
            </p>
            <button
              type="button"
              className="dugme dugme--ikincil"
              disabled={rezervasyon.id === undefined || islemde !== null}
              onClick={() => {
                const kimlik = rezervasyon.id;
                if (kimlik === undefined) {
                  return;
                }
                void islemYap("rezervasyon-iptal", async () => {
                  await apiFetch<void>(`/reservations/${kimlik}`, { method: "DELETE" });
                  setRezervasyon(null);
                  setBasari("Rezervasyon iptal edildi.");
                });
              }}
            >
              Rezervasyonu iptal et
            </button>
          </div>
        ) : (
          <form
            className="form"
            onSubmit={(olay) => {
              olay.preventDefault();
              if (!rezervasyonLot) {
                setIslemHatasi("Lot seçin.");
                return;
              }
              void islemYap("rezervasyon", async () => {
                const yeni = await apiFetch<Rezervasyon>("/reservations", {
                  method: "POST",
                  body: { lot_id: Number(rezervasyonLot), plate: plaka },
                });
                setRezervasyon(yeni);
                setBasari("Rezervasyon oluşturuldu.");
              });
            }}
            noValidate
          >
            <div className="alan">
              <label htmlFor="rezervasyon-lot">Rezervasyon lotu</label>
              <select
                id="rezervasyon-lot"
                value={rezervasyonLot}
                onChange={(olay) => setRezervasyonLot(olay.target.value)}
              >
                <option value="">Lot seçin</option>
                {lotlar.map((lot) => (
                  <option key={lot.id} value={lot.id}>
                    {metin(lot.code)} · {metin(lot.name)}
                  </option>
                ))}
              </select>
            </div>
            <button type="submit" className="dugme" disabled={islemde !== null}>
              {islemde === "rezervasyon" ? "Oluşturuluyor…" : "Rezervasyon oluştur"}
            </button>
          </form>
        )}
      </section>
    </div>
  );
}
