import { useCallback, useEffect, useRef, useState } from "react";
import { apiFetch } from "../api/client";
import { AsyncState } from "../components/AsyncState";
import type { Kullanici, Rol } from "../auth/AuthContext";

type KullaniciSayfasi = {
  users?: Kullanici[];
  items?: Kullanici[];
  next_after_id?: number | null;
  next_cursor?: number | null;
  has_more?: boolean;
};

const ROL_ADLARI: Record<Rol, string> = {
  driver: "Sürücü",
  attendant: "Görevli",
  accountant: "Muhasebe",
  manager: "Müdür",
  admin: "Yönetici",
};

export function AdminUsersPage() {
  const [kullanicilar, setKullanicilar] = useState<Kullanici[]>([]);
  const [sonrakiId, setSonrakiId] = useState<number | null>(null);
  const [ilkYukleniyor, setIlkYukleniyor] = useState(true);
  const [devamYukleniyor, setDevamYukleniyor] = useState(false);
  const [hata, setHata] = useState<string | null>(null);
  const istekNo = useRef(0);

  const sayfaYukle = useCallback(async (afterId: number | null, ilk: boolean) => {
    const buIstek = ++istekNo.current;
    if (ilk) {
      setIlkYukleniyor(true);
      setKullanicilar([]);
    } else {
      setDevamYukleniyor(true);
    }
    setHata(null);

    const sorgu =
      afterId === null
        ? "?limit=20"
        : `?limit=20&after_id=${encodeURIComponent(String(afterId))}`;

    try {
      const yanit = await apiFetch<KullaniciSayfasi>(`/admin/users${sorgu}`);
      if (buIstek !== istekNo.current) {
        return;
      }
      const gelenler = yanit.users ?? yanit.items ?? [];
      setKullanicilar((oncekiler) => (ilk ? gelenler : [...oncekiler, ...gelenler]));
      const yeniSonraki = yanit.next_after_id ?? yanit.next_cursor ?? null;
      if (yeniSonraki !== null) {
        setSonrakiId(yeniSonraki);
      } else if (yanit.has_more === true && gelenler.length > 0) {
        setSonrakiId(gelenler[gelenler.length - 1].id);
      } else {
        setSonrakiId(null);
      }
    } catch (istekHatasi) {
      if (buIstek !== istekNo.current) {
        return;
      }
      setHata(
        istekHatasi instanceof Error
          ? istekHatasi.message
          : "Kullanıcılar yüklenemedi."
      );
    } finally {
      if (buIstek === istekNo.current) {
        setIlkYukleniyor(false);
        setDevamYukleniyor(false);
      }
    }
  }, []);

  useEffect(() => {
    void sayfaYukle(null, true);
  }, [sayfaYukle]);

  const tekrarDene = () => {
    void sayfaYukle(kullanicilar.length === 0 ? null : sonrakiId, kullanicilar.length === 0);
  };

  return (
    <section className="kart kart--genis" aria-labelledby="kullanicilar-baslik">
      <header className="baslik">
        <h1 id="kullanicilar-baslik">Kullanıcılar</h1>
        <p className="altbaslik">Sistemdeki kullanıcı hesapları</p>
      </header>

      {ilkYukleniyor ? <AsyncState durum="yukleniyor" /> : null}

      {!ilkYukleniyor && hata && kullanicilar.length === 0 ? (
        <AsyncState durum="hata" mesaj={hata} onTekrar={tekrarDene} />
      ) : null}

      {!ilkYukleniyor && !hata && kullanicilar.length === 0 ? (
        <AsyncState durum="bos" bosMetni="Henüz kullanıcı yok." />
      ) : null}

      {kullanicilar.length > 0 ? (
        <ul className="kullanici-listesi">
          {kullanicilar.map((kisi) => (
            <li key={kisi.id} className="kullanici-satiri">
              <div>
                <p className="kullanici-adi">{kisi.name}</p>
                <p className="soluk">{kisi.email}</p>
              </div>
              <span className="rol-etiketi">{ROL_ADLARI[kisi.role] ?? kisi.role}</span>
            </li>
          ))}
        </ul>
      ) : null}

      {hata && kullanicilar.length > 0 ? (
        <AsyncState durum="hata" mesaj={hata} onTekrar={tekrarDene} />
      ) : null}

      {sonrakiId !== null ? (
        <button
          type="button"
          className="dugme dugme--ikincil"
          disabled={devamYukleniyor}
          onClick={() => void sayfaYukle(sonrakiId, false)}
        >
          {devamYukleniyor ? "Yükleniyor…" : "Daha fazla"}
        </button>
      ) : null}
    </section>
  );
}
