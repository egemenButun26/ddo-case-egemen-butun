// test/faz18.js — abonelik faturalama ve mutabakat (00007) gercek PostgreSQL 16 testleri.
//   a) gecici docker agi + postgres container (host'a PORT ACILMAZ)
//   b) golang container icinde `go test -p 1` (TEST_DATABASE_URL ile, atlama yasak)
//   c) beklenen testler PASS, SKIP yok; billing domain kapsami >= %85
//   d) her durumda container/ag temizligi
// Yalnizca Node standart kutuphanesi kullanilir. Komutlar shell'siz spawnSync ile calisir.
"use strict";
const assert = require("assert");
const path = require("path");
const { spawnSync } = require("child_process");

const PAKETLER = [
  "./internal/domain/billing/...",
  "./internal/store/...",
  "./internal/service/billingsvc/...",
];
const BEKLENEN_TESTLER = [
  "TestProrate",
  "TestCancelCredit",
  "TestPlanChange",
  "TestDunningSchedule",
  "TestSubscriptionTransitions",
  "TestReconcile",
  "TestReconcileTemiz",
  "TestBillingSchema",
  "TestMigrationAbonelikTekrar",
  "TestSubscribeCancel",
  "TestChangePlan",
  "TestRunPeriodEnd",
  "TestRunPeriodEndConcurrent",
  "TestDunning",
  "TestReconcileDay",
];
const KAPSAM_PAKETI = "./internal/domain/billing/...";
const ASGARI_KAPSAM = 85;

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
      "Docker bulunamadi ya da calismiyor; faz18 testi Docker olmadan dogrulanamaz.\n" +
        kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20)
    );
  }
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

async function main() {
  console.log("== faz18: abonelik faturalama ve mutabakat, gercek PostgreSQL 16");
  dockerKontrol();

  const sonek = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const ag = `parkakil-faz18-${sonek}`;
  const container = `parkakil-faz18-pg-${sonek}`;
  const url = `postgres://test:test@${container}:5432/test?sslmode=disable`;

  let hata = null;
  try {
    ok("a1 gecici docker agi olusturuldu", () => {
      basarili("docker", ["network", "create", ag], { timeout: 60000 });
    });

    ok("a2 postgres:16-alpine baslatildi (host'a port acilmadi)", () => {
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
      const durum = basarili("docker", ["inspect", "--format", "{{.State.Running}}", container], {
        timeout: 30000,
      });
      assert.strictEqual(durum.stdout.trim(), "true", "postgres konteyneri calismiyor");
    });

    await okAsync("a3 postgres hazir (pg_isready, en fazla 60 sn)", async () => {
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

    const komut = [`go test -p 1 -count=1 -v`, PAKETLER.join(" ")].join(" ");
    const test = calistir(
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
        `TEST_DATABASE_URL=${url}`,
        "-e",
        "PARKAKIL_REQUIRE_DB=1",
        GO_IMGE,
        "sh",
        "-c",
        komut,
      ],
      { timeout: 900000 }
    );

    const cikti = `${test.stdout || ""}\n${test.stderr || ""}`;
    console.log(kirp(cikti, 180));

    ok("b1 go test -p 1 cikis kodu 0", () => {
      if (test.error || test.status !== 0) {
        throw new Error(
          "entegrasyon testleri basarisiz\n" + komutHatasi("docker", ["run", GO_IMGE], test)
        );
      }
    });

    ok("b2 ciktida '--- SKIP' yok (atlama yasak)", () => {
      assert.ok(!/---\s*SKIP/.test(cikti), "atlanan test var:\n" + kirp(cikti, 40));
    });

    for (const ad of BEKLENEN_TESTLER) {
      ok(`b3 "${ad}" PASS`, () => {
        const desen = new RegExp(`^\\s*--- PASS: ${ad}(\\s|$)`, "m");
        assert.match(cikti, desen, ad + " icin '--- PASS' satiri yok");
      });
    }

    const kapsamKomut = `go test -count=1 -cover ${KAPSAM_PAKETI}`;
    const kapsam = calistir(
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
        kapsamKomut,
      ],
      { timeout: 600000 }
    );
    const kapsamCikti = `${kapsam.stdout || ""}\n${kapsam.stderr || ""}`;
    ok("c1 billing domain testleri kapsam olcumuyle gecti", () => {
      if (kapsam.error || kapsam.status !== 0) {
        throw new Error("kapsam testi basarisiz\n" + komutHatasi("docker", ["run", GO_IMGE], kapsam));
      }
    });
    ok(`c2 billing domain kapsami >= %${ASGARI_KAPSAM}`, () => {
      const m = kapsamCikti.match(/coverage:\s*([0-9]+(?:\.[0-9]+)?)%/);
      assert.ok(m, "kapsam yuzdesi okunamadi:\n" + kirp(kapsamCikti, 20));
      const yuzde = parseFloat(m[1]);
      assert.ok(
        yuzde >= ASGARI_KAPSAM,
        `billing domain kapsami %${yuzde}, en az %${ASGARI_KAPSAM} beklenirdi`
      );
    });

    temizle(container, ag);

    ok("d1 faz18 konteyneri kalmadi", () => {
      const r = basarili("docker", ["ps", "-a", "--format", "{{.Names}}"], { timeout: 30000 });
      const adlar = r.stdout.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
      assert.ok(!adlar.includes(container), "konteyner hala var: " + container);
    });

    ok("d2 faz18 docker agi kalmadi", () => {
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

  console.log(`\nfaz18: ${n} kontrol gecti`);
}

main().catch((e) => {
  console.error("yakalanmamis hata:", e);
  process.exit(1);
});
