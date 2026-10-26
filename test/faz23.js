// test/faz23.js — Faz 23: rol ekranlari (React) API uclarina bagli.
//   1) web/ bagimliliklari npm ci ile kurulur
//   2) typecheck, vitest (en az 40 test) ve uretim derlemesi gecer
//   3) yeni ekranlar ve format yardimcisi yerinde
// Yalnizca Node standart kutuphanesi kullanilir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const WEB = path.join(KOK, "web");
const NPM = process.platform === "win32" ? process.env.ComSpec || "cmd.exe" : "npm";
const BEKLENEN_ASGARI_TEST = 40;

const BEKLENEN_DOSYALAR = [
  "src/format.ts",
  "src/pages/SurucuPage.tsx",
  "src/pages/GorevliPage.tsx",
  "src/pages/MuhasebePage.tsx",
  "src/pages/MudurPage.tsx",
  "src/pages/YoneticiPage.tsx",
];

let n = 0;
const ok = (ad, f) => {
  f();
  n++;
  console.log("  ok  " + ad);
};

function calistir(komut, args, secenek) {
  const s = secenek || {};
  return spawnSync(komut, args, {
    cwd: s.cwd || KOK,
    encoding: "utf8",
    windowsHide: true,
    timeout: s.timeout || 300000,
    maxBuffer: 64 * 1024 * 1024,
    env: process.env,
  });
}

function kirp(metin, satirSayisi) {
  const temiz = String(metin || "").replace(/\u001b\[[0-9;]*m/g, "");
  const satirlar = temiz.trim().split(/\r?\n/);
  return satirlar.slice(-(satirSayisi || 40)).join("\n");
}

function komutHatasi(ad, r) {
  const parcalar = [];
  if (r.error) {
    parcalar.push(`${ad} calistirilamadi: ${r.error.message}`);
  } else {
    parcalar.push(`${ad} basarisiz (kod ${r.status})`);
  }
  if ((r.stdout || "").trim()) {
    parcalar.push("stdout (son 40 satir):\n" + kirp(r.stdout, 40));
  }
  if ((r.stderr || "").trim()) {
    parcalar.push("stderr (son 40 satir):\n" + kirp(r.stderr, 40));
  }
  return parcalar.join("\n");
}

function npm(args, timeout) {
  if (process.platform === "win32") {
    return calistir(NPM, ["/d", "/s", "/c", "npm " + args.join(" ")], {
      cwd: WEB,
      timeout: timeout || 300000,
    });
  }
  return calistir(NPM, args, { cwd: WEB, timeout: timeout || 300000 });
}

let testSayisi = 0;

try {
  ok("1 web/package-lock.json var (npm ci icin)", () => {
    assert.ok(
      fs.existsSync(path.join(WEB, "package-lock.json")),
      "web/package-lock.json yok"
    );
  });

  ok("2 yeni ekran dosyalari ve format yardimcisi var", () => {
    for (const dosya of BEKLENEN_DOSYALAR) {
      assert.ok(
        fs.existsSync(path.join(WEB, dosya)),
        "eksik dosya: web/" + dosya
      );
    }
  });

  ok("3 npm ci gecti", () => {
    const r = npm(["ci", "--no-audit", "--no-fund"], 600000);
    assert.strictEqual(r.status, 0, komutHatasi("npm ci", r));
  });

  ok("4 npm run typecheck gecti", () => {
    const r = npm(["run", "typecheck"]);
    assert.strictEqual(r.status, 0, komutHatasi("npm run typecheck", r));
  });

  ok(`5 npm test gecti ve en az ${BEKLENEN_ASGARI_TEST} test kostu`, () => {
    const r = npm(["test"], 300000);
    assert.strictEqual(r.status, 0, komutHatasi("npm test", r));
    const temiz = String(r.stdout || "").replace(/\u001b\[[0-9;]*m/g, "");
    const m = temiz.match(/Tests\s+(\d+) passed/);
    assert.ok(m, "test sayisi okunamadi:\n" + kirp(temiz, 40));
    testSayisi = Number(m[1]);
    assert.ok(
      testSayisi >= BEKLENEN_ASGARI_TEST,
      `test sayisi ${testSayisi}, en az ${BEKLENEN_ASGARI_TEST} olmali`
    );
  });

  ok("6 npm run build gecti ve web/dist/index.html olustu", () => {
    const r = npm(["run", "build"]);
    assert.strictEqual(r.status, 0, komutHatasi("npm run build", r));
    const indeks = path.join(WEB, "dist", "index.html");
    assert.ok(fs.existsSync(indeks), "web/dist/index.html yok");
    assert.ok(fs.statSync(indeks).size > 0, "web/dist/index.html bos");
  });

  console.log(`\nfaz23: ${n} kontrol gecti (${testSayisi} vitest testi)`);
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exitCode = 1;
}
