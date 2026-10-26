import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import { bugun, kurus, metin, tarih, tarihSaat } from "../format";
import type {
  Fatura,
  GunlukRapor,
  Lot,
  Mutabakat,
  Odeme,
  Sayfali,
} from "../api/tipler";

type Durum = "yukleniyor" | "hazir" | "hata";

function hataMesaji(hata: unknown, yedek: string): string {
  return hata instanceof Error ? hata.message : yedek;
}

function yeniAnahtar(): string {
  if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
    return crypto.randomUUID();
  }
  return `iade-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

export function MuhasebePage() {
  const [durum, setDurum] = useState<Durum>("yukleniyor");
  const [hata, setHata] = useState<string | null>(null);
  const [odemeler, setOdemeler] = useState<Odeme[]>([]);
  const [sonrakiOdemeId, setSonrakiOdemeId] = useState<number | null>(null);
  const [odemeYukleniyor, setOdemeYukleniyor] = useState(false);
  const [faturalar, setFaturalar] = useState<Fatura[]>([]);
  const [lotlar, setLotlar] = useState<Lot[]>([]);
  const [iadeOdeme, setIadeOdeme] = useState("");
  const [iadeTutar, setIadeTutar] = useState("");
  const [donemBasi, setDonemBasi] = useState(bugun());
  const [mutabakatGun, setMutabakatGun] = useState(bugun());
  const [mutabakat, setMutabakat] = useState<Mutabakat | null>(null);
  const [raporGun, setRaporGun] = useState(bugun());
  const [raporLot, setRaporLot] = useState("");
  const [rapor, setRapor] = useState<GunlukRapor | null>(null);
  const [islemde, setIslemde] = useState<string | null>(null);
  const [islemHatasi, setIslemHatasi] = useState<string | null>(null);
  const [basari, setBasari] = useState<string | null>(null);
  const istekNo = useRef(0);
  const iadeAnahtari = useRef<string | null>(null);

  const yukle = useCallback(async () => {
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    try {
      const [odemeYaniti, faturaYaniti, lotYaniti] = await Promise.all([
        apiFetch<Sayfali<Odeme>>("/payments?limit=20"),
        apiFetch<Sayfali<Fatura>>("/invoices?limit=20"),
        apiFetch<Lot[]>("/lots"),
      ]);
      if (buIstek !== istekNo.current) {
        return;
      }
      setOdemeler(Array.isArray(odemeYaniti.items) ? odemeYaniti.items : []);
      setSonrakiOdemeId(
        typeof odemeYaniti.next_after_id === "number" ? odemeYaniti.next_after_id : null
      );
      setFaturalar(Array.isArray(faturaYaniti.items) ? faturaYaniti.items : []);
      setLotlar(Array.isArray(lotYaniti) ? lotYaniti : []);
      setDurum("hazir");
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(hataMesaji(istekHatasi, "Muhasebe ekranı yüklenemedi."));
      setDurum("hata");
    }
  }, []);

  useEffect(() => {
    void yukle();
  }, [yukle]);

  const dahaFazlaOdeme = async () => {
    if (sonrakiOdemeId === null) {
      return;
    }
    setOdemeYukleniyor(true);
    setIslemHatasi(null);
    try {
      const yanit = await apiFetch<Sayfali<Odeme>>(
        `/payments?limit=20&after_id=${encodeURIComponent(String(sonrakiOdemeId))}`
      );
      setOdemeler((oncekiler) => [
        ...oncekiler,
        ...(Array.isArray(yanit.items) ? yanit.items : []),
      ]);
      setSonrakiOdemeId(
        typeof yanit.next_after_id === "number" ? yanit.next_after_id : null
      );
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Ödemeler yüklenemedi."));
    } finally {
      setOdemeYukleniyor(false);
    }
  };

  const iadeEt = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    const odemeId = Number(iadeOdeme);
    const tutar = Number(iadeTutar);
    if (!iadeOdeme || !Number.isFinite(odemeId)) {
      setIslemHatasi("Ödeme seçin.");
      return;
    }
    if (!Number.isInteger(tutar) || tutar <= 0) {
      setIslemHatasi("Geçerli bir iade tutarı (kuruş) girin.");
      return;
    }
    if (!iadeAnahtari.current) {
      iadeAnahtari.current = yeniAnahtar();
    }
    setIslemHatasi(null);
    setBasari(null);
    setIslemde("iade");
    try {
      await apiFetch<unknown>("/refunds", {
        method: "POST",
        headers: { "Idempotency-Key": iadeAnahtari.current },
        body: { payment_id: odemeId, amount_kurus: tutar, reason: "manual" },
      });
      iadeAnahtari.current = null;
      setBasari("İade isteği alındı.");
      setIadeTutar("");
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "İade isteği gönderilemedi."));
    } finally {
      setIslemde(null);
    }
  };

  const donemSonu = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    if (!donemBasi) {
      setIslemHatasi("Dönem başı gerekli.");
      return;
    }
    setIslemHatasi(null);
    setBasari(null);
    setIslemde("donem");
    try {
      await apiFetch<unknown>("/billing/period-end", {
        method: "POST",
        body: { period_start: donemBasi },
      });
      setBasari("Dönem sonu çalıştırıldı.");
      await yukle();
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Dönem sonu çalıştırılamadı."));
    } finally {
      setIslemde(null);
    }
  };

  const mutabakatGetir = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    if (!mutabakatGun) {
      setIslemHatasi("Gün seçin.");
      return;
    }
    setIslemHatasi(null);
    setIslemde("mutabakat");
    try {
      const yanit = await apiFetch<Mutabakat>(`/reconciliation/${mutabakatGun}`);
      setMutabakat(yanit);
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Mutabakat getirilemedi."));
    } finally {
      setIslemde(null);
    }
  };

  const raporGetir = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    if (!raporGun) {
      setIslemHatasi("Gün seçin.");
      return;
    }
    setIslemHatasi(null);
    setIslemde("rapor");
    try {
      const lotSorgu = raporLot ? `&lot_id=${encodeURIComponent(raporLot)}` : "";
      const yanit = await apiFetch<GunlukRapor>(
        `/reports/daily?day=${raporGun}${lotSorgu}`
      );
      setRapor(yanit);
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Rapor getirilemedi."));
    } finally {
      setIslemde(null);
    }
  };

  if (durum === "yukleniyor") {
    return (
      <section className="kart kart--genis" aria-labelledby="muhasebe-baslik">
        <header className="baslik">
          <h1 id="muhasebe-baslik">Muhasebe</h1>
          <p className="altbaslik">Tahsilat ve mutabakat işlemleri</p>
        </header>
        <AsyncState durum="yukleniyor" />
      </section>
    );
  }

  if (durum === "hata") {
    return (
      <section className="kart kart--genis" aria-labelledby="muhasebe-baslik">
        <header className="baslik">
          <h1 id="muhasebe-baslik">Muhasebe</h1>
          <p className="altbaslik">Tahsilat ve mutabakat işlemleri</p>
        </header>
        <AsyncState durum="hata" mesaj={hata ?? undefined} onTekrar={() => void yukle()} />
      </section>
    );
  }

  return (
    <div className="ekran">
      <section className="kart kart--genis" aria-labelledby="muhasebe-baslik">
        <header className="baslik">
          <h1 id="muhasebe-baslik">Muhasebe</h1>
          <p className="altbaslik">Tahsilat ve mutabakat işlemleri</p>
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

        <h2>Ödemeler</h2>
        {odemeler.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="kayit-listesi">
            {odemeler.map((odeme, sira) => (
              <li key={odeme.id ?? sira} className="kayit-satiri">
                <div>
                  <p className="kayit-basligi">
                    {kurus(odeme.amount_kurus)} · {metin(odeme.status)}
                  </p>
                  <p className="soluk">
                    {tarihSaat(odeme.requested_at)} · {metin(odeme.provider_ref)}
                  </p>
                  <p className="soluk">
                    Oturum {metin(odeme.session_id)}
                    {odeme.late_approval ? " · geç onay" : ""}
                  </p>
                </div>
                <span className="rol-etiketi">#{metin(odeme.id)}</span>
              </li>
            ))}
          </ul>
        )}
        {sonrakiOdemeId !== null ? (
          <button
            type="button"
            className="dugme dugme--ikincil"
            disabled={odemeYukleniyor}
            onClick={() => void dahaFazlaOdeme()}
          >
            {odemeYukleniyor ? "Yükleniyor…" : "Daha fazla"}
          </button>
        ) : null}
      </section>

      <section className="kart kart--genis" aria-labelledby="iade-baslik">
        <header className="baslik">
          <h2 id="iade-baslik">Elle iade</h2>
          <p className="altbaslik">Çift gönderim engellenir (Idempotency-Key)</p>
        </header>
        <form className="form" onSubmit={iadeEt} noValidate>
          <div className="alan">
            <label htmlFor="iade-odeme">Ödeme</label>
            <select
              id="iade-odeme"
              value={iadeOdeme}
              onChange={(olay) => setIadeOdeme(olay.target.value)}
            >
              <option value="">Ödeme seçin</option>
              {odemeler.map((odeme, sira) => (
                <option key={odeme.id ?? sira} value={odeme.id}>
                  #{metin(odeme.id)} · {kurus(odeme.amount_kurus)}
                </option>
              ))}
            </select>
          </div>
          <div className="alan">
            <label htmlFor="iade-tutar">İade tutarı (kuruş)</label>
            <input
              id="iade-tutar"
              inputMode="numeric"
              value={iadeTutar}
              onChange={(olay) => setIadeTutar(olay.target.value)}
            />
          </div>
          <button type="submit" className="dugme" disabled={islemde !== null}>
            {islemde === "iade" ? "Gönderiliyor…" : "İade et"}
          </button>
        </form>
      </section>

      <section className="kart kart--genis" aria-labelledby="muhasebe-fatura-baslik">
        <header className="baslik">
          <h2 id="muhasebe-fatura-baslik">Faturalar</h2>
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
                  <p className="soluk">
                    {metin(fatura.attempts)} deneme · sonraki {tarihSaat(fatura.next_retry_at)}
                  </p>
                </div>
              </li>
            ))}
          </ul>
        )}

        <form className="form" onSubmit={donemSonu} noValidate>
          <div className="alan">
            <label htmlFor="donem-basi">Dönem başı</label>
            <input
              id="donem-basi"
              type="date"
              value={donemBasi}
              onChange={(olay) => setDonemBasi(olay.target.value)}
            />
          </div>
          <button type="submit" className="dugme" disabled={islemde !== null}>
            {islemde === "donem" ? "Çalıştırılıyor…" : "Dönem sonu çalıştır"}
          </button>
        </form>
      </section>

      <section className="kart kart--genis" aria-labelledby="mutabakat-baslik">
        <header className="baslik">
          <h2 id="mutabakat-baslik">Mutabakat</h2>
          <p className="altbaslik">Kasa ve sağlayıcı toplamlarını karşılaştır</p>
        </header>
        <form className="form" onSubmit={mutabakatGetir} noValidate>
          <div className="alan">
            <label htmlFor="mutabakat-gun">Gün</label>
            <input
              id="mutabakat-gun"
              type="date"
              value={mutabakatGun}
              onChange={(olay) => setMutabakatGun(olay.target.value)}
            />
          </div>
          <button type="submit" className="dugme dugme--ikincil" disabled={islemde !== null}>
            {islemde === "mutabakat" ? "Getiriliyor…" : "Mutabakatı getir"}
          </button>
        </form>

        {mutabakat ? (
          <dl className="ayrintilar">
            <div className="satir">
              <dt>Gün</dt>
              <dd>{metin(mutabakat.day)}</dd>
            </div>
            <div className="satir">
              <dt>Bizim toplam</dt>
              <dd>{kurus(mutabakat.our_total_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Sağlayıcı toplam</dt>
              <dd>{kurus(mutabakat.provider_total_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Fark</dt>
              <dd>{kurus(mutabakat.diff_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Eşleşen</dt>
              <dd>{metin(mutabakat.matched)}</dd>
            </div>
            <div className="satir">
              <dt>Sağlayıcıda eksik</dt>
              <dd>{metin(mutabakat.missing_in_provider?.length)}</dd>
            </div>
            <div className="satir">
              <dt>Bizde eksik</dt>
              <dd>{metin(mutabakat.missing_in_ours?.length)}</dd>
            </div>
            <div className="satir">
              <dt>Tutar uyuşmazlığı</dt>
              <dd>{metin(mutabakat.amount_mismatch?.length)}</dd>
            </div>
          </dl>
        ) : null}
      </section>

      <section className="kart kart--genis" aria-labelledby="muhasebe-rapor-baslik">
        <header className="baslik">
          <h2 id="muhasebe-rapor-baslik">Günlük rapor</h2>
          <p className="altbaslik">Giriş, çıkış ve tahsilat özeti</p>
        </header>
        <form className="form" onSubmit={raporGetir} noValidate>
          <div className="alan">
            <label htmlFor="rapor-gun">Gün</label>
            <input
              id="rapor-gun"
              type="date"
              value={raporGun}
              onChange={(olay) => setRaporGun(olay.target.value)}
            />
          </div>
          <div className="alan">
            <label htmlFor="rapor-lot">Lot</label>
            <select
              id="rapor-lot"
              value={raporLot}
              onChange={(olay) => setRaporLot(olay.target.value)}
            >
              <option value="">Tüm lotlar</option>
              {lotlar.map((lot) => (
                <option key={lot.id} value={lot.id}>
                  {metin(lot.code)} · {metin(lot.name)}
                </option>
              ))}
            </select>
          </div>
          <button type="submit" className="dugme dugme--ikincil" disabled={islemde !== null}>
            {islemde === "rapor" ? "Getiriliyor…" : "Raporu getir"}
          </button>
        </form>

        {rapor ? (
          <dl className="ayrintilar">
            <div className="satir">
              <dt>Giriş</dt>
              <dd>{metin(rapor.entries)}</dd>
            </div>
            <div className="satir">
              <dt>Çıkış</dt>
              <dd>{metin(rapor.exits)}</dd>
            </div>
            <div className="satir">
              <dt>Kapanan oturum</dt>
              <dd>{metin(rapor.closed_sessions)}</dd>
            </div>
            <div className="satir">
              <dt>Tahakkuk</dt>
              <dd>{kurus(rapor.charged_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Tahsilat</dt>
              <dd>{kurus(rapor.collected_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Açık borç</dt>
              <dd>{kurus(rapor.open_debt_kurus)}</dd>
            </div>
            <div className="satir">
              <dt>Açık inceleme</dt>
              <dd>{metin(rapor.open_reviews)}</dd>
            </div>
          </dl>
        ) : null}
      </section>
    </div>
  );
}
