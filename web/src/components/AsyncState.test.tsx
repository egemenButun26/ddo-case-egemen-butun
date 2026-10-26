import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AsyncState } from "./AsyncState";

describe("AsyncState", () => {
  it("yükleniyor durumunu gösterir", () => {
    render(<AsyncState durum="yukleniyor" />);
    expect(screen.getByText("Yükleniyor…")).toBeInTheDocument();
  });

  it("boş durumu için varsayılan metni gösterir", () => {
    render(<AsyncState durum="bos" />);
    expect(screen.getByText("Bu ekran henüz bağlanmadı.")).toBeInTheDocument();
  });

  it("hata durumunda mesajı ve Tekrar dene düğmesini gösterir", () => {
    const tekrar = vi.fn();
    render(<AsyncState durum="hata" mesaj="Sunucu hatası" onTekrar={tekrar} />);

    expect(screen.getByRole("alert")).toHaveTextContent("Sunucu hatası");
    fireEvent.click(screen.getByRole("button", { name: "Tekrar dene" }));
    expect(tekrar).toHaveBeenCalledTimes(1);
  });
});
