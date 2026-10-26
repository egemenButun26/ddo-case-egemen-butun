import { describe, expect, it } from "vitest";
import { bugun, kurus, metin, tarihSaat } from "./format";

describe("kurus", () => {
  it("sıfırı iki ondalıkla biçimler", () => {
    expect(kurus(0)).toBe("0,00 TL");
  });

  it("beş kuruşu 0,05 TL olarak gösterir", () => {
    expect(kurus(5)).toBe("0,05 TL");
  });

  it("binlik ayracı ve ondalık virgül kullanır", () => {
    expect(kurus(123456)).toBe("1.234,56 TL");
  });

  it("negatif tutarı eksi işaretiyle gösterir", () => {
    expect(kurus(-123456)).toBe("-1.234,56 TL");
  });

  it("eksik ya da geçersiz değerde tire gösterir", () => {
    expect(kurus(undefined)).toBe("—");
    expect(kurus(Number.NaN)).toBe("—");
  });
});

describe("metin ve tarih", () => {
  it("eksik alanları tire ile gösterir", () => {
    expect(metin(undefined)).toBe("—");
    expect(metin(null)).toBe("—");
    expect(metin(0)).toBe("0");
  });

  it("Istanbul saat diliminde tarih üretir", () => {
    expect(tarihSaat("2026-10-26T21:00:00Z")).toContain("27.10.2026");
    expect(tarihSaat("gecersiz")).toBe("—");
  });

  it("bugünü YYYY-AA-GG biçiminde verir", () => {
    expect(bugun()).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});
