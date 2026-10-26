import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SurucuPage } from "./SurucuPage";
import { govdeOku, jsonYaniti, sahteFetch, yol } from "../test/yardim";

const LOTLAR = [{ id: 1, code: "A", name: "Merkez", capacity: 100 }];
const PLANLAR = [{ code: "aylik", lot_id: 1, monthly_price_kurus: 50000 }];
const BORC = {
  plate: "34ABC123",
  total_kurus: 123456,
  items: [
    {
      id: 1,
      session_id: 9,
      lot_id: 1,
      amount_kurus: 123456,
      paid_kurus: 0,
      remaining_kurus: 123456,
      status: "open",
      created_at: "2026-10-01T10:00:00Z",
    },
  ],
};
const ABONELIK = {
  id: 4,
  plan_code: "aylik",
  plate: "34ABC123",
  status: "active",
  current_period_start: "2026-10-01T00:00:00Z",
  current_period_end: "2026-11-01T00:00:00Z",
};
const FATURA = {
  id: 8,
  subscription_id: 4,
  kind: "period",
  period_start: "2026-10-01T00:00:00Z",
  amount_kurus: 50000,
  status: "paid",
  attempts: 1,
};

function surucuRotalari(
  ek?: (url: string, init?: RequestInit) => Response | null
) {
  return sahteFetch((url, init) => {
    const ozel = ek?.(url, init);
    if (ozel) {
      return ozel;
    }
    const path = yol(url);
    if (path === "/lots") {
      return jsonYaniti(LOTLAR);
    }
    if (path === "/debts") {
      return jsonYaniti(BORC);
    }
    if (path === "/plans") {
      return jsonYaniti(PLANLAR);
    }
    if (path === "/subscriptions" && init?.method === "POST") {
      return jsonYaniti({ ...ABONELIK, id: 11 });
    }
    if (path === "/subscriptions") {
      return jsonYaniti({ items: [ABONELIK], next_after_id: null });
    }
    if (path === "/invoices") {
      return jsonYaniti({ items: [FATURA], next_after_id: null });
    }
    return jsonYaniti({});
  });
}

describe("SurucuPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("yükleniyor durumundan veriye geçer", async () => {
    surucuRotalari();

    render(<SurucuPage plaka="34ABC123" />);

    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
    expect((await screen.findAllByText("1.234,56 TL")).length).toBeGreaterThan(0);
    expect(screen.getAllByText("34ABC123").length).toBeGreaterThan(0);
    expect(screen.getByText(/Faturalar/)).toBeInTheDocument();
    expect(screen.getByText("period · 500,00 TL")).toBeInTheDocument();
  });

  it("boş listelerde kayıt yok gösterir", async () => {
    sahteFetch((url) => {
      const path = yol(url);
      if (path === "/lots") {
        return jsonYaniti([]);
      }
      if (path === "/debts") {
        return jsonYaniti({ plate: "34ABC123", total_kurus: 0, items: [] });
      }
      if (path === "/plans") {
        return jsonYaniti([]);
      }
      if (path === "/subscriptions") {
        return jsonYaniti({ items: [], next_after_id: null });
      }
      return jsonYaniti({ items: [], next_after_id: null });
    });

    render(<SurucuPage plaka="34ABC123" />);

    expect((await screen.findAllByText("Kayıt yok.")).length).toBeGreaterThanOrEqual(3);
  });

  it("500 hatasında mesaj ve Tekrar dene gösterir", async () => {
    sahteFetch(() =>
      jsonYaniti(
        { error: { code: "server_error", message: "Sunucu hatası" } },
        500
      )
    );

    render(<SurucuPage plaka="34ABC123" />);

    expect(await screen.findByText("Sunucu hatası")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tekrar dene" })).toBeInTheDocument();
  });

  it("abonelik oluştururken plaka ve plan kodunu POST eder", async () => {
    const fetchSahte = surucuRotalari();

    render(<SurucuPage plaka="34ABC123" />);

    const planSecimi = await screen.findByLabelText("Yeni abonelik planı");
    fireEvent.change(planSecimi, { target: { value: "aylik" } });
    fireEvent.click(screen.getByRole("button", { name: "Abonelik oluştur" }));

    await waitFor(() => {
      const cagri = fetchSahte.mock.calls.find(
        ([url, init]) => yol(String(url)) === "/subscriptions" && init?.method === "POST"
      );
      expect(cagri).toBeTruthy();
      expect(govdeOku(cagri?.[1])).toEqual({
        plate: "34ABC123",
        plan_code: "aylik",
      });
    });
  });

  it("abonelik iptalini POST /subscriptions/{id}/cancel ile gönderir", async () => {
    const fetchSahte = surucuRotalari();

    render(<SurucuPage plaka="34ABC123" />);

    fireEvent.click(await screen.findByRole("button", { name: "İptal et" }));

    await waitFor(() => {
      expect(
        fetchSahte.mock.calls.some(
          ([url, init]) =>
            yol(String(url)) === "/subscriptions/4/cancel" && init?.method === "POST"
        )
      ).toBe(true);
    });
  });
});
