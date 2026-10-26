import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import { DolulukCubugu } from "../components/DolulukCubugu";
import { KaraListePaneli } from "../components/KaraListePaneli";
import { SayacKaymasiListesi } from "../components/SayacKaymasiListesi";
import { bugun, kurus, metin } from "../format";
import type {
  Doluluk,
  GunlukRapor,
  KaraListeKaydi,
  Lot,
  SayacKaymasi,
  Sayfali,
} from "../api/tipler";

type Durum = "yukleniyor" | "hazir" | "hata";

function hataMesaji(hata: unknown, yedek: string): string {
  return hata instanceof Error ? hata.message : yedek;
}

export function MudurPage() {
  const [durum, setDurum] = useState<Durum>("yukleniyor");
  const [hata, setHata] = useState<string | null>(null);
  const [doluluklar, setDoluluklar] = useState<Doluluk[]>([]);
  const [karaListe, setKaraListe] = useState<KaraListeKaydi[]>([]);
  const [sonrakiKaraListeId, setSonrakiKaraListeId] = useState<number | null>(null);
  const [karaListeYukleniyor, setKaraListeYukleniyor] = useState(false);
  const [kaymalar, setKaymalar] = useState<SayacKaymasi[]>([]);
  const [lotlar, setLotlar] = useState<Lot[]>([]);
  const [dolulukHatasi, setDolulukHatasi] = useState<string | null>(null);
  const [raporGun, setRaporGun] = useState(bugun());
  const [raporLot, setRaporLot] = useState("");
  const [rapor, setRapor] = useState<GunlukRapor | null>(null);
  const [islemde, setIslemde] = useState<string | null>(null);
  const [islemHatasi, setIslemHatasi] = useState<string | null>(null);
  const istekNo = useRef(0);

  const dolulukYenile = useCallback(async () => {
    try {
      const yanit = await apiFetch<Doluluk[]>("/occupancy");
      setDoluluklar(Array.isArray(yanit) ? yanit : []);
      setDolulukHatasi(null);
    } catch (istekHatasi) {
      setDolulukHatasi(hataMesaji(istekHatasi, "Doluluk yenilenemedi."));
    }
  }, []);

  const yukle = useCallback(async () => {
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    try {
      const [dolulukYaniti, karaYaniti, kaymaYaniti, lotYaniti] = await Promise.all([
        apiFetch<Doluluk[]>("/occupancy"),
        apiFetch<Sayfali<KaraListeKaydi>>("/blacklist?limit=20"),
        apiFetch<Sayfali<SayacKaymasi>>("/admin/counter-drifts?limit=20"),
        apiFetch<Lot[]>("/lots"),
      ]);
      if (buIstek !== istekNo.current) {
        return;
      }
      setDoluluklar(Array.isArray(dolulukYaniti) ? dolulukYaniti : []);
      setKaraListe(Array.isArray(karaYaniti.items) ? karaYaniti.items : []);
      setSonrakiKaraListeId(
        typeof karaYaniti.next_after_id === "number" ? karaYaniti.next_after_id : null
      );
      setKaymalar(Array.isArray(kaymaYaniti.items) ? kaymaYaniti.items : []);
      setLotlar(Array.isArray(lotYaniti) ? lotYaniti : []);
      setDurum("hazir");
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(hataMesaji(istekHatasi, "Müdür ekranı yüklenemedi."));
      setDurum("hata");
    }
  }, []);

  useEffect(() => {
    void yukle();
  }, [yukle]);

  useEffect(() => {
    const zamanlayici = setInterval(() => {
      void dolulukYenile();
    }, 15000);
    return () => clearInterval(zamanlayici);
  }, [dolulukYenile]);

  const karaListeYenile = async () => {
    const yanit = await apiFetch<Sayfali<KaraListeKaydi>>("/blacklist?limit=20");
    setKaraListe(Array.isArray(yanit.items) ? yanit.items : []);
    setSonrakiKaraListeId(
      typeof yanit.next_after_id === "number" ? yanit.next_after_id : null
    );
  };

  const karaListeDahaFazla = async () => {
    if (sonrakiKaraListeId === null) {
      return;
    }
    setKaraListeYukleniyor(true);
    setIslemHatasi(null);
    try {
      const yanit = await apiFetch<Sayfali<KaraListeKaydi>>(
        `/blacklist?limit=20&after_id=${encodeURIComponent(String(sonrakiKaraListeId))}`
      );
      setKaraListe((oncekiler) => [
        ...oncekiler,
        ...(Array.isArray(yanit.items) ? yanit.items : []),
      ]);
      setSonrakiKaraListeId(
        typeof yanit.next_after_id === "number" ? yanit.next_after_id : null
      );
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Kara liste yüklenemedi."));
    } finally {
      setKaraListeYukleniyor(false);
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
      <section className="kart kart--genis" aria-labelledby="mudur-baslik">
        <header className="baslik">
          <h1 id="mudur-baslik">Müdür</h1>
          <p className="altbaslik">Doluluk panosu ve raporlar</p>
        </header>
        <AsyncState durum="yukleniyor" />
      </section>
    );
  }

  if (durum === "hata") {
    return (
      <section className="kart kart--genis" aria-labelledby="mudur-baslik">
        <header className="baslik">
          <h1 id="mudur-baslik">Müdür</h1>
          <p className="altbaslik">Doluluk panosu ve raporlar</p>
        </header>
        <AsyncState durum="hata" mesaj={hata ?? undefined} onTekrar={() => void yukle()} />
      </section>
    );
  }

  return (
    <div className="ekran">
      <section className="kart kart--genis" aria-labelledby="mudur-baslik">
        <header className="baslik">
          <h1 id="mudur-baslik">Müdür</h1>
          <p className="altbaslik">Doluluk panosu ve raporlar</p>
        </header>

        {islemHatasi ? (
          <p className="hata" role="alert">
            {islemHatasi}
          </p>
        ) : null}
        {dolulukHatasi ? (
          <p className="hata" role="alert">
            {dolulukHatasi}
          </p>
        ) : null}

        <h2>Doluluk panosu</h2>
        <p className="soluk">15 saniyede bir otomatik yenilenir.</p>
        {doluluklar.length === 0 ? (
          <p className="durum-bildirimi" role="status">
            Kayıt yok.
          </p>
        ) : (
          <ul className="doluluk-listesi">
            {doluluklar.map((kayit, sira) => (
              <li key={kayit.lot_id ?? sira} className="doluluk-karti">
                <p className="kayit-basligi">
                  {metin(kayit.code)} · {metin(kayit.lot_id)}
                </p>
                <DolulukCubugu
                  dolu={kayit.occupied}
                  kapasite={kayit.capacity}
                  etiket={metin(kayit.code)}
                />
                <p className="soluk">{metin(kayit.available)} boş yer</p>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="kart kart--genis" aria-labelledby="mudur-rapor-baslik">
        <header className="baslik">
          <h2 id="mudur-rapor-baslik">Günlük rapor</h2>
          <p className="altbaslik">Gün ve lot seçerek rapor al</p>
        </header>
        <form className="form" onSubmit={raporGetir} noValidate>
          <div className="alan">
            <label htmlFor="mudur-rapor-gun">Gün</label>
            <input
              id="mudur-rapor-gun"
              type="date"
              value={raporGun}
              onChange={(olay) => setRaporGun(olay.target.value)}
            />
          </div>
          <div className="alan">
            <label htmlFor="mudur-rapor-lot">Lot</label>
            <select
              id="mudur-rapor-lot"
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
          <button type="submit" className="dugme" disabled={islemde !== null}>
            {islemde === "rapor" ? "Getiriliyor…" : "Raporu getir"}
          </button>
        </form>

        {rapor ? (
          <dl className="ayrintilar">
            <div className="satir">
              <dt>Giriş / çıkış</dt>
              <dd>
                {metin(rapor.entries)} / {metin(rapor.exits)}
              </dd>
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

      <KaraListePaneli
        kayitlar={karaListe}
        hata={islemHatasi}
        onEkle={async (plaka, gerekce) => {
          await apiFetch<unknown>("/blacklist", {
            method: "POST",
            body: { plate: plaka, reason: gerekce },
          });
          await karaListeYenile();
        }}
        onKaldir={async (id) => {
          await apiFetch<void>(`/blacklist/${id}`, { method: "DELETE" });
          await karaListeYenile();
        }}
      />
      {sonrakiKaraListeId !== null ? (
        <button
          type="button"
          className="dugme dugme--ikincil"
          disabled={karaListeYukleniyor}
          onClick={() => void karaListeDahaFazla()}
        >
          {karaListeYukleniyor ? "Yükleniyor…" : "Daha fazla"}
        </button>
      ) : null}

      <SayacKaymasiListesi kayitlar={kaymalar} />
    </div>
  );
}
