import { useState, type FormEvent } from "react";
import { Navigate, useNavigate } from "react-router-dom";
import { ApiError } from "../api/client";
import { ROL_ANA_SAYFASI, useAuth } from "../auth/AuthContext";

export function LoginPage() {
  const { kullanici, hazir, login } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState("");
  const [sifre, setSifre] = useState("");
  const [alanHatalari, setAlanHatalari] = useState<{ email?: string; sifre?: string }>({});
  const [hata, setHata] = useState<string | null>(null);
  const [gonderiliyor, setGonderiliyor] = useState(false);

  if (hazir && kullanici) {
    return <Navigate to={ROL_ANA_SAYFASI[kullanici.role]} replace />;
  }

  const gonder = async (olay: FormEvent<HTMLFormElement>) => {
    olay.preventDefault();
    const yeniHatalar: { email?: string; sifre?: string } = {};
    if (!email.trim()) {
      yeniHatalar.email = "E-posta adresi gerekli.";
    }
    if (!sifre) {
      yeniHatalar.sifre = "Şifre gerekli.";
    }
    setAlanHatalari(yeniHatalar);
    setHata(null);
    if (Object.keys(yeniHatalar).length > 0) {
      return;
    }

    setGonderiliyor(true);
    try {
      const kisi = await login(email.trim(), sifre);
      navigate(ROL_ANA_SAYFASI[kisi.role], { replace: true });
    } catch (istekHatasi) {
      if (istekHatasi instanceof ApiError && istekHatasi.status === 401) {
        setHata("E-posta veya şifre hatalı.");
      } else if (istekHatasi instanceof Error) {
        setHata(istekHatasi.message);
      } else {
        setHata("Giriş yapılamadı. Lütfen tekrar deneyin.");
      }
    } finally {
      setGonderiliyor(false);
    }
  };

  return (
    <section className="kart giris-karti" aria-labelledby="giris-baslik">
      <header className="baslik">
        <h1 id="giris-baslik">ParkAkıl</h1>
        <p className="altbaslik">Sisteme giriş yapın</p>
      </header>

      <form className="form" onSubmit={gonder} noValidate>
        <div className="alan">
          <label htmlFor="email">E-posta</label>
          <input
            id="email"
            name="email"
            type="email"
            autoComplete="username"
            value={email}
            aria-invalid={alanHatalari.email ? true : undefined}
            aria-describedby={alanHatalari.email ? "email-hatasi" : undefined}
            onChange={(olay) => setEmail(olay.target.value)}
          />
          {alanHatalari.email ? (
            <p id="email-hatasi" className="alan-hatasi" role="alert">
              {alanHatalari.email}
            </p>
          ) : null}
        </div>

        <div className="alan">
          <label htmlFor="sifre">Şifre</label>
          <input
            id="sifre"
            name="sifre"
            type="password"
            autoComplete="current-password"
            value={sifre}
            aria-invalid={alanHatalari.sifre ? true : undefined}
            aria-describedby={alanHatalari.sifre ? "sifre-hatasi" : undefined}
            onChange={(olay) => setSifre(olay.target.value)}
          />
          {alanHatalari.sifre ? (
            <p id="sifre-hatasi" className="alan-hatasi" role="alert">
              {alanHatalari.sifre}
            </p>
          ) : null}
        </div>

        {hata ? (
          <p className="hata" role="alert">
            {hata}
          </p>
        ) : null}

        <button type="submit" className="dugme" disabled={gonderiliyor}>
          {gonderiliyor ? "Giriş yapılıyor…" : "Giriş yap"}
        </button>
      </form>
    </section>
  );
}
