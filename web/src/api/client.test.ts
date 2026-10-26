import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  ApiError,
  TOKEN_ANAHTARI,
  ZAMAN_ASIMI_MS,
  apiFetch,
  yetkisizIsleyiciAyarla,
} from "./client";

function jsonYaniti(govde: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => govde,
  } as Response;
}

describe("apiFetch", () => {
  beforeEach(() => {
    yetkisizIsleyiciAyarla(null);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.useRealTimers();
    sessionStorage.clear();
  });

  it("token varsa Authorization Bearer başlığı ekler", async () => {
    sessionStorage.setItem(TOKEN_ANAHTARI, "abc123");
    const fetchSahte = vi.fn(
      async (_yol: string, _secenekler?: RequestInit) => jsonYaniti({ ok: true })
    );
    vi.stubGlobal("fetch", fetchSahte);

    await apiFetch("/deneme");

    const [, secenekler] = fetchSahte.mock.calls[0];
    const basliklar = new Headers(secenekler?.headers);
    expect(basliklar.get("Authorization")).toBe("Bearer abc123");
  });

  it("token yoksa Authorization başlığı göndermez", async () => {
    const fetchSahte = vi.fn(
      async (_yol: string, _secenekler?: RequestInit) => jsonYaniti({ ok: true })
    );
    vi.stubGlobal("fetch", fetchSahte);

    await apiFetch("/deneme");

    const [, secenekler] = fetchSahte.mock.calls[0];
    const basliklar = new Headers(secenekler?.headers);
    expect(basliklar.has("Authorization")).toBe(false);
  });

  it("hata gövdesini ApiError(status, code, message) olarak ayrıştırır", async () => {
    const fetchSahte = vi.fn(async () =>
      jsonYaniti(
        { error: { code: "invalid_input", message: "Alanlar geçersiz." } },
        422
      )
    );
    vi.stubGlobal("fetch", fetchSahte);

    const hata = await apiFetch("/deneme").catch((e: unknown) => e);

    expect(hata).toBeInstanceOf(ApiError);
    expect((hata as ApiError).status).toBe(422);
    expect((hata as ApiError).code).toBe("invalid_input");
    expect((hata as ApiError).message).toBe("Alanlar geçersiz.");
  });

  it("401 yanıtında oturumu temizler ve yetkisiz işleyiciyi çağırır", async () => {
    sessionStorage.setItem(TOKEN_ANAHTARI, "gecersiz");
    const isleyici = vi.fn();
    yetkisizIsleyiciAyarla(isleyici);
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        jsonYaniti(
          { error: { code: "invalid_credentials", message: "Oturum geçersiz." } },
          401
        )
      )
    );

    const hata = await apiFetch("/auth/me").catch((e: unknown) => e);

    expect(hata).toBeInstanceOf(ApiError);
    expect((hata as ApiError).status).toBe(401);
    expect(sessionStorage.getItem(TOKEN_ANAHTARI)).toBeNull();
    expect(isleyici).toHaveBeenCalledTimes(1);
  });

  it("10 saniyede yanıt gelmezse zaman aşımı hatası verir", async () => {
    vi.useFakeTimers();
    const fetchSahte = vi.fn(
      (_yol: string, secenekler?: RequestInit) =>
        new Promise<Response>((_cozum, reddet) => {
          secenekler?.signal?.addEventListener("abort", () => {
            reddet(new DOMException("Aborted", "AbortError"));
          });
        })
    );
    vi.stubGlobal("fetch", fetchSahte);

    const istek = apiFetch("/yavas");
    const beklenti = expect(istek).rejects.toMatchObject({
      status: 0,
      code: "timeout",
    });

    await vi.advanceTimersByTimeAsync(ZAMAN_ASIMI_MS);
    await beklenti;
  });

  it("204 yanıtında değer döndürmez", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({ ok: true, status: 204 }) as Response)
    );

    await expect(apiFetch("/auth/logout", { method: "POST" })).resolves.toBeUndefined();
  });
});
