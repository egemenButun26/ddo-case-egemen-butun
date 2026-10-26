import { afterEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AdminUsersPage } from "./AdminUsersPage";

function sayfa(govde: unknown): Response {
  return { ok: true, status: 200, json: async () => govde } as Response;
}

describe("AdminUsersPage", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("ilk sayfayı yükler ve Daha fazla ikinci sayfayı after_id ile ister", async () => {
    const fetchSahte = vi
      .fn()
      .mockResolvedValueOnce(
        sayfa({
          users: [
            { id: 1, email: "ali@ornek.test", name: "Ali", role: "driver" },
            { id: 2, email: "ayse@ornek.test", name: "Ayşe", role: "attendant" },
          ],
          next_after_id: 2,
        })
      )
      .mockResolvedValueOnce(
        sayfa({
          users: [
            { id: 3, email: "mehmet@ornek.test", name: "Mehmet", role: "manager" },
          ],
          next_after_id: null,
        })
      );
    vi.stubGlobal("fetch", fetchSahte);

    render(<AdminUsersPage />);

    expect(await screen.findByText("Ali")).toBeInTheDocument();
    expect(screen.getByText("Ayşe")).toBeInTheDocument();
    expect(fetchSahte.mock.calls[0][0]).toBe("/api/admin/users?limit=20");

    fireEvent.click(screen.getByRole("button", { name: "Daha fazla" }));

    expect(await screen.findByText("Mehmet")).toBeInTheDocument();
    expect(fetchSahte.mock.calls[1][0]).toBe("/api/admin/users?limit=20&after_id=2");
    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Daha fazla" })).not.toBeInTheDocument()
    );
  });

  it("boş liste için bağlanmadı yerine kullanıcı yok durumunu gösterir", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => sayfa({ users: [], next_after_id: null }))
    );

    render(<AdminUsersPage />);

    expect(await screen.findByText("Henüz kullanıcı yok.")).toBeInTheDocument();
  });
});
