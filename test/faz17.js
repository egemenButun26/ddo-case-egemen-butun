// test/faz17.js — tohumlu plaka simulatoru testleri.
//   a) Docker'da go test + kapsam (internal/simulator, cmd/simulator)
//   b) simulator dump: ayni tohum ayni cikti, farkli tohum farkli cikti
// Yalnizca Node standart kutuphanesi kullanilir. Docker yoksa test atlanmaz.
"use strict";
const assert = require("assert");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const GO_IMGE = "golang:1.23-alpine";
const GO_PAKETLER = ["./internal/simulator/...", "./cmd/simulator/..."].join(" ");

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

function dockerKontrol() {
  const r = calistir("docker", ["info", "--format", "{{.ServerVersion}}"], { timeout: 30000 });
  if (r.error || r.status !== 0) {
    throw new Error(
      "Docker bulunamadi ya da calismiyor; faz17 testi Docker olmadan dogrulanamaz.\n" +
        kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20)
    );
  }
}

function execContainer(container, args, secenek) {
  return basarili("docker", ["exec", container].concat(args), secenek);
}

function execShell(container, komut, secenek) {
  return basarili("docker", ["exec", container, "sh", "-c", komut], secenek);
}

function temizle(container) {
  if (!container) return;
  const r = calistir("docker", ["rm", "-f", container], { timeout: 120000 });
  if (r.error || r.status !== 0) {
    console.error("uyari: konteyner silinemedi:\n" + kirp((r.stdout || "") + "\n" + (r.stderr || ""), 20));
  }
}

function dumpCalistir(container, seed, adet) {
  const r = execContainer(
    container,
    ["/tmp/simulator", "dump", "--seed", String(seed), "--events", String(adet), "--out", "-"],
    { timeout: 120000 }
  );
  return r.stdout;
}

async function main() {
  console.log("== faz17: tohumlu plaka simulatoru");
  dockerKontrol();

  const sonek = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const container = `parkakil-faz17-${sonek}`;
  let hata = null;

  try {
    const imaj = calistir("docker", ["image", "inspect", GO_IMGE], { timeout: 30000 });
    if (imaj.error || imaj.status !== 0) {
      basarili("docker", ["pull", GO_IMGE], { timeout: 600000 });
    }

    ok("a1 golang test konteyneri baslatildi", () => {
      basarili(
        "docker",
        [
          "run",
          "-d",
          "--name",
          container,
          "-v",
          `${KOK_MOUNT}:/src`,
          "-v",
          "parkakil-gomod:/go/pkg/mod",
          "-w",
          "/src",
          "-e",
          "GOFLAGS=-buildvcs=false",
          GO_IMGE,
          "sleep",
          "900",
        ],
        { timeout: 120000 }
      );
      const durum = basarili("docker", ["inspect", "--format", "{{.State.Running}}", container], {
        timeout: 30000,
      });
      assert.strictEqual(durum.stdout.trim(), "true", "test konteyneri calismiyor");
    });

    let testCikti = "";
    ok("a2 go test (simulator paketleri) geciyor", () => {
      const r = execShell(
        container,
        `go test -count=1 -coverprofile=/tmp/cover.out ${GO_PAKETLER}`,
        { timeout: 900000 }
      );
      testCikti = `${r.stdout || ""}\n${r.stderr || ""}`;
      assert.ok(!/FAIL/.test(testCikti), "go test FAIL:\n" + kirp(testCikti, 80));
      assert.match(testCikti, /ok\s+parkakil\/internal\/simulator/, "internal/simulator testi kosmadi");
      assert.match(testCikti, /ok\s+parkakil\/cmd\/simulator/, "cmd/simulator testi kosmadi");
    });

    ok("a3 kapsam >= %70", () => {
      const r = execShell(container, "go tool cover -func=/tmp/cover.out", { timeout: 120000 });
      const eslesme = String(r.stdout || "").match(/total:\s+\(statements\)\s+([0-9.]+)%/);
      assert.ok(eslesme, "kapsam satiri bulunamadi:\n" + kirp(r.stdout, 20));
      const kapsam = Number(eslesme[1]);
      assert.ok(kapsam >= 70, `kapsam %${kapsam}, en az %70 olmali`);
    });

    ok("a4 simulator ikilisi derlendi", () => {
      execShell(container, "go build -o /tmp/simulator ./cmd/simulator", { timeout: 300000 });
      execContainer(container, ["/tmp/simulator", "dump", "--events", "1", "--out", "-"], {
        timeout: 120000,
      });
    });

    let ilk = "";
    let ikinci = "";
    let farkli = "";
    ok("b1 ayni tohum iki kez ayni dump ciktisini veriyor", () => {
      ilk = dumpCalistir(container, 20261026, 200);
      ikinci = dumpCalistir(container, 20261026, 200);
      assert.strictEqual(ikinci, ilk, "ayni tohum farkli cikti verdi");
    });

    ok("b2 dump 200 satir ve her satir gecerli JSON", () => {
      const satirlar = ilk.trim().split(/\r?\n/);
      assert.strictEqual(satirlar.length, 200, "satir sayisi 200 degil: " + satirlar.length);
      for (const satir of satirlar) {
        const kayit = JSON.parse(satir);
        assert.ok(kayit.camera_event_id, "camera_event_id yok");
        assert.ok(kayit.camera_id, "camera_id yok");
        assert.ok(kayit.lot_id, "lot_id yok");
        assert.ok(kayit.captured_at, "captured_at yok");
      }
    });

    ok("b3 farkli tohum farkli dump ciktisi veriyor", () => {
      farkli = dumpCalistir(container, 20261027, 200);
      assert.notStrictEqual(farkli, ilk, "farkli tohum ayni ciktiyi verdi");
    });
  } catch (e) {
    hata = e;
  } finally {
    temizle(container);
  }

  if (hata) {
    console.error("\nHATA: " + hata.message);
    process.exit(1);
  }

  console.log(`\nfaz17: ${n} kontrol gecti`);
}

main().catch((e) => {
  console.error("yakalanmamis hata:", e);
  process.exit(1);
});
