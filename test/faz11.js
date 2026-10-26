// test/faz11.js — T-4 odeme domain paketi: Go testleri, kapsam ve saflık denetimi.
//   1) go test + kapsam (Docker, coverprofile konteyner icinde /tmp)
//   2) toplam kapsam >= %90
//   3) paket import listesinde database/sql, net/http, os, math/rand, crypto/rand yok
// Kontroller git durumundan bagimsizdir; commit oncesi ve sonrasi gecer.
// Docker yoksa test ATLANMAZ; acik mesajla basarisiz olur.
// Yalnizca Node standart kutuphanesi kullanilir.
"use strict";
const assert = require("assert");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const PAKET = "./internal/domain/payment/";
const IMaj = "golang:1.23-alpine";
const YASAK = ["database/sql", "net/http", "os", "math/rand", "crypto/rand"];
const KAPSAM_ESIGI = 90;

let n = 0;
const ok = (ad, f) => {
  f();
  n++;
  console.log("  ok  " + ad);
};

function calistir(komut, args, secenek) {
  const s = secenek || {};
  return spawnSync(komut, args, {
    cwd: KOK,
    encoding: "utf8",
    windowsHide: true,
    timeout: s.timeout || 120000,
    maxBuffer: 32 * 1024 * 1024,
  });
}

function kirp(metin, satirSayisi) {
  const satirlar = String(metin || "").trim().split(/\r?\n/);
  return satirlar.slice(-(satirSayisi || 40)).join("\n");
}

function goDocker(komut, timeout) {
  return calistir(
    "docker",
    [
      "run",
      "--rm",
      "-v",
      `${KOK_MOUNT}:/src`,
      "-v",
      "parkakil-gomod:/go/pkg/mod",
      "-w",
      "/src",
      "-e",
      "GOFLAGS=-buildvcs=false",
      IMaj,
      "sh",
      "-c",
      komut,
    ],
    { timeout: timeout || 600000 }
  );
}

function goHatasi(komut, r) {
  return (
    "docker go komutu basarisiz: " + komut + "\n" +
    kirp((r.stdout || "") + "\n" + (r.stderr || ""))
  );
}

const info = calistir("docker", ["info", "--format", "{{.ServerVersion}}"], { timeout: 30000 });
if (info.error || info.status !== 0) {
  console.error(
    "Docker bulunamadi ya da calismiyor; faz11 testi Docker olmadan dogrulanamaz.\n" +
      kirp((info.stdout || "") + "\n" + (info.stderr || ""))
  );
  process.exit(1);
}

const TEST_KOMUTU =
  "go test -count=1 -v -coverprofile=/tmp/c.out " +
  PAKET +
  " && go tool cover -func=/tmp/c.out";

let testCikti = "";
let kapsam = 0;

try {
  ok("1 go test odeme paketi (Docker) geciyor", () => {
    const r = goDocker(TEST_KOMUTU);
    testCikti = (r.stdout || "") + "\n" + (r.stderr || "");
    if (r.error) throw new Error(goHatasi(TEST_KOMUTU, r));
    assert.strictEqual(r.status, 0, goHatasi(TEST_KOMUTU, r));
    assert.match(testCikti, /^ok\s+parkakil\/internal\/domain\/payment/m, kirp(testCikti));
  });

  ok(`2 toplam kapsam >= %${KAPSAM_ESIGI}`, () => {
    const m = testCikti.match(/total:\s+\(statements\)\s+([0-9.]+)%/);
    assert.ok(m, "kapsam satiri bulunamadi:\n" + kirp(testCikti));
    kapsam = parseFloat(m[1]);
    assert.ok(kapsam >= KAPSAM_ESIGI, `kapsam %${kapsam}: %${KAPSAM_ESIGI} altinda`);
  });

  ok("3 import listesi temiz (DB/HTTP/os/rand yok)", () => {
    const komut = "go list -f '{{join .Imports \" \"}}' " + PAKET;
    const r = goDocker(komut, 300000);
    if (r.error) throw new Error(goHatasi(komut, r));
    assert.strictEqual(r.status, 0, goHatasi(komut, r));
    const importlar = r.stdout.trim().split(/\s+/).filter(Boolean);
    for (const yasak of YASAK) {
      assert.ok(!importlar.includes(yasak), "paket yasakli import kullaniyor: " + yasak);
    }
    for (const imp of importlar) {
      assert.ok(!imp.includes("pgx"), "paket pgx kullaniyor: " + imp);
    }
    console.log("     importlar: " + (importlar.join(" ") || "(yok)"));
  });

  console.log(`\nfaz11: ${n} kontrol gecti (kapsam %${kapsam})`);
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exit(1);
}
