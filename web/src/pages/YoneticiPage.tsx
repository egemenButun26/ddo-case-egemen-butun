import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import { KaraListePaneli } from "../components/KaraListePaneli";
import { SayacKaymasiListesi } from "../components/SayacKaymasiListesi";
import { AdminUsersPage } from "./AdminUsersPage";
import { metin, tarihSaat } from "../format";
import type {
  KaraListeKaydi,
  Lot,
  SayacKaymasi,
  SayacYenidenSayim,
  Sayfali,
} from "../api/tipler";

type Durum = "yukleniyor" | "hazir" | "hata";

function hataMesaji(hata: unknown, yedek: string): string {
  return hata instanceof Error ? hata.message : yedek;
}

export function YoneticiPage() {
  const [durum, setDurum] = useState<Durum>("yukleniyor");
  const [hata, setHata] = useState<string | null>(null);
  const [karaListe, setKaraListe] = useState<KaraListeKaydi[]>([]);
  const [kaymalar, setKaymalar] = useState<SayacKaymasi[]>([]);
  const [lotlar, setLotlar] = useState<Lot[]>([]);
  const [sayimLot, setSayimLot] = useState("");
  const [sayim, setSayim] = useState<SayacYenidenSayim | null>(null);
  const [islemde, setIslemde] = useState<string | null>(null);
  const [islemHatasi, setIslemHatasi] = useState<string | null>(null);
  const [basari, setBasari] = useState<string | null>(null);
  const istekNo = useRef(0);

  const yukle = useCallback(async () => {
    const buIstek = ++istekNo.current;
    setDurum("yukleniyor");
    setHata(null);
    try {
      const [karaYaniti, kaymaYaniti, lotYaniti] = await Promise.all([
        apiFetch<Sayfali<KaraListeKaydi>>("/blacklist?limit=20"),
        apiFetch<Sayfali<SayacKaymasi>>("/admin/counter-drifts?limit=20"),
        apiFetch<Lot[]>("/lots"),
      ]);
      if (buIstek !== istekNo.current) {
        return;
      }
      setKaraListe(Array.isArray(karaYaniti.items) ? karaYaniti.items : []);
      setKaymalar(Array.isArray(kaymaYaniti.items) ? kaymaYaniti.items : []);
      setLotlar(Array.isArray(lotYaniti) ? lotYaniti : []);
      setDurum("hazir");
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(hataMesaji(istekHatasi, "Yönetici ekranı yüklenemedi."));
      setDurum("hata");
    }
  }, []);

  useEffect(() => {
    void yukle();
  }, [yukle]);

  const karaListeYenile = async () => {
    const yanit = await apiFetch<Sayfali<KaraListeKaydi>>("/blacklist?limit=20");
    setKaraListe(Array.isArray(yanit.items) ? yanit.items : []);
  };

  const yenidenSay = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    if (!sayimLot) {
      setIslemHatasi("Lot seçin.");
      return;
    }
    setIslemHatasi(null);
    setBasari(null);
    setIslemde("sayim");
    try {
      const yanit = await apiFetch<SayacYenidenSayim>(
        `/admin/occupancy/${encodeURIComponent(sayimLot)}/recount`,
        { method: "POST" }
      );
      setSayim(yanit);
      setBasari("Yeniden sayım tamamlandı.");
      await yukle();
    } catch (istekHatasi) {
      setIslemHatasi(hataMesaji(istekHatasi, "Yeniden sayım yapılamadı."));
    } finally {
      setIslemde(null);
    }
  };

  if (durum === "yukleniyor") {
    return (
      <section className="kart kart--genis" aria-labelledby="yonetici-baslik">
        <header className="baslik">
          <h1 id="yonetici-baslik">Yönetici</h1>
          <p className="altbaslik">Kullanıcılar, sayaçlar ve kara liste</p>
        </header>
        <AsyncState durum="yukleniyor" />
      </section>
    );
  }

  if (durum === "hata") {
    return (
      <section className="kart kart--genis" aria-labelledby="yonetici-baslik">
        <header className="baslik">
          <h1 id="yonetici-baslik">Yönetici</h1>
          <p className="altbaslik">Kullanıcılar, sayaçlar ve kara liste</p>
        </header>
        <AsyncState durum="hata" mesaj={hata ?? undefined} onTekrar={() => void yukle()} />
      </section>
    );
  }

  return (
    <div className="ekran">
      <section className="kart kart--genis" aria-labelledby="yonetici-baslik">
        <header className="baslik">
          <h1 id="yonetici-baslik">Yönetici</h1>
          <p className="altbaslik">Kullanıcılar, sayaçlar ve kara liste</p>
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

        <form className="form" onSubmit={yenidenSay} noValidate>
          <div className="alan">
            <label htmlFor="sayim-lot">Yeniden sayılacak lot</label>
            <select
              id="sayim-lot"
              value={sayimLot}
              onChange={(olay) => setSayimLot(olay.target.value)}
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
            {islemde === "sayim" ? "Sayılıyor…" : "Sayaç yeniden sayımı"}
          </button>
        </form>

        {sayim ? (
          <dl className="ayrintilar">
            <div className="satir">
              <dt>Depolanan</dt>
              <dd>{metin(sayim.stored)}</dd>
            </div>
            <div className="satir">
              <dt>Yeniden hesaplanan</dt>
              <dd>{metin(sayim.recomputed)}</dd>
            </div>
            <div className="satir">
              <dt>Fark</dt>
              <dd>{metin(sayim.drifted)}</dd>
            </div>
            <div className="satir">
              <dt>Düzeltilen</dt>
              <dd>{metin(sayim.corrected)}</dd>
            </div>
            <div className="satir">
              <dt>Sonuç zamanı</dt>
              <dd>{tarihSaat(new Date().toISOString())}</dd>
            </div>
          </dl>
        ) : null}
      </section>

      <AdminUsersPage />

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

      <SayacKaymasiListesi kayitlar={kaymalar} />
    </div>
  );
}
