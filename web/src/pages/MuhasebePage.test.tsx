import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MuhasebePage } from "./MuhasebePage";
import {
  govdeOku,
  jsonYaniti,
  parametre,
  sahteFetch,
  yol,
} from "../test/yardim";

const ODEME = {
  id: 1,
  session_id: 12,
  amount_kurus: 123456,
  status: "paid",
  provider_ref: "P1",
  requested_at: "2026-10-26T10:00:00Z",
  paid_at: "2026-10-26T10:01:00Z",
  late_approval: false,
};

function muhasebeRotalari(
  ek?: (url: string, init?: RequestInit) => Response | null
) {
  return sahteFetch((url, init) => {
    const ozel = ek?.(url, init);
    if (ozel) {
      return ozel;
    }
    const path = yol(url);
    if (path === "/payments") {
      return jsonYaniti({ items: [ODEME], next_after_id: null });
    }
    if (path === "/invoices") {
      return jsonYaniti({ items: [], next_after_id: null });
    }
    if (path === "/lots") {
      return jsonYaniti([]);
    }
    return jsonYaniti({});
  });
}

describe("MuhasebePage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("yükleniyor durumundan ödeme verisine geçer", async () => {
    muhasebeRotalari();

    render(<MuhasebePage />);

    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
    expect(await screen.findByText("1.234,56 TL · paid")).toBeInTheDocument();
    expect(screen.getByText(/P1/)).toBeInTheDocument();
    expect(screen.getByText("İade et")).toBeInTheDocument();
  });

  it("boş listelerde kayıt yok gösterir", async () => {
    sahteFetch((url) => {
      const path = yol(url);
      if (path === "/payments" || path === "/invoices") {
        return jsonYaniti({ items: [], next_after_id: null });
      }
      return jsonYaniti([]);
    });

    render(<MuhasebePage />);

    expect((await screen.findAllByText("Kayıt yok.")).length).toBeGreaterThanOrEqual(2);
  });

  it("500 hatasında Tekrar dene düğmesi gösterir", async () => {
    sahteFetch(() =>
      jsonYaniti({ error: { code: "server_error", message: "Sunucu hatası" } }, 500)
    );

    render(<MuhasebePage />);

    expect(await screen.findByText("Sunucu hatası")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tekrar dene" })).toBeInTheDocument();
  });

  it("ödemelerde keyset Daha fazla isteğini after_id ile yapar", async () => {
    const fetchSahte = sahteFetch((url) => {
      const path = yol(url);
      if (path === "/payments") {
        if (parametre(url, "after_id") === "7") {
          return jsonYaniti({
            items: [{ ...ODEME, id: 2, provider_ref: "P2", amount_kurus: 500 }],
            next_after_id: null,
          });
        }
        return jsonYaniti({ items: [ODEME], next_after_id: 7 });
      }
      return jsonYaniti({ items: [], next_after_id: null });
    });

    render(<MuhasebePage />);

    expect(await screen.findByText(/P1/)).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Daha fazla" }));

    expect(await screen.findByText(/P2/)).toBeInTheDocument();
    const ikinciCagri = fetchSahte.mock.calls.find(
      ([url]) => yol(String(url)) === "/payments" && parametre(String(url), "after_id") === "7"
    );
    expect(ikinciCagri).toBeTruthy();
  });

  it("iade isteğinde Idempotency-Key başlığını korur ve gövdeyi doğru gönderir", async () => {
    let iadeCagrisi = 0;
    const fetchSahte = muhasebeRotalari((url, init) => {
      if (yol(url) === "/refunds" && init?.method === "POST") {
        iadeCagrisi += 1;
        if (iadeCagrisi === 1) {
          return jsonYaniti(
            { error: { code: "provider_error", message: "İade başarısız oldu." } },
            500
          );
        }
        return jsonYaniti({ ok: true });
      }
      return null;
    });

    render(<MuhasebePage />);

    fireEvent.change(await screen.findByLabelText("Ödeme"), { target: { value: "1" } });
    fireEvent.change(screen.getByLabelText("İade tutarı (kuruş)"), {
      target: { value: "5000" },
    });
    fireEvent.click(screen.getByRole("button", { name: "İade et" }));

    expect(await screen.findByText("İade başarısız oldu.")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "İade et" }));

    await waitFor(() => expect(iadeCagrisi).toBe(2));

    const iadeler = fetchSahte.mock.calls.filter(
      ([url, init]) => yol(String(url)) === "/refunds" && init?.method === "POST"
    );
    const ilkBasliklar = new Headers(iadeler[0][1]?.headers);
    const ikinciBasliklar = new Headers(iadeler[1][1]?.headers);
    const ilkAnahtar = ilkBasliklar.get("Idempotency-Key");
    expect(ilkAnahtar).toBeTruthy();
    expect(ikinciBasliklar.get("Idempotency-Key")).toBe(ilkAnahtar);
    expect(govdeOku(iadeler[0][1])).toEqual({
      payment_id: 1,
      amount_kurus: 5000,
      reason: "manual",
    });
  });
});
