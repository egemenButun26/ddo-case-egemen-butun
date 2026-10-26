const BOS = "—";

function binlikAyir(sayi: string): string {
  return sayi.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
}

export function kurus(deger: number | null | undefined): string {
  if (typeof deger !== "number" || !Number.isFinite(deger)) {
    return BOS;
  }
  const yuvarlanmis = Math.round(deger);
  const negatif = yuvarlanmis < 0;
  const mutlak = Math.abs(yuvarlanmis);
  const tam = binlikAyir(String(Math.floor(mutlak / 100)));
  const kesir = String(mutlak % 100).padStart(2, "0");
  return `${negatif ? "-" : ""}${tam},${kesir} TL`;
}

export function metin(deger: unknown): string {
  if (deger === undefined || deger === null || deger === "") {
    return BOS;
  }
  return String(deger);
}

const TARIH_SAAT = new Intl.DateTimeFormat("tr-TR", {
  dateStyle: "short",
  timeStyle: "short",
  timeZone: "Europe/Istanbul",
});

const TARIH = new Intl.DateTimeFormat("tr-TR", {
  dateStyle: "short",
  timeZone: "Europe/Istanbul",
});

function gecerliTarih(deger: string | null | undefined): Date | null {
  if (!deger) {
    return null;
  }
  const tarih = new Date(deger);
  return Number.isNaN(tarih.getTime()) ? null : tarih;
}

export function tarihSaat(deger: string | null | undefined): string {
  const tarih = gecerliTarih(deger);
  return tarih ? TARIH_SAAT.format(tarih) : BOS;
}

export function tarih(deger: string | null | undefined): string {
  const sonuc = gecerliTarih(deger);
  return sonuc ? TARIH.format(sonuc) : BOS;
}

export function saat(deger: Date = new Date()): string {
  return new Intl.DateTimeFormat("tr-TR", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZone: "Europe/Istanbul",
  }).format(deger);
}

export function bugun(): string {
  return new Intl.DateTimeFormat("en-CA", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    timeZone: "Europe/Istanbul",
  }).format(new Date());
}
