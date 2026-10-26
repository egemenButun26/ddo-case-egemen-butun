import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useNavigate } from "react-router-dom";
import {
  apiFetch,
  TOKEN_ANAHTARI,
  yetkisizIsleyiciAyarla,
} from "../api/client";

export type Rol = "driver" | "attendant" | "accountant" | "manager" | "admin";

export type Kullanici = {
  id: number;
  email: string;
  name: string;
  role: Rol;
};

type GirisYaniti = {
  token: string;
  expires_at: string;
  user: Kullanici;
};

type AuthBaglami = {
  kullanici: Kullanici | null;
  hazir: boolean;
  login: (email: string, sifre: string) => Promise<Kullanici>;
  logout: () => Promise<void>;
};

const AuthContext = createContext<AuthBaglami | null>(null);

export const ROL_ANA_SAYFASI: Record<Rol, string> = {
  driver: "/surucu",
  attendant: "/gorevli",
  accountant: "/muhasebe",
  manager: "/mudur",
  admin: "/yonetici",
};

export function AuthProvider({ children }: { children: ReactNode }) {
  const [kullanici, setKullanici] = useState<Kullanici | null>(null);
  const [hazir, setHazir] = useState(() => !sessionStorage.getItem(TOKEN_ANAHTARI));
  const navigate = useNavigate();

  useEffect(() => {
    yetkisizIsleyiciAyarla(() => {
      setKullanici(null);
      navigate("/giris", { replace: true });
    });
    return () => yetkisizIsleyiciAyarla(null);
  }, [navigate]);

  useEffect(() => {
    const token = sessionStorage.getItem(TOKEN_ANAHTARI);
    if (!token) {
      return;
    }
    let iptal = false;
    setHazir(false);
    apiFetch<Kullanici>("/auth/me")
      .then((kisi) => {
        if (!iptal) {
          setKullanici(kisi);
        }
      })
      .catch(() => {
        if (!iptal) {
          setKullanici(null);
        }
      })
      .finally(() => {
        if (!iptal) {
          setHazir(true);
        }
      });
    return () => {
      iptal = true;
    };
  }, []);

  const login = useCallback(async (email: string, sifre: string) => {
    const yanit = await apiFetch<GirisYaniti>("/auth/login", {
      method: "POST",
      body: { email, password: sifre },
    });
    sessionStorage.setItem(TOKEN_ANAHTARI, yanit.token);
    setKullanici(yanit.user);
    return yanit.user;
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiFetch<void>("/auth/logout", { method: "POST" });
    } catch {
      // Oturum yerelde her durumda kapatılır.
    } finally {
      sessionStorage.removeItem(TOKEN_ANAHTARI);
      setKullanici(null);
    }
  }, []);

  const deger = useMemo(
    () => ({ kullanici, hazir, login, logout }),
    [kullanici, hazir, login, logout]
  );

  return <AuthContext.Provider value={deger}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthBaglami {
  const baglam = useContext(AuthContext);
  if (!baglam) {
    throw new Error("useAuth yalnızca AuthProvider içinde kullanılabilir.");
  }
  return baglam;
}
