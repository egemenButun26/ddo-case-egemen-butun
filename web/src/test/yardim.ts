import { vi } from "vitest";

export function jsonYaniti(govde: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => govde,
  } as Response;
}

export type Isleyici = (
  url: string,
  init?: RequestInit
) => Response | Promise<Response>;

export function sahteFetch(isleyici: Isleyici) {
  const sahte = vi.fn(async (url: string, init?: RequestInit) => isleyici(url, init));
  vi.stubGlobal("fetch", sahte);
  return sahte;
}

export function yol(url: string): string {
  const temiz = url.startsWith("/api") ? url.slice(4) : url;
  return temiz.split("?")[0];
}

export function parametre(url: string, ad: string): string | null {
  const sorgu = url.split("?")[1] ?? "";
  return new URLSearchParams(sorgu).get(ad);
}

export function govdeOku(init?: RequestInit): Record<string, unknown> {
  if (!init || typeof init.body !== "string") {
    return {};
  }
  return JSON.parse(init.body) as Record<string, unknown>;
}
