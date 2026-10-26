import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { GorevliPage } from "./GorevliPage";
import { govdeOku, jsonYaniti, sahteFetch, yol } from "../test/yardim";

const DOLULUK = [
  {
    lot_id: 1,
    code: "A",
    capacity: 100,
    occupied: 40,
    active_reservations: 2,
    available: 60,
  },
];
const INCELEME = {
  id: 7,
  reason: "low_confidence",
  read_plate: "34ABC",
  event_id: 5,
  captured_at: "2026-10-26T18:30:00Z",
  suggested_session_id: 3,
  confidence: 0.42,
};
const OLAY = {
  id: 1,
  lot_id: 1,
  camera_id: 2,
  direction: "in",
  plate_raw: "34 xyz",
  plate_norm: "34XYZ",
  captured_at: "2026-10-26T18:31:00Z",
  outcome: "matched",
  session_id: 3,
};

function gorevliRotalari() {
  return sahteFetch((url) => {
    const path = yol(url);
    if (path === "/occupancy") {
      return jsonYaniti(DOLULUK);
    }
    if (path === "/reviews") {
      return jsonYaniti({ items: [INCELEME], next_after_id: null });
    }
    if (path === "/events/recent") {
      return jsonYaniti({ items: [OLAY], next_before_id: 1 });
    }
    return jsonYaniti({});
  });
}

describe("GorevliPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("yükleniyor durumundan veriye geçer ve canlı gösterge sunar", async () => {
    gorevliRotalari();

    render(<GorevliPage />);

    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
    expect(await screen.findByText("Sistem canlı · son güncelleme", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("low_confidence ·", { exact: false })).toBeInTheDocument();
    expect(screen.getByText("34XYZ")).toBeInTheDocument();
    expect(screen.getByText("40 / 100 dolu · 60 boş")).toBeInTheDocument();
  });

  it("boş listelerde kayıt yok gösterir", async () => {
    sahteFetch((url) => {
      const path = yol(url);
      if (path === "/occupancy") {
        return jsonYaniti([]);
      }
      if (path === "/reviews") {
        return jsonYaniti({ items: [], next_after_id: null });
      }
      return jsonYaniti({ items: [], next_before_id: null });
    });

    render(<GorevliPage />);

    expect((await screen.findAllByText("Kayıt yok.")).length).toBeGreaterThanOrEqual(3);
  });

  it("500 hatasında Tekrar dene düğmesi gösterir", async () => {
    sahteFetch(() =>
      jsonYaniti({ error: { code: "server_error", message: "Sunucu hatası" } }, 500)
    );

    render(<GorevliPage />);

    expect(await screen.findByText("Sunucu hatası")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tekrar dene" })).toBeInTheDocument();
  });

  it("incelemeyi yok sayarken dismiss POST gövdesini gönderir", async () => {
    const fetchSahte = gorevliRotalari();

    render(<GorevliPage />);

    fireEvent.click(await screen.findByRole("button", { name: "Yok say" }));

    await waitFor(() => {
      const cagri = fetchSahte.mock.calls.find(
        ([url, init]) =>
          yol(String(url)) === "/reviews/7/resolve" && init?.method === "POST"
      );
      expect(cagri).toBeTruthy();
      expect(govdeOku(cagri?.[1])).toEqual({ action: "dismiss" });
    });
    await waitFor(() =>
      expect(screen.queryByText("low_confidence ·", { exact: false })).not.toBeInTheDocument()
    );
  });
});
