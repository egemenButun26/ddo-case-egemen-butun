import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MudurPage } from "./MudurPage";
import { govdeOku, jsonYaniti, sahteFetch, yol } from "../test/yardim";

const DOLULUK = [
  {
    lot_id: 1,
    code: "A",
    capacity: 100,
    occupied: 50,
    active_reservations: 3,
    available: 50,
  },
];
const KARA_LISTE = {
  id: 1,
  plate: "34XYZ",
  reason: "kaçak giriş",
  active: true,
  created_at: "2026-10-20T09:00:00Z",
};
const KAYMA = {
  id: 1,
  lot_id: 1,
  stored: 50,
  recomputed: 48,
  drifted: 2,
  corrected: false,
  created_at: "2026-10-20T09:10:00Z",
};

function mudurRotalari(
  ek?: (url: string, init?: RequestInit) => Response | null
) {
  return sahteFetch((url, init) => {
    const ozel = ek?.(url, init);
    if (ozel) {
      return ozel;
    }
    const path = yol(url);
    if (path === "/occupancy") {
      return jsonYaniti(DOLULUK);
    }
    if (path === "/blacklist" && init?.method === "POST") {
      return jsonYaniti({ ...KARA_LISTE, id: 2, plate: "34ABC" });
    }
    if (path === "/blacklist") {
      return jsonYaniti({ items: [KARA_LISTE], next_after_id: null });
    }
    if (path === "/admin/counter-drifts") {
      return jsonYaniti({ items: [KAYMA], next_after_id: null });
    }
    if (path === "/lots") {
      return jsonYaniti([{ id: 1, code: "A", name: "Merkez", capacity: 100 }]);
    }
    return jsonYaniti({});
  });
}

describe("MudurPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("yükleniyor durumundan doluluk panosuna geçer ve yüzdeyi gösterir", async () => {
    mudurRotalari();

    render(<MudurPage />);

    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
    const cubuk = await screen.findByRole("progressbar", { name: "A doluluk" });
    expect(cubuk).toHaveAttribute("aria-valuenow", "50");
    expect(screen.getByText("34XYZ")).toBeInTheDocument();
    expect(screen.getByText(/Depolanan 50/)).toBeInTheDocument();
  });

  it("boş listelerde kayıt yok gösterir", async () => {
    sahteFetch((url) => {
      const path = yol(url);
      if (path === "/occupancy" || path === "/lots") {
        return jsonYaniti([]);
      }
      return jsonYaniti({ items: [], next_after_id: null });
    });

    render(<MudurPage />);

    expect((await screen.findAllByText("Kayıt yok.")).length).toBeGreaterThanOrEqual(3);
  });

  it("500 hatasında Tekrar dene düğmesi gösterir", async () => {
    sahteFetch(() =>
      jsonYaniti({ error: { code: "server_error", message: "Sunucu hatası" } }, 500)
    );

    render(<MudurPage />);

    expect(await screen.findByText("Sunucu hatası")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tekrar dene" })).toBeInTheDocument();
  });

  it("boş plaka ile kara listeye ekleme yapmaz ve uyarı gösterir", async () => {
    const fetchSahte = mudurRotalari();

    render(<MudurPage />);

    await screen.findAllByText("34XYZ");
    fireEvent.click(screen.getByRole("button", { name: "Kara listeye ekle" }));

    expect(await screen.findByText("Plaka gerekli.")).toBeInTheDocument();
    expect(
      fetchSahte.mock.calls.some(
        ([url, init]) => yol(String(url)) === "/blacklist" && init?.method === "POST"
      )
    ).toBe(false);
  });

  it("kara listeye plaka ve gerekçe ile POST eder", async () => {
    const fetchSahte = mudurRotalari();

    render(<MudurPage />);

    fireEvent.change(await screen.findByLabelText("Plaka"), {
      target: { value: "34ABC" },
    });
    fireEvent.change(screen.getByLabelText("Gerekçe"), {
      target: { value: "kaçak" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Kara listeye ekle" }));

    await waitFor(() => {
      const cagri = fetchSahte.mock.calls.find(
        ([url, init]) => yol(String(url)) === "/blacklist" && init?.method === "POST"
      );
      expect(cagri).toBeTruthy();
      expect(govdeOku(cagri?.[1])).toEqual({ plate: "34ABC", reason: "kaçak" });
    });
  });

  it("kara liste kaydını DELETE ile kaldırır", async () => {
    const fetchSahte = mudurRotalari();

    render(<MudurPage />);

    fireEvent.click(await screen.findByRole("button", { name: "Kaldır" }));

    await waitFor(() => {
      expect(
        fetchSahte.mock.calls.some(
          ([url, init]) => yol(String(url)) === "/blacklist/1" && init?.method === "DELETE"
        )
      ).toBe(true);
    });
  });
});
