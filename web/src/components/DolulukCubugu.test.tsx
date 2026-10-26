import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { DolulukCubugu, dolulukYuzdesi } from "./DolulukCubugu";

describe("DolulukCubugu", () => {
  it("dolu / kapasite oranını yüzde olarak hesaplar", () => {
    render(<DolulukCubugu dolu={25} kapasite={100} etiket="Merkez" />);

    const cubuk = screen.getByRole("progressbar", { name: "Merkez doluluk" });
    expect(cubuk).toHaveAttribute("aria-valuenow", "25");
    expect(screen.getByText(/%25/)).toBeInTheDocument();
  });

  it("kapasite sıfırken yüzdeyi sıfırda tutar ve aşırı değeri sınırlar", () => {
    expect(dolulukYuzdesi(10, 0)).toBe(0);
    expect(dolulukYuzdesi(150, 100)).toBe(100);
    expect(dolulukYuzdesi(undefined, 100)).toBe(0);
  });
});
