import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { YoneticiPage } from "./YoneticiPage";
import { jsonYaniti, sahteFetch, yol } from "../test/yardim";

function yoneticiRotalari(
  ek?: (url: string, init?: RequestInit) => Response | null
) {
  return sahteFetch((url, init) => {
    const ozel = ek?.(url, init);
    if (ozel) {
      return ozel;
    }
    const path = yol(url);
    if (path === "/admin/users") {
      return jsonYaniti({
        users: [
          { id: 1, email: "ali@ornek.test", name: "Ali", role: "driver" },
        ],
        next_after_id: null,
      });
    }
    if (path === "/admin/occupancy/1/recount" && init?.method === "POST") {
      return jsonYaniti({ stored: 100, recomputed: 98, drifted: 2, corrected: 1 });
    }
    if (path === "/blacklist") {
      return jsonYaniti({ items: [], next_after_id: null });
    }
    if (path === "/admin/counter-drifts") {
      return jsonYaniti({ items: [], next_after_id: null });
    }
    if (path === "/lots") {
      return jsonYaniti([{ id: 1, code: "A", name: "Merkez", capacity: 100 }]);
    }
    return jsonYaniti({});
  });
}

describe("YoneticiPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("yükleniyor durumundan kullanıcı ve lot verisine geçer", async () => {
    yoneticiRotalari();

    render(<YoneticiPage />);

    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
    expect(await screen.findByText("Ali")).toBeInTheDocument();
    expect(screen.getByLabelText("Yeniden sayılacak lot")).toBeInTheDocument();
  });

  it("boş listelerde kayıt yok ve kullanıcı yok gösterir", async () => {
    sahteFetch((url) => {
      const path = yol(url);
      if (path === "/admin/users") {
        return jsonYaniti({ users: [], next_after_id: null });
      }
      if (path === "/lots") {
        return jsonYaniti([]);
      }
      return jsonYaniti({ items: [], next_after_id: null });
    });

    render(<YoneticiPage />);

    expect(await screen.findByText("Henüz kullanıcı yok.")).toBeInTheDocument();
    expect((await screen.findAllByText("Kayıt yok.")).length).toBeGreaterThanOrEqual(2);
  });

  it("500 hatasında Tekrar dene düğmesi gösterir", async () => {
    sahteFetch(() =>
      jsonYaniti({ error: { code: "server_error", message: "Sunucu hatası" } }, 500)
    );

    render(<YoneticiPage />);

    expect(await screen.findByText("Sunucu hatası")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Tekrar dene" })).toBeInTheDocument();
  });

  it("seçilen lot için sayaç yeniden sayımı POST eder", async () => {
    const fetchSahte = yoneticiRotalari();

    render(<YoneticiPage />);

    fireEvent.change(await screen.findByLabelText("Yeniden sayılacak lot"), {
      target: { value: "1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Sayaç yeniden sayımı" }));

    await waitFor(() => {
      expect(
        fetchSahte.mock.calls.some(
          ([url, init]) =>
            yol(String(url)) === "/admin/occupancy/1/recount" && init?.method === "POST"
        )
      ).toBe(true);
    });
    expect(await screen.findByText("Yeniden sayım tamamlandı.")).toBeInTheDocument();
    expect(screen.getByText("98")).toBeInTheDocument();
  });
});
