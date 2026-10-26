// test/faz27.js — Faz 27: OpenAPI 3.1 dokümantasyonu.
//   1) docs/openapi.yaml ile internal/httpx/openapi.yaml birebir ayni
//   2) YAML'deki yol+metot kumesi koddaki rotalarla ayni (eksik/fazla yok)
//   3) redocly lint gecer
//   4) GET /openapi.yaml Go testi 200 doner
// Yalnizca Node standart kutuphanesi kullanilir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const HTTPX = path.join(KOK, "internal", "httpx");
const DOCS_YAML = path.join(KOK, "docs", "openapi.yaml");
const EMBED_YAML = path.join(HTTPX, "openapi.yaml");
const NPM = process.platform === "win32" ? process.env.ComSpec || "cmd.exe" : "npm";

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
    timeout: s.timeout || 600000,
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

function npx(args, timeout) {
  if (process.platform === "win32") {
    return calistir(NPM, ["/d", "/s", "/c", "npx " + args.join(" ")], { timeout: timeout });
  }
  return calistir("npx", args, { timeout: timeout });
}

function yamlRotalari(metin) {
  const satirlar = metin.split(/\r?\n/);
  const rotalar = [];
  let yollarda = false;
  let aktifYol = null;
  for (const satir of satirlar) {
    if (/^paths:\s*$/.test(satir)) {
      yollarda = true;
      continue;
    }
    if (!yollarda) continue;
    if (/^\S/.test(satir)) {
      yollarda = false;
      aktifYol = null;
      continue;
    }
    const yol = /^ {2}(\/[^:]+):\s*$/.exec(satir);
    if (yol) {
      aktifYol = yol[1];
      continue;
    }
    const metot = /^ {4}(get|post|put|patch|delete|options|head|trace):\s*$/.exec(satir);
    if (metot && aktifYol) {
      rotalar.push(metot[1].toUpperCase() + " " + aktifYol);
    }
  }
  return rotalar;
}

function routeBloklari(metin) {
  const bloklar = [];
  const desen = /\.Route\(\s*"([^"]+)"/g;
  let m;
  while ((m = desen.exec(metin))) {
    const acilis = metin.indexOf("{", m.index + m[0].length);
    if (acilis < 0) continue;
    let derinlik = 0;
    let i = acilis;
    for (; i < metin.length; i++) {
      if (metin[i] === "{") derinlik++;
      else if (metin[i] === "}") {
        derinlik--;
        if (derinlik === 0) break;
      }
    }
    bloklar.push({ bas: acilis, son: i, onek: m[1] });
  }
  return bloklar;
}

function kodRotalari(metin) {
  const bloklar = routeBloklari(metin);
  const rotalar = [];
  const desen = /\.\s*(Get|Post|Put|Patch|Delete)\s*\(\s*"(\/[^"]*)"/g;
  let m;
  while ((m = desen.exec(metin))) {
    let yol = m[2];
    for (const blok of bloklar) {
      if (m.index > blok.bas && m.index < blok.son) {
        yol = blok.onek + yol;
      }
    }
    rotalar.push(m[1].toUpperCase() + " " + yol);
  }
  return rotalar;
}

function kume(liste, ad) {
  const gorulen = new Set();
  for (const oge of liste) {
    assert.ok(!gorulen.has(oge), `${ad} icinde tekrar eden uc: ${oge}`);
    gorulen.add(oge);
  }
  return gorulen;
}

let uclSayisi = 0;

try {
  ok("1 docs/openapi.yaml var", () => {
    assert.ok(fs.existsSync(DOCS_YAML), "docs/openapi.yaml yok");
  });

  ok("2 internal/httpx/openapi.yaml docs/openapi.yaml ile birebir ayni", () => {
    assert.ok(fs.existsSync(EMBED_YAML), "internal/httpx/openapi.yaml yok");
    const docs = fs.readFileSync(DOCS_YAML);
    const embed = fs.readFileSync(EMBED_YAML);
    assert.ok(docs.equals(embed), "iki OpenAPI dosyasi ayni degil");
  });

  const yamlMetin = fs.readFileSync(DOCS_YAML, "utf8");
  const yamlKume = kume(yamlRotalari(yamlMetin), "docs/openapi.yaml");
  uclSayisi = yamlKume.size;

  ok("3 YAML en az 30 uc tanimliyor", () => {
    assert.ok(uclSayisi >= 30, `YAML'de ${uclSayisi} uc var`);
  });

  ok("4 YAML'deki yol+metot kumesi koddaki rotalarla ayni", () => {
    const kod = [];
    for (const dosya of fs.readdirSync(HTTPX)) {
      if (!dosya.endsWith(".go") || dosya.endsWith("_test.go")) continue;
      kod.push(...kodRotalari(fs.readFileSync(path.join(HTTPX, dosya), "utf8")));
    }
    const kodKume = kume(kod, "internal/httpx/*.go");
    const eksik = [...kodKume].filter((u) => !yamlKume.has(u)).sort();
    const fazla = [...yamlKume].filter((u) => !kodKume.has(u)).sort();
    assert.deepStrictEqual(
      { eksik, fazla },
      { eksik: [], fazla: [] },
      `eksik: ${eksik.join(", ") || "-"}; fazla: ${fazla.join(", ") || "-"}`
    );
  });

  ok("5 redocly lint gecti", () => {
    const r = npx(["--yes", "@redocly/cli@1.25.0", "lint", "docs/openapi.yaml"], 600000);
    assert.strictEqual(r.status, 0, komutHatasi("redocly lint", r));
  });

  ok("6 GET /openapi.yaml Go testi 200 doner", () => {
    const kaynak = path.resolve(KOK);
    const r = calistir(
      "docker",
      [
        "run",
        "--rm",
        "-v",
        kaynak + ":/src",
        "-v",
        "parkakil-gomod:/go/pkg/mod",
        "-w",
        "/src",
        "-e",
        "GOFLAGS=-buildvcs=false",
        "golang:1.23-alpine",
        "sh",
        "-c",
        "go test -count=1 ./internal/httpx/ -run OpenAPI",
      ],
      { timeout: 600000 }
    );
    assert.strictEqual(r.status, 0, komutHatasi("go test OpenAPI", r));
    assert.ok(
      /ok\s+parkakil\/internal\/httpx/.test(String(r.stdout || "")),
      "go test ciktisi beklenen bicimde degil:\n" + kirp(r.stdout, 20)
    );
  });

  console.log(`\n${n} kontrol gecti; ${uclSayisi} uc dogrulandi.`);
} catch (err) {
  console.error("\nHATA: " + (err && err.message ? err.message : err));
  process.exit(1);
}
