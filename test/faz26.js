// test/faz26.js — gelistirici notlari (faz26): tablo onekleri, node basligi,
// hz ucu, seed kullanicisi, container_name ve README satiri.
//   a) statik kontroller (README, compose)
//   b) gecici docker agi + bos postgres container (host'a PORT ACILMAZ)
//   c) migration'lar bos veritabanina uygulanir, pg_tables denetlenir
//   d) 00009 down/up Go testleri
//   e) X-DDO-Node ve /_hz/45a6 Go testleri
//   f) her durumda container/ag temizligi
// Yalnizca Node standart kutuphanesi kullanilir. Komutlar shell'siz spawnSync ile calisir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const BEKLENEN_TABLOLAR = [
  "ya-app_meta",
  "ya-lots",
  "ya-tariff_versions",
  "ya-plate_events",
  "ya-sessions",
  "ya-review_items",
  "ya-payments",
  "ya-refunds",
  "ya-debts",
  "ya-qo-users",
  "ya-auth_sessions",
  "ya-audit_log",
  "ya-reservations",
  "ya-lot_counters",
  "ya-counter_drifts",
  "ya-plans",
  "ya-subscriptions",
  "ya-invoices",
  "ya-billing_runs",
  "ya-provider_settlements",
  "ya-reconciliation_reports",
  "ya-blacklist",
];

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const GO_IMGE = "golang:1.23-alpine";
const PG_IMGE = "postgres:16-alpine";

let n = 0;
function ok(ad, f) {
  f();
  n++;
  console.log("  ok  " + ad);
}
async function okAsync(ad, f) {
  await f();
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

function bekle(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function dockerKontrol() {
  const r = calistir("docker", ["info", "--format", "{{.ServerVersion}}"], { timeout: 30000 });
  if (r.error || r.status !== 0) {
    throw new Error(
      "Docker bulunamadi ya da calismiyor; faz26 testi Docker olmadan dogrulanamaz.\n" +
        kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20)
    );
  }
}

function servisBloklari(metin) {
  const sonuc = {};
  let servislerde = false;
  let aktif = null;
  for (const satir of metin.split(/\r?\n/)) {
    if (/^services:\s*$/.test(satir)) {
      servislerde = true;
      aktif = null;
      continue;
    }
    if (!servislerde) continue;
    if (/^\S/.test(satir)) {
      servislerde = false;
      aktif = null;
      continue;
    }
    const m = satir.match(/^ {2}([A-Za-z0-9_.-]+):\s*$/);
    if (m) {
      aktif = m[1];
      sonuc[aktif] = [];
      continue;
    }
    if (aktif) sonuc[aktif].push(satir);
  }
  return sonuc;
}

const temizlenen = new Set();
function temizle(container, ag) {
  if (container && !temizlenen.has(container)) {
    const r = calistir("docker", ["rm", "-f", "-v", container], { timeout: 120000 });
    if (r.error || r.status !== 0) {
      console.error("uyari: konteyner silinemedi:\n" + kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20));
    } else {
      temizlenen.add(container);
    }
  }
  if (ag && !temizlenen.has(ag)) {
    const r = calistir("docker", ["network", "rm", ag], { timeout: 120000 });
    if (r.error || r.status !== 0) {
      console.error("uyari: ag silinemedi:\n" + kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20));
    } else {
      temizlenen.add(ag);
    }
  }
}

function psql(container, sql) {
  const r = basarili(
    "docker",
    ["exec", container, "psql", "-U", "test", "-d", "test", "-tAc", sql],
    { timeout: 60000 }
  );
  return r.stdout.trim();
}

function goDocker(url, ag, komut, timeout) {
  return calistir(
    "docker",
    [
      "run",
      "--rm",
      "--network",
      ag,
      "-v",
      `${KOK_MOUNT}:/src`,
      "-v",
      "parkakil-gomod:/go/pkg/mod",
      "-w",
      "/src",
      "-e",
      "GOFLAGS=-buildvcs=false",
      "-e",
      `DATABASE_URL=${url}`,
      "-e",
      `TEST_DATABASE_URL=${url}`,
      "-e",
      "PARKAKIL_REQUIRE_DB=1",
      GO_IMGE,
      "sh",
      "-c",
      komut,
    ],
    { timeout: timeout || 900000 }
  );
}

async function main() {
  console.log("== faz26: gelistirici notlari");
  dockerKontrol();

  const sonek = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const ag = `parkakil-faz26-${sonek}`;
  const container = `parkakil-faz26-pg-${sonek}`;
  const url = `postgres://test:test@${container}:5432/test?sslmode=disable`;

  let hata = null;
  try {
    ok("a1 README son satiri 'DDO Case Kodu: ARDIC-45a6ab' ve tek satir sonu", () => {
      const metin = fs.readFileSync(path.join(KOK, "README.md"), "utf8");
      const satirlar = metin.split(/\r?\n/);
      assert.strictEqual(satirlar[satirlar.length - 1], "", "README tek satir sonu ile bitmeli");
      assert.strictEqual(
        satirlar[satirlar.length - 2],
        "DDO Case Kodu: ARDIC-45a6ab",
        "README son satiri yanlis"
      );
      assert.ok(!metin.endsWith("\n\n"), "README sonunda birden fazla satir sonu var");
    });

    ok("a2 compose api servisinde container_name: ddo-limon-api, digerlerinde yok", () => {
      const metin = fs.readFileSync(path.join(KOK, "docker-compose.yml"), "utf8");
      const bloklar = servisBloklari(metin);
      assert.ok(bloklar.api, "api servisi yok");
      assert.ok(
        bloklar.api.some((s) => /^\s{4}container_name\s*:\s*ddo-limon-api\s*$/.test(s)),
        "api servisinde container_name: ddo-limon-api yok"
      );
      for (const [ad, satirlar] of Object.entries(bloklar)) {
        if (ad === "api") continue;
        assert.ok(
          !satirlar.some((s) => /^\s{4}container_name\s*:/.test(s)),
          ad + " servisinde container_name olmamali"
        );
      }
    });

    ok("b1 gecici docker agi olusturuldu", () => {
      basarili("docker", ["network", "create", ag], { timeout: 60000 });
    });

    ok("b2 postgres:16-alpine baslatildi (host'a port acilmadi)", () => {
      basarili(
        "docker",
        [
          "run",
          "-d",
          "--name",
          container,
          "--network",
          ag,
          "-e",
          "POSTGRES_PASSWORD=test",
          "-e",
          "POSTGRES_USER=test",
          "-e",
          "POSTGRES_DB=test",
          PG_IMGE,
        ],
        { timeout: 300000 }
      );
    });

    await okAsync("b3 postgres hazir (pg_isready, en fazla 60 sn)", async () => {
      const baslangic = Date.now();
      let son = null;
      while (Date.now() - baslangic < 60000) {
        son = calistir("docker", ["exec", container, "pg_isready", "-U", "test", "-d", "test"], {
          timeout: 15000,
        });
        if (!son.error && son.status === 0) return;
        await bekle(1000);
      }
      throw new Error(
        "postgres 60 sn icinde hazir olmadi\n" +
          kirp((son && (son.stdout || "") + "\n" + (son.stderr || "")) || "", 20)
      );
    });

    ok("c1 migration'lar bos veritabanina uygulandi", () => {
      const r = goDocker(url, ag, "go run ./cmd/migrate", 600000);
      if (r.error || r.status !== 0) {
        throw new Error("migration calistirilamadi\n" + komutHatasi("docker", ["run", GO_IMGE], r));
      }
    });

    ok("c2 ya- oneki olmayan tablo yok (goose_db_version haric)", () => {
      const satirlar = psql(
        container,
        "SELECT tablename FROM pg_tables WHERE schemaname = 'public' " +
          "AND tablename <> 'goose_db_version' AND tablename NOT LIKE 'ya-%' ORDER BY tablename"
      )
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean);
      assert.deepStrictEqual(satirlar, [], "oneksiz tablolar: " + satirlar.join(", "));
    });

    ok("c3 ya-qo-users ve beklenen tum tablolar var", () => {
      const satirlar = new Set(
        psql(container, "SELECT tablename FROM pg_tables WHERE schemaname = 'public'")
          .split(/\r?\n/)
          .map((s) => s.trim())
          .filter(Boolean)
      );
      for (const tablo of BEKLENEN_TABLOLAR) {
        assert.ok(satirlar.has(tablo), tablo + " tablosu yok");
      }
      assert.ok(!satirlar.has("users"), "eski users tablosu hala var");
    });

    ok("c4 00009 down/up Go testleri gecti", () => {
      const komut =
        "go test -p 1 -count=1 -run 'TestMigrationDownUp|TestMigrationOdemeTekrar' ./internal/store";
      const r = goDocker(url, ag, komut, 900000);
      if (r.error || r.status !== 0) {
        throw new Error("migration down/up testi basarisiz\n" + komutHatasi("docker", ["run", GO_IMGE], r));
      }
    });

    ok("c5 X-DDO-Node ve /_hz/45a6 Go testleri gecti", () => {
      const komut = "go test -count=1 -run 'TestDdoNodeHeader|TestHzEndpoint' ./internal/httpx";
      const r = goDocker(url, ag, komut, 900000);
      if (r.error || r.status !== 0) {
        throw new Error("HTTP baslik/hz testi basarisiz\n" + komutHatasi("docker", ["run", GO_IMGE], r));
      }
    });

    temizle(container, ag);

    ok("d1 faz26 konteyneri kalmadi", () => {
      const r = basarili("docker", ["ps", "-a", "--format", "{{.Names}}"], { timeout: 30000 });
      const adlar = r.stdout.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      assert.ok(!adlar.includes(container), "konteyner hala var: " + container);
    });

    ok("d2 faz26 docker agi kalmadi", () => {
      const r = basarili("docker", ["network", "ls", "--format", "{{.Name}}"], { timeout: 30000 });
      const adlar = r.stdout.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      assert.ok(!adlar.includes(ag), "ag hala var: " + ag);
    });
  } catch (e) {
    hata = e;
  } finally {
    temizle(container, ag);
  }

  if (hata) {
    console.error("\nHATA: " + hata.message);
    process.exit(1);
  }

  console.log(`\nfaz26: ${n} kontrol gecti`);
}

main().catch((e) => {
  console.error("yakalanmamis hata:", e);
  process.exit(1);
});
