// test/faz19.js — gozlemlenebilirlik ve operasyon (faz19).
//   a) Docker kontrolu (postgres/DB GEREKMEZ)
//   b) golang container icinde TestMetrics testleri + kapsam
//   c) go vet ./... temiz, gofmt -l . bos
//   d) k6 inspect basarili (grafana/k6:0.54.0)
//   e) iki operasyon betigi sh -n ile sozdizimi temiz
//   f) Makefile'da load ve chaos hedefleri (.PHONY dahil)
// Yalnizca Node standart kutuphanesi kullanilir. Komutlar shell'siz spawnSync ile calisir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const BEKLENEN_TESTLER = [
  "TestMetricsRequiresToken",
  "TestMetricsFailClosedWithoutConfiguredToken",
  "TestMetricsOKAndContentType",
  "TestMetricsCountsAndHistogram",
  "TestMetricsUnmatchedRoute",
  "TestMetricsConcurrentRequests",
];

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const GO_IMGE = "golang:1.23-alpine";
const K6_IMGE = "grafana/k6:0.54.0";

let n = 0;
function ok(ad, f) {
  f();
  n++;
  console.log("  ok  " + ad);
}

function calistir(komut, args, secenek) {
  const s = secenek || {};
  return spawnSync(komut, args, {
    cwd: KOK,
    encoding: "utf8",
    windowsHide: true,
    timeout: s.timeout || 120000,
    env: s.env || process.env,
    maxBuffer: 64 * 1024 * 1024,
  });
}

function kirp(metin, satirSayisi) {
  const satirlar = String(metin || "").trim().split(/\r?\n/);
  return satirlar.slice(-(satirSayisi || 80)).join("\n");
}

function komutHatasi(komut, args, r) {
  const parcalar = [];
  if (r.error) {
    parcalar.push(`${komut} ${args.slice(0, 4).join(" ")} calistirilamadi: ${r.error.message}`);
  } else {
    parcalar.push(`${komut} ${args.slice(0, 4).join(" ")} basarisiz (kod ${r.status})`);
  }
  if ((r.stderr || "").trim()) {
    parcalar.push("stderr (son 20 satir):\n" + kirp(r.stderr, 20));
  }
  if ((r.stdout || "").trim()) {
    parcalar.push("stdout (son 80 satir):\n" + kirp(r.stdout, 80));
  }
  return parcalar.join("\n");
}

function basarili(komut, args, secenek) {
  const r = calistir(komut, args, secenek);
  if (r.error || r.status !== 0) {
    throw new Error(komutHatasi(komut, args, r));
  }
  return r;
}

function bolum(metin, bas, son) {
  const i = metin.indexOf(bas);
  if (i < 0) return "";
  const j = metin.indexOf(son, i + bas.length);
  return j < 0 ? "" : metin.slice(i + bas.length, j);
}

function dockerKontrol() {
  const r = calistir("docker", ["info", "--format", "{{.ServerVersion}}"], { timeout: 30000 });
  if (r.error || r.status !== 0) {
    throw new Error(
      "Docker bulunamadi ya da calismiyor; faz19 testi Docker olmadan dogrulanamaz.\n" +
        kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20)
    );
  }
}

function main() {
  console.log("== faz19: /metrics, k6 yuk testi ve kaos betigi");
  dockerKontrol();

  const komut = [
    "go test -count=1 -run 'TestMetrics' -cover -v ./internal/httpx/",
    "&& echo '---VET-BAS---'",
    "&& go vet ./...",
    "&& echo '---VET-SON---'",
    "&& echo '---GOFMT-BAS---'",
    "&& gofmt -l .",
    "&& echo '---GOFMT-SON---'",
  ].join(" ");

  const go = calistir(
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
      GO_IMGE,
      "sh",
      "-c",
      komut,
    ],
    { timeout: 900000 }
  );

  const cikti = `${go.stdout || ""}\n${go.stderr || ""}`;
  console.log(kirp(cikti, 160));

  ok("b1 go test -run TestMetrics cikis kodu 0", () => {
    if (go.error || go.status !== 0) {
      throw new Error("TestMetrics basarisiz\n" + komutHatasi("docker", ["run", GO_IMGE], go));
    }
  });

  ok("b2 ciktida '--- SKIP' yok", () => {
    assert.ok(!/---\s*SKIP/.test(cikti), "atlanan test var:\n" + kirp(cikti, 40));
  });

  for (const ad of BEKLENEN_TESTLER) {
    ok(`b3 "${ad}" PASS`, () => {
      const desen = new RegExp(`^\\s*--- PASS: ${ad}(\\s|$)`, "m");
      assert.match(cikti, desen, ad + " icin '--- PASS' satiri yok");
    });
  }

  ok("b4 kapsam raporu var ve sifirdan buyuk", () => {
    const m = cikti.match(/coverage:\s*([0-9.]+)% of statements/);
    assert.ok(m, "coverage satiri okunamadi");
    const kapsam = Number(m[1]);
    assert.ok(kapsam > 0, `httpx kapsami %${kapsam}`);
    console.log(`  bilgi  internal/httpx kapsami: %${kapsam.toFixed(1)}`);
  });

  ok("b5 go vet ./... temiz", () => {
    assert.ok(cikti.includes("---VET-SON---"), "go vet tamamlanmadi:\n" + kirp(cikti, 60));
  });

  ok("b6 gofmt -l . bos", () => {
    const govde = bolum(cikti, "---GOFMT-BAS---", "---GOFMT-SON---").trim();
    assert.strictEqual(govde, "", "gofmt gerektiren dosyalar:\n" + govde);
  });

  ok("b7 ornek /metrics ciktisi gorunur (ilk 15 satir)", () => {
    const m = cikti.match(/ornek \/metrics cikti \(ilk 15 satir\):\n([\s\S]*?)(?:\n\s*--- PASS|\n\s*=== RUN)/);
    assert.ok(m, "/metrics ornek ciktisi test logunda yok");
    console.log("  bilgi  /metrics ilk satirlar:\n" + m[1]);
  });

  ok("c1 k6 inspect basarili", () => {
    const r = calistir(
      "docker",
      [
        "run",
        "--rm",
        "-v",
        `${KOK_MOUNT}/scripts/load:/scripts/load`,
        "-w",
        "/scripts/load",
        K6_IMGE,
        "inspect",
        "k6-gate.js",
      ],
      { timeout: 300000 }
    );
    if (r.error || r.status !== 0) {
      throw new Error("k6 inspect basarisiz\n" + komutHatasi("docker", ["run", K6_IMGE, "inspect"], r));
    }
    const metin = `${r.stdout || ""}\n${r.stderr || ""}`;
    assert.ok(metin.includes("camera_events"), "inspect ciktisinda camera_events senaryosu yok");
    assert.ok(metin.includes("health"), "inspect ciktisinda health senaryosu yok");
  });

  ok("d1 scripts/load/run.sh sh -n temiz", () => {
    basarili("sh", ["-n", path.join(KOK, "scripts/load/run.sh")], { timeout: 30000 });
  });

  ok("d2 scripts/chaos/postgres-kesintisi.sh sh -n temiz", () => {
    basarili("sh", ["-n", path.join(KOK, "scripts/chaos/postgres-kesintisi.sh")], { timeout: 30000 });
  });

  const makefile = fs.readFileSync(path.join(KOK, "Makefile"), "utf8");

  ok("e1 Makefile'da load hedefi var", () => {
    assert.match(makefile, /^load:[ \t]*$/m, "load hedefi yok");
    assert.match(makefile, /sh scripts\/load\/run\.sh/, "load hedefi run.sh cagirmiyor");
  });

  ok("e2 Makefile'da chaos hedefi var", () => {
    assert.match(makefile, /^chaos:[ \t]*$/m, "chaos hedefi yok");
    assert.match(
      makefile,
      /sh scripts\/chaos\/postgres-kesintisi\.sh/,
      "chaos hedefi postgres-kesintisi.sh cagirmiyor"
    );
  });

  ok("e3 .PHONY load ve chaos iceriyor", () => {
    const m = makefile.match(/^\.PHONY:(.*)$/m);
    assert.ok(m, ".PHONY satiri yok");
    for (const hedef of ["load", "chaos"]) {
      assert.ok(m[1].split(/\s+/).includes(hedef), ".PHONY " + hedef + " icermiyor");
    }
  });

  console.log(`\nfaz19: ${n} kontrol gecti`);
}

try {
  main();
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exit(1);
}
