import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { AuthProvider } from "../auth/AuthContext";
import { TOKEN_ANAHTARI } from "../api/client";
import { LoginPage } from "./LoginPage";

function ciz() {
  return render(
    <MemoryRouter initialEntries={["/giris"]}>
      <AuthProvider>
        <Routes>
          <Route path="/giris" element={<LoginPage />} />
          <Route path="/mudur" element={<p>mudur sayfasi</p>} />
        </Routes>
      </AuthProvider>
    </MemoryRouter>
  );
}

function doldur() {
  fireEvent.change(screen.getByLabelText("E-posta"), {
    target: { value: "mudur@ornek.test" },
  });
  fireEvent.change(screen.getByLabelText("Şifre"), {
    target: { value: "cok-gizli" },
  });
}

describe("LoginPage", () => {
  beforeEach(() => {
    sessionStorage.clear();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("boş alanlarda doğrulama hatası gösterir ve istek atmaz", async () => {
    const fetchSahte = vi.fn();
    vi.stubGlobal("fetch", fetchSahte);
    ciz();

    fireEvent.click(screen.getByRole("button", { name: "Giriş yap" }));

    expect(await screen.findByText("E-posta adresi gerekli.")).toBeInTheDocument();
    expect(screen.getByText("Şifre gerekli.")).toBeInTheDocument();
    expect(fetchSahte).not.toHaveBeenCalled();
  });

  it("401 yanıtında kullanıcıya hatalı bilgi mesajı gösterir", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 401,
        json: async () => ({
          error: { code: "invalid_credentials", message: "Hatalı giriş." },
        }),
      }))
    );
    ciz();
    doldur();

    fireEvent.click(screen.getByRole("button", { name: "Giriş yap" }));

    expect(await screen.findByText("E-posta veya şifre hatalı.")).toBeInTheDocument();
    expect(sessionStorage.getItem(TOKEN_ANAHTARI)).toBeNull();
  });

  it("başarılı girişte tokenı saklar ve role göre ana sayfaya yönlendirir", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({
          token: "yeni-token",
          expires_at: "2026-10-27T00:00:00Z",
          user: {
            id: 7,
            email: "mudur@ornek.test",
            name: "Müdür Kullanıcı",
            role: "manager",
          },
        }),
      }))
    );
    ciz();
    doldur();

    fireEvent.click(screen.getByRole("button", { name: "Giriş yap" }));

    expect(await screen.findByText("mudur sayfasi")).toBeInTheDocument();
    expect(sessionStorage.getItem(TOKEN_ANAHTARI)).toBe("yeni-token");
  });
});
