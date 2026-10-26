export const TOKEN_ANAHTARI = "parkakil_token";
export const API_TABANI = "/api";
export const ZAMAN_ASIMI_MS = 10000;

const GIRIS_YOLU = "/giris";

export class ApiError extends Error {
  readonly status: number;
  readonly code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.code = code;
  }
}

type YetkisizIsleyici = () => void;

let yetkisizIsleyici: YetkisizIsleyici | null = null;

export function yetkisizIsleyiciAyarla(isleyici: YetkisizIsleyici | null): void {
  yetkisizIsleyici = isleyici;
}

async function hataGovdesi(yanit: Response): Promise<{ code: string; message: string }> {
  try {
    const govde = (await yanit.json()) as {
      error?: { code?: unknown; message?: unknown };
    };
    const kod = typeof govde.error?.code === "string" ? govde.error.code : "unknown";
    const mesaj =
      typeof govde.error?.message === "string" && govde.error.message.trim()
        ? govde.error.message
        : "İstek başarısız oldu.";
    return { code: kod, message: mesaj };
  } catch {
    return { code: "unknown", message: "İstek başarısız oldu." };
  }
}

function oturumuTemizle(): void {
  sessionStorage.removeItem(TOKEN_ANAHTARI);
  if (yetkisizIsleyici) {
    yetkisizIsleyici();
    return;
  }
  if (window.location.pathname !== GIRIS_YOLU) {
    window.location.assign(GIRIS_YOLU);
  }
}

export type IstekSecenekleri = Omit<RequestInit, "body"> & {
  body?: unknown;
};

export async function apiFetch<T>(yol: string, secenekler: IstekSecenekleri = {}): Promise<T> {
  const { body, headers, ...diger } = secenekler;
  const denetleyici = new AbortController();
  const zamanAsimi = setTimeout(() => denetleyici.abort(), ZAMAN_ASIMI_MS);

  const basliklar = new Headers(headers);
  basliklar.set("Accept", "application/json");
  if (body !== undefined) {
    basliklar.set("Content-Type", "application/json");
  }

  const token = sessionStorage.getItem(TOKEN_ANAHTARI);
  if (token) {
    basliklar.set("Authorization", `Bearer ${token}`);
  }

  try {
    const yanit = await fetch(`${API_TABANI}${yol}`, {
      ...diger,
      headers: basliklar,
      signal: denetleyici.signal,
      body: body === undefined ? undefined : JSON.stringify(body),
    });

    if (yanit.status === 401) {
      const hata = await hataGovdesi(yanit);
      oturumuTemizle();
      throw new ApiError(401, hata.code, hata.message);
    }

    if (!yanit.ok) {
      const hata = await hataGovdesi(yanit);
      throw new ApiError(yanit.status, hata.code, hata.message);
    }

    if (yanit.status === 204) {
      return undefined as T;
    }

    return (await yanit.json()) as T;
  } catch (hata) {
    if (hata instanceof ApiError) {
      throw hata;
    }
    if (hata instanceof DOMException && hata.name === "AbortError") {
      throw new ApiError(0, "timeout", "Sunucu 10 saniye içinde yanıt vermedi.");
    }
    if (hata instanceof Error && hata.name === "AbortError") {
      throw new ApiError(0, "timeout", "Sunucu 10 saniye içinde yanıt vermedi.");
    }
    throw new ApiError(0, "network", "Sunucuya ulaşılamadı. Bağlantınızı kontrol edin.");
  } finally {
    clearTimeout(zamanAsimi);
  }
}
