import { metin, tarihSaat } from "../format";
import type { SayacKaymasi } from "../api/tipler";

export function SayacKaymasiListesi({
  kayitlar,
  baslik = "Sayaç kayması kayıtları",
}: {
  kayitlar: SayacKaymasi[];
  baslik?: string;
}) {
  return (
    <section className="kart kart--genis" aria-labelledby="sayac-kaymasi-baslik">
      <header className="baslik">
        <h2 id="sayac-kaymasi-baslik">{baslik}</h2>
        <p className="altbaslik">Depolanan ve yeniden hesaplanan doluluk farkları</p>
      </header>

      {kayitlar.length === 0 ? (
        <p className="durum-bildirimi" role="status">
          Kayıt yok.
        </p>
      ) : (
        <ul className="kayit-listesi">
          {kayitlar.map((kayit, sira) => (
            <li key={kayit.id ?? sira} className="kayit-satiri">
              <div>
                <p className="kayit-basligi">Lot {metin(kayit.lot_id)}</p>
                <p className="soluk">
                  Depolanan {metin(kayit.stored)} · hesaplanan {metin(kayit.recomputed)} · fark{" "}
                  {metin(kayit.drifted)}
                </p>
                <p className="soluk">{tarihSaat(kayit.created_at)}</p>
              </div>
              <span className="rol-etiketi">
                {kayit.corrected ? "düzeltildi" : "bekliyor"}
              </span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
