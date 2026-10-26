import { Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { RequireRole } from "./components/RequireRole";
import { HomePage, RoleHomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { AdminUsersPage } from "./pages/AdminUsersPage";

function Bulunamadi() {
  return (
    <section className="kart" aria-labelledby="bulunamadi-baslik">
      <h1 id="bulunamadi-baslik">Sayfa bulunamadı</h1>
      <p className="soluk">Aradığınız sayfa mevcut değil.</p>
    </section>
  );
}

export function App() {
  return (
    <Routes>
      <Route element={<Layout />}>
        <Route index element={<HomePage />} />
        <Route path="giris" element={<LoginPage />} />
        <Route
          path="surucu"
          element={
            <RequireRole rol="driver">
              <RoleHomePage
                baslik="Sürücü"
                aciklama="Borç sorgulama ve ödeme işlemleri"
              />
            </RequireRole>
          }
        />
        <Route
          path="gorevli"
          element={
            <RequireRole rol="attendant">
              <RoleHomePage
                baslik="Görevli"
                aciklama="İnceleme kuyruğu ve saha işlemleri"
              />
            </RequireRole>
          }
        />
        <Route
          path="muhasebe"
          element={
            <RequireRole rol="accountant">
              <RoleHomePage
                baslik="Muhasebe"
                aciklama="Tahsilat ve mutabakat işlemleri"
              />
            </RequireRole>
          }
        />
        <Route
          path="mudur"
          element={
            <RequireRole rol="manager">
              <RoleHomePage
                baslik="Müdür"
                aciklama="Doluluk panosu ve raporlar"
              />
            </RequireRole>
          }
        />
        <Route
          path="yonetici"
          element={
            <RequireRole rol="admin">
              <AdminUsersPage />
            </RequireRole>
          }
        />
        <Route path="*" element={<Bulunamadi />} />
      </Route>
    </Routes>
  );
}
