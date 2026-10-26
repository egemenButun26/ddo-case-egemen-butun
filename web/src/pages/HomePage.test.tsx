import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { HomePage } from "./HomePage";

describe("HomePage sistem durumu", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("sağlıklı yanıtta Sistem çalışıyor gösterir", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        status: 200,
        json: async () => ({ status: "ok", checks: { database: "ok" } }),
      }))
    );

    render(<HomePage />);

    expect(await screen.findByText("Sistem çalışıyor")).toBeInTheDocument();
    expect(screen.getByText("ok")).toBeInTheDocument();
  });

  it("hata yanıtında sorun mesajı gösterir", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: false,
        status: 503,
        json: async () => ({
          error: { code: "unavailable", message: "Veritabanına ulaşılamıyor." },
        }),
      }))
    );

    render(<HomePage />);

    expect(await screen.findByText("Sistemde sorun var")).toBeInTheDocument();
    expect(screen.getByText("Veritabanına ulaşılamıyor.")).toBeInTheDocument();
  });
});
