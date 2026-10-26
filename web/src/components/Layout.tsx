import { useState } from "react";
import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { ROL_ANA_SAYFASI, useAuth, type Rol } from "../auth/AuthContext";

const ROL_ETIKETLERI: Record<Rol, string> = {
  driver: "Sürücü",
  attendant: "Görevli",
  accountant: "Muhasebe",
  manager: "Müdür",
  admin: "Yönetici",
};

export function Layout() {
  const { kullanici, logout } = useAuth();
  const navigate = useNavigate();
  const [menuAcik, setMenuAcik] = useState(false);

  const cikisYap = async () => {
    await logout();
    navigate("/giris", { replace: true });
  };

  const baglantiSinifi = ({ isActive }: { isActive: boolean }) =>
    isActive ? "menu-baglantisi menu-baglantisi--aktif" : "menu-baglantisi";

  return (
    <div className="uygulama">
      <header className="ust-menu">
        <div className="ust-menu-icerik">
          <NavLink to="/" className="marka" onClick={() => setMenuAcik(false)}>
            ParkAkıl
          </NavLink>
          <button
            type="button"
            className="menu-dugmesi"
            aria-expanded={menuAcik}
            aria-controls="ana-menu"
            onClick={() => setMenuAcik((acik) => !acik)}
          >
            Menü
          </button>
          <nav
            id="ana-menu"
            className={menuAcik ? "menu menu--acik" : "menu"}
            aria-label="Ana menü"
          >
            <NavLink to="/" end className={baglantiSinifi} onClick={() => setMenuAcik(false)}>
              Sistem durumu
            </NavLink>
            {kullanici ? (
              <NavLink
                to={ROL_ANA_SAYFASI[kullanici.role]}
                className={baglantiSinifi}
                onClick={() => setMenuAcik(false)}
              >
                {ROL_ETIKETLERI[kullanici.role]}
              </NavLink>
            ) : null}
            {kullanici ? (
              <button type="button" className="cikis-dugmesi" onClick={cikisYap}>
                Çıkış yap
              </button>
            ) : (
              <NavLink to="/giris" className={baglantiSinifi} onClick={() => setMenuAcik(false)}>
                Giriş
              </NavLink>
            )}
          </nav>
        </div>
        {kullanici ? (
          <p className="oturum-bilgisi">
            {kullanici.name} · {ROL_ETIKETLERI[kullanici.role]}
          </p>
        ) : null}
      </header>
      <main className="icerik">
        <Outlet />
      </main>
    </div>
  );
}
