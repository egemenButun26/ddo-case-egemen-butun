import type { ReactNode } from "react";
import { Navigate } from "react-router-dom";
import { useAuth, type Rol } from "../auth/AuthContext";
import { AsyncState } from "./AsyncState";

type RequireRoleProps = {
  rol: Rol;
  children: ReactNode;
};

export function RequireRole({ rol, children }: RequireRoleProps) {
  const { kullanici, hazir } = useAuth();

  if (!hazir) {
    return <AsyncState durum="yukleniyor" />;
  }

  if (!kullanici) {
    return <Navigate to="/giris" replace />;
  }

  if (kullanici.role !== rol) {
    return (
      <section className="kart" aria-labelledby="yetkisiz-baslik">
        <h2 id="yetkisiz-baslik">Bu sayfaya erişim yetkiniz yok</h2>
        <p className="soluk">Bu ekranı görüntülemek için gerekli role sahip değilsiniz.</p>
      </section>
    );
  }

  return <>{children}</>;
}
