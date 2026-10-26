#!/usr/bin/env node
// scripts/verify/verify.js — calisan ve seed edilmis sistemde F/T kimliklerini
// API uzerinden ucdan uca dener. Her satir PASS/FAIL basar; hata varsa cikis
// kodu sifirdan farklidir.
//
//   node scripts/verify/verify.js
//   API_URL=http://localhost:8080 WEB_URL=http://localhost:3000 node scripts/verify/verify.js
"use strict";

const crypto = require("crypto");
const fs = require("fs");
const path = require("path");

const KOK = path.join(__dirname, "..", "..");
const API = (process.env.API_URL || "http://localhost:8080").replace(/\/+$/, "");
const WEB = (process.env.WEB_URL || "http://localhost:3000").replace(/\/+$/, "");

function envOku(dosya) {
  const out = {};
  if (!fs.existsSync(dosya)) return out;
  for (const satir of fs.readFileSync(dosya, "utf8").split(/\r?\n/)) {
    const temiz = satir.trim();
    if (!temiz || temiz.startsWith("#")) continue;
    const i = temiz.indexOf("=");
    if (i <= 0) continue;
    const anahtar = temiz.slice(0, i).trim();
    let deger = temiz.slice(i + 1).trim();
    if ((deger.startsWith('"') && deger.endsWith('"')) || (deger.startsWith("'") && deger.endsWith("'"))) {
      deger = deger.slice(1, -1);
    }
    out[anahtar] = deger;
  }
  return out;
}

const env = envOku(path.join(KOK, ".env"));
const CAMERA_KEY = process.env.CAMERA_API_KEY || env.CAMERA_API_KEY || "";
const PAROLA = process.env.SEED_DEMO_PASSWORD || env.SEED_DEMO_PASSWORD || "";
const WEBHOOK_SECRET = process.env.PROVIDER_WEBHOOK_SECRET || env.PROVIDER_WEBHOOK_SECRET || "";

if (!CAMERA_KEY || !PAROLA) {
  console.error("HATA: .env icinde CAMERA_API_KEY ve SEED_DEMO_PASSWORD gerekli.");
  process.exit(2);
}

const runTag = (Date.now().toString(36) + Math.random().toString(36).slice(2, 6)).toUpperCase();
let eventSeq = 0;
let plateSeq = 0;

function bekle(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function istek(method, yol, secenek = {}) {
  const { token, cameraKey, body, raw, headers = {} } = secenek;
  const h = { ...headers };
  if (raw !== undefined || body !== undefined) h["Content-Type"] = "application/json";
  if (token) h.Authorization = "Bearer " + token;
  if (cameraKey) h["X-Camera-Key"] = cameraKey;
  const res = await fetch(API + yol, {
    method,
    headers: h,
    body: raw !== undefined ? raw : body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(30000),
  });
  const metin = await res.text();
  let veri = null;
  try {
    veri = metin ? JSON.parse(metin) : null;
  } catch {
    veri = metin;
  }
  return { durum: res.status, basliklar: res.headers, veri, metin };
}

function bekleStatus(yanit, ...kodlar) {
  if (!kodlar.includes(yanit.durum)) {
    throw new Error(`HTTP ${yanit.durum} (beklenen ${kodlar.join("/")}): ${yanit.metin.slice(0, 180)}`);
  }
}

function olmali(kosul, mesaj) {
  if (!kosul) throw new Error(mesaj);
}

function imzala(govde) {
  return "sha256=" + crypto.createHmac("sha256", WEBHOOK_SECRET).update(govde).digest("hex");
}

async function imzaliBildirim(providerRef, amountKurus) {
  if (!WEBHOOK_SECRET) throw new Error("PROVIDER_WEBHOOK_SECRET yok");
  const govde = JSON.stringify({
    provider_ref: providerRef,
    outcome: "succeeded",
    amount_kurus: amountKurus,
    provider_at: new Date().toISOString(),
  });
  const yanit = await istek("POST", "/provider/notifications", {
    raw: govde,
    headers: { "X-Provider-Signature": imzala(govde) },
  });
  bekleStatus(yanit, 200);
  return yanit.veri;
}
async function giris(email) {
  const yanit = await istek("POST", "/auth/login", { body: { email, password: PAROLA } });
  bekleStatus(yanit, 200);
  olmali(yanit.veri && yanit.veri.token, "login token yok");
  return yanit.veri.token;
}

function yeniPlaka(harfler = "QX") {
  plateSeq += 1;
  const rakam = String(1000 + ((Date.now() + plateSeq * 137) % 9000));
  return `34${harfler}${rakam}`;
}

async function olay(lotId, yon, plaka, capturedAt, onek = "verify") {
  eventSeq += 1;
  const cameraEventId = `verify-${runTag}-${eventSeq}`;
  const yanit = await istek("POST", "/camera-events", {
    cameraKey: CAMERA_KEY,
    body: {
      lot_id: lotId,
      camera_id: `${onek}-${runTag}`,
      camera_event_id: cameraEventId,
      direction: yon,
      plate_raw: plaka,
      captured_at: capturedAt,
    },
  });
  return { yanit, cameraEventId };
}

const sonuclar = [];
let ctx = {};

async function kontrol(id, ad, is) {
  const bas = Date.now();
  try {
    const ayrinti = await is();
    const sure = Date.now() - bas;
    sonuclar.push({ id, ad, ok: true, ayrinti: ayrinti || "-", sure });
    console.log(`PASS  ${id.padEnd(6)} ${ad}${ayrinti ? " — " + ayrinti : ""}`);
  } catch (hata) {
    const sure = Date.now() - bas;
    sonuclar.push({ id, ad, ok: false, ayrinti: hata.message, sure });
    console.log(`FAIL  ${id.padEnd(6)} ${ad} — ${hata.message}`);
  }
}

async function main() {
  console.log(`ParkAkıl hızlı doğrulama — API ${API}, Web ${WEB}\n`);

  await kontrol("HEALTH", "GET /health, /_hz/45a6 ve X-DDO-Node", async () => {
    const saglik = await istek("GET", "/health");
    bekleStatus(saglik, 200);
    olmali(saglik.veri && saglik.veri.status === "ok", "health status ok degil");
    olmali(saglik.veri.checks && saglik.veri.checks.database === "ok", "veritabani kontrolu ok degil");
    olmali(saglik.basliklar.get("x-ddo-node") === "45a6ab", "X-DDO-Node basligi yok");
    const hz = await istek("GET", "/_hz/45a6");
    bekleStatus(hz, 200);
    olmali(hz.basliklar.get("x-ddo-node") === "45a6ab", "hz X-DDO-Node basligi yok");
    return "ikisi de 200";
  });

  await kontrol("AUTH", "Parola, 401/403 ve rol kontrolu", async () => {
    ctx.yonetici = await giris("seed.ardic@ddo-case.invalid");
    ctx.sofor = await giris("surucu@parkakil.test");
    ctx.gorevli = await giris("gorevli@parkakil.test");
    ctx.muhasebe = await giris("muhasebe@parkakil.test");
    ctx.mudur = await giris("mudur@parkakil.test");

    const yanlis = await istek("POST", "/auth/login", {
      body: { email: "surucu@parkakil.test", password: "kesinlikle-yanlis" },
    });
    bekleStatus(yanlis, 401);

    const yetkisiz = await istek("GET", "/admin/users");
    bekleStatus(yetkisiz, 401);

    const yasak = await istek("GET", "/admin/users", { token: ctx.sofor });
    bekleStatus(yasak, 403);

    return "5 rol girisi, yanlis parola 401, yetkisiz 401, sofor 403";
  });

  await kontrol("F-1", "Plaka olaylari / kamera simulatoru canli", async () => {
    const lotlar = await istek("GET", "/lots", { token: ctx.mudur });
    bekleStatus(lotlar, 200);
    olmali(Array.isArray(lotlar.veri) && lotlar.veri.length >= 5, "5 otopark yok");

    const doluluk = await istek("GET", "/occupancy", { token: ctx.gorevli });
    bekleStatus(doluluk, 200);
    const uygun = [...doluluk.veri].sort((a, b) => b.available - a.available)[0];
    olmali(uygun && uygun.available > 0, "bos kapasiteli otopark yok");
    ctx.lotId = uygun.lot_id;

    const akis = await istek("GET", "/events/recent?limit=20", { token: ctx.gorevli });
    bekleStatus(akis, 200);
    olmali(Array.isArray(akis.veri.items) && akis.veri.items.length > 0, "canli olay akisi bos");

    const plaka = yeniPlaka("QA");
    const simdi = new Date().toISOString();
    const girisOlayi = await olay(ctx.lotId, "entry", plaka, simdi);
    bekleStatus(girisOlayi.yanit, 201);
    olmali(girisOlayi.yanit.veri.outcome === "opened", "giris outcome opened degil");
    ctx.f2 = { plaka, giris: girisOlayi.yanit.veri };
    return `${lotlar.veri.length} otopark, ${akis.veri.items.length} canli olay`;
  });

  await kontrol("F-2", "Giris / cikis oturumu", async () => {
    olmali(ctx.f2, "F-2 icin giris verisi yok");
    const cikis = await olay(ctx.lotId, "exit", ctx.f2.plaka, new Date().toISOString());
    bekleStatus(cikis.yanit, 201);
    olmali(cikis.yanit.veri.outcome === "closed", `cikis outcome ${cikis.yanit.veri.outcome}`);
    olmali(cikis.yanit.veri.session_id === ctx.f2.giris.session_id, "cikis ayni oturuma baglanmadi");
    olmali(cikis.yanit.veri.amount_kurus !== null, "cikis tutari yok");
    return `oturum ${ctx.f2.giris.session_id} kapandi, tutar ${cikis.yanit.veri.amount_kurus} kurus`;
  });

  await kontrol("F-3", "Tarife ve ucret hesabi", async () => {
    const ucretsizPlaka = yeniPlaka("QZ");
    const ucretsizGiris = await olay(ctx.lotId, "entry", ucretsizPlaka, "2026-10-27T10:00:00+03:00");
    bekleStatus(ucretsizGiris.yanit, 201);
    const ucretsizCikis = await olay(ctx.lotId, "exit", ucretsizPlaka, "2026-10-27T10:10:00+03:00");
    bekleStatus(ucretsizCikis.yanit, 201);
    olmali(ucretsizCikis.yanit.veri.amount_kurus === 0, `10 dk ucretsiz degil: ${ucretsizCikis.yanit.veri.amount_kurus}`);

    ctx.t2Plaka = yeniPlaka("QT");
    const giris = await olay(ctx.lotId, "entry", ctx.t2Plaka, "2026-10-27T10:00:00+03:00");
    bekleStatus(giris.yanit, 201);
    const cikis = await olay(ctx.lotId, "exit", ctx.t2Plaka, "2026-10-27T12:00:00+03:00");
    bekleStatus(cikis.yanit, 201);
    ctx.t2Tutar = cikis.yanit.veri.amount_kurus;
    olmali(ctx.t2Tutar === 7000, `2 saatlik tutar ${ctx.t2Tutar}, beklenen 7000`);
    return `10 dk = 0, 2 saat = ${ctx.t2Tutar} kurus`;
  });

  await kontrol("F-4", "Abonelik ve rezervasyon", async () => {
    const rezervasyonPlaka = yeniPlaka("QR");
    const rezervasyonGovde = { lot_id: ctx.lotId, plate: rezervasyonPlaka };
    const anahtar = `verify-rez-${runTag}`;
    const ilk = await istek("POST", "/reservations", {
      token: ctx.mudur,
      headers: { "Idempotency-Key": anahtar },
      body: rezervasyonGovde,
    });
    bekleStatus(ilk, 201);
    olmali(ilk.veri.replayed === false, "ilk rezervasyon replayed=true");
    const tekrar = await istek("POST", "/reservations", {
      token: ctx.mudur,
      headers: { "Idempotency-Key": anahtar },
      body: rezervasyonGovde,
    });
    bekleStatus(tekrar, 200);
    olmali(tekrar.veri.replayed === true && tekrar.veri.id === ilk.veri.id, "rezervasyon idempotency calismadi");

    const farkli = await istek("POST", "/reservations", {
      token: ctx.mudur,
      headers: { "Idempotency-Key": anahtar },
      body: { lot_id: ctx.lotId, plate: yeniPlaka("QS") },
    });
    bekleStatus(farkli, 409);
    olmali(farkli.veri.error && farkli.veri.error.code === "idempotency_conflict", "catisma kodu yanlis");

    const sil = await istek("DELETE", `/reservations/${ilk.veri.id}`, { token: ctx.mudur });
    bekleStatus(sil, 204);

    const planlar = await istek("GET", "/plans", { token: ctx.mudur });
    bekleStatus(planlar, 200);
    const plan = planlar.veri.items.find((p) => p.active);
    olmali(plan, "aktif plan yok");
    const abonelikPlaka = yeniPlaka("QN");
    const abonelik = await istek("POST", "/subscriptions", {
      token: ctx.mudur,
      body: { plate: abonelikPlaka, plan_code: plan.code },
    });
    bekleStatus(abonelik, 201);
    const iptal = await istek("POST", `/subscriptions/${abonelik.veri.id}/cancel`, { token: ctx.mudur });
    bekleStatus(iptal, 200);
    olmali(iptal.veri.invoice_id > 0, "iptal proration faturasi yok");
    ctx.t5 = { subscriptionId: abonelik.veri.id, plate: abonelikPlaka };
    return `rezervasyon idempotent, abonelik ${abonelik.veri.id} iptal edildi`;
  });

  await kontrol("F-5", "Kiosk odeme ve tahsilat", async () => {
    const harf = (runTag.replace(/[^A-Z]/g, "") + "XY").slice(0, 2);
    const plaka = yeniPlaka(harf);
    const giris = await olay(ctx.lotId, "entry", plaka, new Date(Date.now() - 45 * 60 * 1000).toISOString());
    bekleStatus(giris.yanit, 201);
    olmali(giris.yanit.veri.session_id, "odeme oturumu yok");
    ctx.t4 = {
      plaka,
      sessionId: giris.yanit.veri.session_id,
      paymentId: null,
      providerRef: null,
      amountKurus: 0,
    };
    const teklif = await istek("GET", `/kiosk/quote?lot_id=${ctx.lotId}&plate=${encodeURIComponent(plaka)}`, {
      cameraKey: CAMERA_KEY,
    });
    bekleStatus(teklif, 200);
    olmali(teklif.veri.due_kurus > 0, "odeme tutari sifir");
    const govde = { session_id: teklif.veri.session_id, amount_kurus: teklif.veri.due_kurus };
    const anahtar = `verify-pay-${runTag}-${ctx.t4.sessionId}`;
    const odeme = await istek("POST", "/kiosk/payments", {
      cameraKey: CAMERA_KEY,
      headers: { "Idempotency-Key": anahtar },
      body: govde,
    });
    bekleStatus(odeme, 201);
    const tekrar = await istek("POST", "/kiosk/payments", {
      cameraKey: CAMERA_KEY,
      headers: { "Idempotency-Key": anahtar },
      body: govde,
    });
    bekleStatus(tekrar, 200);
    olmali(tekrar.veri.replayed === true && tekrar.veri.payment_id === odeme.veri.payment_id, "odeme idempotency calismadi");
    ctx.t4.paymentId = odeme.veri.payment_id;
    ctx.t4.providerRef = odeme.veri.provider_ref;
    ctx.t4.amountKurus = teklif.veri.due_kurus;
    return `odeme ${odeme.veri.payment_id}, ${teklif.veri.due_kurus} kurus`;
  });

  await kontrol("F-6", "Kara liste / borclu arac", async () => {
    const borc = await istek("GET", `/debts?plate=${encodeURIComponent(ctx.t2Plaka)}`, { token: ctx.muhasebe });
    bekleStatus(borc, 200);
    olmali(borc.veri.total_kurus >= 7000, `borc ${borc.veri.total_kurus}, beklenen >= 7000`);

    const karaPlaka = yeniPlaka("QK");
    const ekle = await istek("POST", "/blacklist", {
      token: ctx.mudur,
      body: { plate: karaPlaka, reason: "verify" },
    });
    bekleStatus(ekle, 201);
    const liste = await istek("GET", "/blacklist?limit=50", { token: ctx.gorevli });
    bekleStatus(liste, 200);
    olmali(liste.veri.items.some((x) => x.id === ekle.veri.id), "kara liste kaydi listede yok");
    const sil = await istek("DELETE", `/blacklist/${ekle.veri.id}`, { token: ctx.mudur });
    bekleStatus(sil, 204);
    return `borc ${borc.veri.total_kurus}, kara liste ekle/sil`;
  });

  await kontrol("F-7", "Doluluk panosu ve raporlar", async () => {
    const doluluk = await istek("GET", "/occupancy", { token: ctx.gorevli });
    bekleStatus(doluluk, 200);
    olmali(Array.isArray(doluluk.veri) && doluluk.veri.length >= 5, "doluluk listesi eksik");

    const rapor = await istek("GET", `/reports/daily?day=2026-10-27&lot_id=${ctx.lotId}`, {
      token: ctx.muhasebe,
    });
    bekleStatus(rapor, 200);
    olmali(rapor.veri.entries >= 2, `gunluk rapor girisleri ${rapor.veri.entries}`);

    try {
      const web = await fetch(WEB + "/", { signal: AbortSignal.timeout(10000) });
      olmali(web.status === 200, `web durum ${web.status}`);
      const html = await web.text();
      olmali(html.includes("ParkAk"), "web sayfasi beklenen icerigi dondurmedi");
    } catch (hata) {
      if (hata.name === "TimeoutError") throw new Error("web zaman asimi");
      throw new Error("web erisilemedi: " + hata.message);
    }
    return `${doluluk.veri.length} otopark, rapor ${rapor.veri.entries} giris, web 200`;
  });

  await kontrol("T-1", "Hatali plaka okumalariyla oturum eslestirme", async () => {
    const plaka = yeniPlaka("UV");
    const giris = await olay(ctx.lotId, "entry", plaka, new Date(Date.now() - 60 * 60 * 1000).toISOString());
    bekleStatus(giris.yanit, 201);
    // U/V OCR karisikligi: iki plaka da gecerli TR biciminde.
    const okunan = plaka.replace("U", "V");
    const cikis = await olay(ctx.lotId, "exit", okunan, new Date().toISOString());
    bekleStatus(cikis.yanit, 201);
    olmali(cikis.yanit.veri.outcome === "closed", `bulanik eslesme outcome ${cikis.yanit.veri.outcome}`);
    olmali(cikis.yanit.veri.session_id === giris.yanit.veri.session_id, "bulanik eslesme yanlis oturuma baglandi");

    const bozuk = await olay(ctx.lotId, "exit", "3OABC12", new Date().toISOString(), "verify-review");
    bekleStatus(bozuk.yanit, 201);
    olmali(bozuk.yanit.veri.outcome === "review", `gecersiz okuma outcome ${bozuk.yanit.veri.outcome}`);
    olmali(bozuk.yanit.veri.review_id > 0, "inceleme kaydi olusmadi");
    const coz = await istek("POST", `/reviews/${bozuk.yanit.veri.review_id}/resolve`, {
      token: ctx.gorevli,
      body: { action: "dismiss" },
    });
    bekleStatus(coz, 200);
    return `U/V eslesti, gecersiz okuma incelemeye dustu ve cozuldu`;
  });

  await kontrol("T-2", "Tarife motoru", async () => {
    olmali(ctx.t2Tutar === 7000, `2 saatlik ucret ${ctx.t2Tutar}`);
    return `dilimli tarife + ilk 15 dk: 2 saat = ${ctx.t2Tutar} kurus`;
  });

  await kontrol("T-3", "Kapasite, rezervasyon ve sayac tutarliligi", async () => {
    const once = await istek("GET", "/occupancy", { token: ctx.gorevli });
    bekleStatus(once, 200);
    const onceLot = once.veri.find((x) => x.lot_id === ctx.lotId);
    olmali(onceLot, "otopark dolulukta yok");

    const plaka = yeniPlaka("QC");
    const rez = await istek("POST", "/reservations", {
      token: ctx.mudur,
      body: { lot_id: ctx.lotId, plate: plaka },
    });
    bekleStatus(rez, 201);
    const sonra = await istek("GET", "/occupancy", { token: ctx.gorevli });
    bekleStatus(sonra, 200);
    const sonraLot = sonra.veri.find((x) => x.lot_id === ctx.lotId);
    olmali(sonraLot.active_reservations >= onceLot.active_reservations + 1, "aktif rezervasyon sayaci artmadi");
    await istek("DELETE", `/reservations/${rez.veri.id}`, { token: ctx.mudur });

    const recount = await istek("POST", `/admin/occupancy/${ctx.lotId}/recount`, { token: ctx.yonetici });
    bekleStatus(recount, 200);
    olmali(typeof recount.veri.recomputed === "number", "recount sonucu yok");
    const drifts = await istek("GET", "/admin/counter-drifts?limit=10", { token: ctx.mudur });
    bekleStatus(drifts, 200);
    return `rezervasyon kapasiteye yansidi, recount ${recount.veri.stored}/${recount.veri.recomputed}`;
  });

  await kontrol("T-4", "Odeme ve cikis karari", async () => {
    olmali(ctx.t4 && ctx.t4.paymentId, "T-4 odeme verisi yok");
    try {

    const teklifOku = () =>
      istek("GET", `/kiosk/quote?lot_id=${ctx.lotId}&plate=${encodeURIComponent(ctx.t4.plaka)}`, {
        cameraKey: CAMERA_KEY,
      });
    const durumOku = async () => {
      const liste = await istek("GET", "/payments?limit=50", { token: ctx.muhasebe });
      if (liste.durum !== 200) return null;
      return liste.veri.items.find((x) => x.id === ctx.t4.paymentId) || null;
    };
    const bildir = async () => {
      if (!WEBHOOK_SECRET) return;
      try {
        await imzaliBildirim(ctx.t4.providerRef, ctx.t4.amountKurus);
      } catch {
        // Saglayici simulatoru zaten onaylamis olabilir; yoklama devam eder.
      }
    };

    // Saglayici simulatoru zaman asimi/gec onay uretse de akisi deterministik
    // bitirmek icin imzali basarili bildirim gonderilir.
    await bildir();

    let odendi = false;
    for (let deneme = 0; deneme < 4 && !odendi; deneme++) {
      const bitis = Date.now() + 20000;
      while (Date.now() < bitis) {
        const teklif = await teklifOku();
        if (teklif.durum === 200 && teklif.veri.paid_kurus > 0 && teklif.veri.due_kurus === 0) {
          odendi = true;
          break;
        }
        const odeme = await durumOku();
        if (odeme && (odeme.status === "failed" || odeme.status === "timed_out")) break;
        await bekle(1000);
      }
      if (odendi) break;

      const teklif = await teklifOku();
      if (teklif.durum === 200 && teklif.veri.paid_kurus > 0) {
        odendi = true;
        break;
      }
      if (teklif.durum !== 200 || teklif.veri.due_kurus <= 0) {
        await bekle(2000);
        continue;
      }
      const anahtar = `verify-pay-${runTag}-${ctx.t4.sessionId}-${deneme + 2}`;
      const yeni = await istek("POST", "/kiosk/payments", {
        cameraKey: CAMERA_KEY,
        headers: { "Idempotency-Key": anahtar },
        body: { session_id: teklif.veri.session_id, amount_kurus: teklif.veri.due_kurus },
      });
      if (yeni.durum === 201) {
        ctx.t4.paymentId = yeni.veri.payment_id;
        ctx.t4.providerRef = yeni.veri.provider_ref;
        ctx.t4.amountKurus = teklif.veri.due_kurus;
        await bildir();
      } else {
        // Onceki odeme hala pending; zaman asimini bekle.
        await bekle(5000);
      }
    }
    olmali(odendi, "saglayici odemeyi onaylamadi");

    const karar = await istek("GET", `/gate/decision?lot_id=${ctx.lotId}&plate=${encodeURIComponent(ctx.t4.plaka)}`, {
      cameraKey: CAMERA_KEY,
    });
    bekleStatus(karar, 200);
    olmali(karar.veri.action === "open", `kapi karari ${karar.veri.action}/${karar.veri.reason}`);

    const sureler = [];
    for (let i = 0; i < 100; i++) {
      const bas = performance.now();
      const y = await istek("GET", `/gate/decision?lot_id=${ctx.lotId}&plate=${encodeURIComponent(ctx.t4.plaka)}`, {
        cameraKey: CAMERA_KEY,
      });
      sureler.push(performance.now() - bas);
      if (y.durum !== 200) throw new Error("kapi karari HTTP " + y.durum);
    }
    sureler.sort((a, b) => a - b);
    const p95 = sureler[Math.floor(sureler.length * 0.95) - 1];
    olmali(p95 < 50, `kapi karari p95 ${p95.toFixed(2)} ms >= 50 ms`);

    const iade = await istek("POST", "/refunds", {
      token: ctx.muhasebe,
      headers: { "Idempotency-Key": `verify-refund-${runTag}` },
      body: { payment_id: ctx.t4.paymentId, amount_kurus: 100, reason: "manual" },
    });
    olmali([200, 201].includes(iade.durum), `iade HTTP ${iade.durum}: ${iade.metin.slice(0, 120)}`);
    return `kapi acildi, p95 ${p95.toFixed(2)} ms, iade ${iade.veri.status}`;
    } finally {
      if (ctx.t4 && ctx.t4.plaka) {
        try {
          await olay(ctx.lotId, "exit", ctx.t4.plaka, new Date().toISOString(), "verify-t4-kapat");
        } catch {
          // Temizlik en iyi caba; asil sonucu degistirmez.
        }
      }
    }
  });

  await kontrol("T-5", "Abonelik faturalama ve mutabakat", async () => {
    const planlar = await istek("GET", "/plans", { token: ctx.mudur });
    bekleStatus(planlar, 200);
    const plan = planlar.veri.items.find((p) => p.active);
    olmali(plan, "T-5 icin aktif plan yok");
    const abonelik = await istek("POST", "/subscriptions", {
      token: ctx.mudur,
      body: { plate: yeniPlaka("QM"), plan_code: plan.code },
    });
    bekleStatus(abonelik, 201);
    ctx.t5 = { subscriptionId: abonelik.veri.id };

    let hash = 0;
    for (const ch of runTag) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0;
    const ay = (hash % 12) + 1;
    const yil = 2030 + (hash % 60);
    const donem = `${yil}-${String(ay).padStart(2, "0")}-01`;
    const ilk = await istek("POST", "/billing/period-end", {
      token: ctx.muhasebe,
      body: { period_start: donem },
    });
    bekleStatus(ilk, 200);
    olmali(ilk.veri.invoices_created > 0, `donem sonu fatura uretmedi (${donem})`);
    const ikinci = await istek("POST", "/billing/period-end", {
      token: ctx.muhasebe,
      body: { period_start: donem },
    });
    bekleStatus(ikinci, 200);
    olmali(ikinci.veri.invoices_created === 0, "donem sonu idempotent degil");

    const faturalar = await istek("GET", `/invoices?subscription_id=${ctx.t5.subscriptionId}&limit=20`, {
      token: ctx.muhasebe,
    });
    bekleStatus(faturalar, 200);
    const acik = faturalar.veri.items.find((x) => x.status === "open");
    olmali(acik, "T-5 aboneligi icin acik fatura yok");
    let dunning = "";
    if (acik) {
      const deneme = await istek("POST", `/invoices/${acik.id}/payment-attempts`, {
        token: ctx.muhasebe,
        body: { success: false },
      });
      bekleStatus(deneme, 200);
      olmali(deneme.veri.next_retry_at !== null, "dunning yeniden deneme zamani yok");
      dunning = `dunning ${deneme.veri.attempts}. deneme`;
    }

    const gun = "1990-01-01";
    const mutabakat = await istek("POST", `/reconciliation/${gun}/provider-report`, {
      token: ctx.muhasebe,
      body: [{ provider_ref: `verify-${runTag}`, amount_kurus: 1234 }],
    });
    bekleStatus(mutabakat, 200);
    olmali(mutabakat.veri.report && mutabakat.veri.report.diff_kurus !== 0, "mutabakat farki bulunmadi");
    const rapor = await istek("GET", `/reconciliation/${gun}`, { token: ctx.muhasebe });
    bekleStatus(rapor, 200);
    return `donem ${donem}: ${ilk.veri.invoices_created} fatura, ${dunning}, mutabakat farki ${mutabakat.veri.report.diff_kurus}`;
  });

  const gecen = sonuclar.filter((x) => x.ok).length;
  const kalan = sonuclar.length - gecen;
  console.log("\n| Kimlik | Sonuc | Ayrinti |");
  console.log("|---|---|---|");
  for (const s of sonuclar) {
    console.log(`| ${s.id} | ${s.ok ? "PASS" : "FAIL"} | ${s.ayrinti.replace(/\|/g, "\\|")} |`);
  }
  console.log(`\n${gecen}/${sonuclar.length} PASS, ${kalan} FAIL`);
  process.exit(kalan === 0 ? 0 : 1);
}

main().catch((hata) => {
  console.error("Dogrulama beklenmedik sekilde durdu:", hata);
  process.exit(2);
});
