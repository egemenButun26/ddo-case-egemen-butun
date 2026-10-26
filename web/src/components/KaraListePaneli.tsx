import { useState, type FormEvent } from "react";
import { tarihSaat, metin } from "../format";
import type { KaraListeKaydi } from "../api/tipler";

type KaraListePaneliProps = {
  kayitlar: KaraListeKaydi[];
  onEkle: (plaka: string, gerekce: string) => Promise<void>;
  onKaldir: (id: number) => Promise<void>;
  hata?: string | null;
};

export function KaraListePaneli({ kayitlar, onEkle, onKaldir, hata }: KaraListePaneliProps) {
  const [plaka, setPlaka] = useState("");
  const [gerekce, setGerekce] = useState("");
  const [formHatasi, setFormHatasi] = useState<string | null>(null);
  const [islemde, setIslemde] = useState(false);
  const [kaldirilan, setKaldirilan] = useState<number | null>(null);

  const ekle = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    const temizPlaka = plaka.trim();
    if (!temizPlaka) {
      setFormHatasi("Plaka gerekli.");
      return;
    }
    setFormHatasi(null);
    setIslemde(true);
    try {
      await onEkle(temizPlaka, gerekce.trim());
      setPlaka("");
      setGerekce("");
    } catch (istekHatasi) {
      setFormHatasi(
        istekHatasi instanceof Error ? istekHatasi.message : "Kara listeye eklenemedi."
      );
    } finally {
      setIslemde(false);
    }
  };

  const kaldir = async (id: number) => {
    setFormHatasi(null);
    setKaldirilan(id);
    try {
      await onKaldir(id);
    } catch (istekHatasi) {
      setFormHatasi(
        istekHatasi instanceof Error ? istekHatasi.message : "Kayıt kaldırılamadı."
      );
    } finally {
      setKaldirilan(null);
    }
  };

  return (
    <section className="kart kart--genis" aria-labelledby="kara-liste-baslik">
      <header className="baslik">
        <h2 id="kara-liste-baslik">Kara liste</h2>
        <p className="altbaslik">Girişi engellenecek plakalar</p>
      </header>

      <form className="form" onSubmit={ekle} noValidate>
        <div className="alan">
          <label htmlFor="kara-liste-plaka">Plaka</label>
          <input
            id="kara-liste-plaka"
            name="plaka"
            value={plaka}
            aria-invalid={formHatasi ? true : undefined}
            onChange={(olay) => setPlaka(olay.target.value)}
          />
        </div>
        <div className="alan">
          <label htmlFor="kara-liste-gerekce">Gerekçe</label>
          <input
            id="kara-liste-gerekce"
            name="gerekce"
            value={gerekce}
            onChange={(olay) => setGerekce(olay.target.value)}
          />
        </div>
        {formHatasi ?? hata ? (
          <p className="hata" role="alert">
            {formHatasi ?? hata}
          </p>
        ) : null}
        <button type="submit" className="dugme" disabled={islemde}>
          {islemde ? "Ekleniyor…" : "Kara listeye ekle"}
        </button>
      </form>

      {kayitlar.length === 0 ? (
        <p className="durum-bildirimi" role="status">
          Kayıt yok.
        </p>
      ) : (
        <ul className="kayit-listesi">
          {kayitlar.map((kayit, sira) => (
            <li key={kayit.id ?? sira} className="kayit-satiri">
              <div>
                <p className="kayit-basligi">{metin(kayit.plate)}</p>
                <p className="soluk">{metin(kayit.reason)}</p>
                <p className="soluk">
                  {tarihSaat(kayit.created_at)} · {kayit.active === false ? "pasif" : "aktif"}
                </p>
              </div>
              <button
                type="button"
                className="dugme dugme--ikincil dugme--dar"
                disabled={kayit.id === undefined || kaldirilan === kayit.id}
                onClick={() => {
                  if (kayit.id !== undefined) {
                    void kaldir(kayit.id);
                  }
                }}
              >
                Kaldır
              </button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
