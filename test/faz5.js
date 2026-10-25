// test/faz5.js — F-1/F-2 parça A: oturum karar paketi, migrasyon ve yasaklar.
//   a) statik kontroller | b) Docker ile go test + kapsam | c) import yasaklari
// Docker yoksa test ATLANMAZ; acik mesajla basarisiz olur.
// Yalnizca Node standart kutuphanesi kullanilir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const PAKET = path.join(KOK, "internal", "domain", "session");
const PAKET_GORELI = "./internal/domain/session";
const KAYNAKLAR = ["event.go", "decide.go"];
const TESTLER = ["event_test.go", "decide_test.go"];
const IZINLI_IMPORTLAR = ["errors", "fmt", "strconv", "time", "parkakil/internal/domain/plate"];
const KAPSAM_ESIGI = 85;

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
    timeout: s.timeout || 300000,
    env: s.env || process.env,
    maxBuffer: 32 * 1024 * 1024,
  });
}

function kirp(metin, satirSayisi) {
  const satirlar = String(metin || "").trim().split(/\r?\n/);
  return satirlar.slice(-(satirSayisi || 60)).join("\n");
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
      "golang:1.23-alpine",
      "sh",
      "-c",
      komut,
    ],
    { timeout: timeout || 300000 }
  );
}

function goBasarili(komut) {
  const r = goDocker(komut);
  if (r.error || r.status !== 0) {
    throw new Error(
      "docker go komutu basarisiz: " + komut + "\n" + kirp((r.stdout || "") + "\n" + (r.stderr || ""))
    );
  }
  return r;
}

function main() {
  const bilgi = calistir("docker", ["info", "--format", "{{.ServerVersion}}"]);
  if (bilgi.error || bilgi.status !== 0) {
    console.error(
      "faz5: Docker bulunamadi ya da calismiyor; oturum paketi testi Docker olmadan dogrulanamaz.\n" +
        kirp((bilgi.stdout || "") + "\n" + (bilgi.stderr || ""))
    );
    process.exit(1);
  }

  console.log("\n== a) statik kontroller");

  ok("a1 paket kaynaklari, testleri, migrasyon ve dokuman mevcut", () => {
    for (const ad of KAYNAKLAR.concat(TESTLER)) {
      const yol = path.join(PAKET, ad);
      assert.ok(fs.existsSync(yol), yol + " yok");
    }
    assert.ok(fs.existsSync(path.join(KOK, "migrations", "00002_oturum.sql")), "migrations/00002_oturum.sql yok");
    assert.ok(fs.existsSync(path.join(KOK, "docs", "F2-oturum.md")), "docs/F2-oturum.md yok");
  });

  ok("a2 migrasyon goose biciminde, btree_gist Down'da dusurulmuyor", () => {
    const metin = fs.readFileSync(path.join(KOK, "migrations", "00002_oturum.sql"), "utf8");
    assert.match(metin, /^-- \+goose Up\s*$/m, "-- +goose Up yok");
    assert.match(metin, /^-- \+goose Down\s*$/m, "-- +goose Down yok");
    assert.match(metin, /CREATE EXTENSION IF NOT EXISTS btree_gist/, "btree_gist yok");
    assert.ok(!/DROP EXTENSION/i.test(metin), "btree_gist Down'da dusurulmemeli");
    for (const tablo of ["lots", "tariff_versions", "plate_events", "sessions", "review_items"]) {
      assert.match(metin, new RegExp("CREATE TABLE " + tablo + "\\b"), tablo + " tablosu yok");
    }
    assert.match(metin, /outcome IN \('opened','duplicate_entry','closed','duplicate_exit','orphan_exit','review'\)/);
  });

  console.log("\n== b) go test ve kapsam");

  let testCiktisi = "";
  ok("b1 go test gecerli ve kapsam profili uretiliyor", () => {
    const r = goBasarili(
      "go test -count=1 -coverprofile=/tmp/c.out " + PAKET_GORELI + " && go tool cover -func=/tmp/c.out"
    );
    testCiktisi = (r.stdout || "") + "\n" + (r.stderr || "");
    assert.match(testCiktisi, /^ok\s+parkakil\/internal\/domain\/session/m, kirp(testCiktisi));
  });

  ok(`b2 toplam kapsam >= %${KAPSAM_ESIGI}`, () => {
    const m = testCiktisi.match(/total:\s+\(statements\)\s+([0-9.]+)%/);
    assert.ok(m, "kapsam satiri bulunamadi:\n" + kirp(testCiktisi));
    const kapsam = Number(m[1]);
    assert.ok(kapsam >= KAPSAM_ESIGI, `toplam kapsam %${kapsam} < %${KAPSAM_ESIGI}`);
    console.log("      kapsam: %" + kapsam);
  });

  console.log("\n== c) import yasaklari");

  ok("c1 paket importlarinda database/sql, net/http, pgx, os yok", () => {
    const r = goBasarili("go list -f '{{join .Imports \"\\n\"}}' " + PAKET_GORELI);
    const importlar = r.stdout
      .split(/\r?\n/)
      .map((s) => s.trim())
      .filter(Boolean);
    assert.ok(importlar.length > 0, "import listesi bos");

    const yasaklar = [
      ["database/sql", (i) => i === "database/sql"],
      ["net/http", (i) => i === "net/http"],
      ["pgx", (i) => i.includes("pgx")],
      ["os", (i) => i === "os"],
    ];
    for (const [ad, kontrol] of yasaklar) {
      assert.ok(!importlar.some(kontrol), "yasakli import: " + ad + " (" + importlar.join(", ") + ")");
    }
    for (const i of importlar) {
      assert.ok(IZINLI_IMPORTLAR.includes(i), "izinsiz import: " + i + " (" + importlar.join(", ") + ")");
    }
  });

  console.log(`\nfaz5: ${n} kontrol gecti`);
}

try {
  main();
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exit(1);
}
