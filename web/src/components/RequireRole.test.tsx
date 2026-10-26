import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AuthProvider } from "../auth/AuthContext";
import { TOKEN_ANAHTARI } from "../api/client";
import { RequireRole } from "./RequireRole";

function kullaniciYaniti(role: "driver" | "admin") {
  return {
    ok: true,
    status: 200,
    json: async () => ({
      id: 1,
      email: `${role}@ornek.test`,
      name: role === "driver" ? "Sürücü Kullanıcı" : "Yönetici Kullanıcı",
      role,
    }),
  } as Response;
}

function ciz() {
  return render(
    <MemoryRouter initialEntries={["/"]}>
      <AuthProvider>
        <Routes>
          <Route
            path="/"
            element={
              <RequireRole rol="driver">
                <p>gizli içerik</p>
              </RequireRole>
            }
          />
          <Route path="/giris" element={<p>giris sayfasi</p>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

describe("RequireRole", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
    sessionStorage.clear();
  });

  it("giriş yoksa /giris sayfasına yönlendirir", async () => {
    ciz();
    expect(await screen.findByText("giris sayfasi")).toBeInTheDocument();
  });

  it("rol uymuyorsa erişim yetkisi mesajı gösterir", async () => {
    sessionStorage.setItem(TOKEN_ANAHTARI, "token");
    vi.stubGlobal("fetch", vi.fn(async () => kullaniciYaniti("admin")));

    ciz();

    expect(
      await screen.findByText("Bu sayfaya erişim yetkiniz yok")
    ).toBeInTheDocument();
    expect(screen.queryByText("gizli içerik")).not.toBeInTheDocument();
  });

  it("rol uyuyorsa içeriği gösterir", async () => {
    sessionStorage.setItem(TOKEN_ANAHTARI, "token");
    vi.stubGlobal("fetch", vi.fn(async () => kullaniciYaniti("driver")));

    ciz();

    expect(await screen.findByText("gizli içerik")).toBeInTheDocument();
  });
});
