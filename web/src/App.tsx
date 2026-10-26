import { Route, Routes } from "react-router-dom";
import { Layout } from "./components/Layout";
import { RequireRole } from "./components/RequireRole";
import { HomePage } from "./pages/HomePage";
import { LoginPage } from "./pages/LoginPage";
import { SurucuPage } from "./pages/SurucuPage";
import { GorevliPage } from "./pages/GorevliPage";
import { MuhasebePage } from "./pages/MuhasebePage";
import { MudurPage } from "./pages/MudurPage";
import { YoneticiPage } from "./pages/YoneticiPage";

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
              <SurucuPage />
            </RequireRole>
          }
        />
        <Route
          path="gorevli"
          element={
            <RequireRole rol="attendant">
              <GorevliPage />
            </RequireRole>
          }
        />
        <Route
          path="muhasebe"
          element={
            <RequireRole rol="accountant">
              <MuhasebePage />
            </RequireRole>
          }
        />
        <Route
          path="mudur"
          element={
            <RequireRole rol="manager">
              <MudurPage />
            </RequireRole>
          }
        />
        <Route
          path="yonetici"
          element={
            <RequireRole rol="admin">
              <YoneticiPage />
            </RequireRole>
          }
        />
        <Route path="*" element={<Bulunamadi />} />
      </Route>
    </Routes>
  );
}
