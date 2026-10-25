// test/faz4.js — T-2 tarife motoru: saf domain paketi, kapsam ve yasaklar.
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
const PAKET = path.join(KOK, "internal", "domain", "tariff");
const PAKET_GORELI = "./internal/domain/tariff";
const KAYNAKLAR = ["money.go", "version.go", "calc.go"];
const TESTLER = ["money_test.go", "version_test.go", "calc_test.go"];
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
      "faz4: Docker bulunamadi ya da calismiyor; tarife testi Docker olmadan dogrulanamaz.\n" +
        kirp((bilgi.stdout || "") + "\n" + (bilgi.stderr || ""))
    );
    process.exit(1);
  }

  console.log("\n== a) statik kontroller");

  ok("a1 paket kaynaklari, testleri ve dokuman mevcut", () => {
    for (const ad of KAYNAKLAR.concat(TESTLER)) {
      const yol = path.join(PAKET, ad);
      assert.ok(fs.existsSync(yol), yol + " yok");
    }
    assert.ok(fs.existsSync(path.join(KOK, "docs", "T2-tarife.md")), "docs/T2-tarife.md yok");
  });

  ok("a2 kaynakta float32/float64/time.Now( yok (testler haric)", () => {
    for (const ad of KAYNAKLAR) {
      const metin = fs.readFileSync(path.join(PAKET, ad), "utf8");
      for (const yasak of ["float32", "float64", "time.Now("]) {
        assert.ok(!metin.includes(yasak), ad + " icinde yasakli ifade: " + yasak);
      }
    }
  });

  console.log("\n== b) go test ve kapsam");

  let testCiktisi = "";
  ok("b1 go test gecerli ve kapsam profili uretiliyor", () => {
    const r = goBasarili(
      "go test -count=1 -coverprofile=/tmp/c.out " + PAKET_GORELI + " && go tool cover -func=/tmp/c.out"
    );
    testCiktisi = (r.stdout || "") + "\n" + (r.stderr || "");
    assert.match(testCiktisi, /^ok\s+parkakil\/internal\/domain\/tariff/m, kirp(testCiktisi));
  });

  ok(`b2 toplam kapsam >= %${KAPSAM_ESIGI}`, () => {
    const m = testCiktisi.match(/total:\s+\(statements\)\s+([0-9.]+)%/);
    assert.ok(m, "kapsam satiri bulunamadi:\n" + kirp(testCiktisi));
    const kapsam = Number(m[1]);
    assert.ok(kapsam >= KAPSAM_ESIGI, `toplam kapsam %${kapsam} < %${KAPSAM_ESIGI}`);
    console.log("      kapsam: %" + kapsam);
  });

  console.log("\n== c) import yasaklari");

  ok("c1 paket importlarinda database/sql, net/http, pgx, os yok; time/tzdata var", () => {
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
    assert.ok(importlar.includes("time/tzdata"), "time/tzdata importu yok: " + importlar.join(", "));
  });

  console.log(`\nfaz4: ${n} kontrol gecti`);
}

try {
  main();
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exit(1);
}
