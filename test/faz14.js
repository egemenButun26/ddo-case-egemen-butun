// test/faz14.js — Faz 1-B React (Vite) frontend iskeleti.
//   1) web/ bagimliliklari npm ci ile kurulur
//   2) typecheck, vitest ve uretim derlemesi gecer
//   3) Docker imaji derlenir, root olmayan kullaniciyla calisir ve dist icerir
// Sonunda kendi imaji silinir (disk sinirli).
// Docker yoksa test ATLANMAZ; acik mesajla basarisiz olur.
// Yalnizca Node standart kutuphanesi kullanilir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const { spawnSync } = require("child_process");

const KOK = path.join(__dirname, "..");
const WEB = path.join(KOK, "web");
const IMaj = "parkakil-web-faz14:local";
const NPM = process.platform === "win32" ? process.env.ComSpec || "cmd.exe" : "npm";
const BEKLENEN_ASGARI_TEST = 15;

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

function imajKaldir() {
  const r = calistir("docker", ["image", "rm", "-f", IMaj], { timeout: 60000 });
  if (r.error) {
    console.warn("uyari: imaj silinemedi: " + r.error.message);
  } else if (r.status !== 0 && !/No such image/i.test(String(r.stderr))) {
    console.warn("uyari: imaj silinemedi:\n" + kirp(r.stderr, 10));
  }
}

const docker = calistir("docker", ["info", "--format", "{{.ServerVersion}}"], {
  timeout: 30000,
});
if (docker.error || docker.status !== 0) {
  console.error(
    "Docker bulunamadi ya da calismiyor; faz14 testi Docker olmadan dogrulanamaz.\n" +
      kirp((docker.stdout || "") + "\n" + (docker.stderr || ""))
  );
  process.exit(1);
}

let testSayisi = 0;

try {
  ok("1 web/package-lock.json var (npm ci icin)", () => {
    assert.ok(
      fs.existsSync(path.join(WEB, "package-lock.json")),
      "web/package-lock.json yok"
    );
  });

  ok("2 npm ci gecti", () => {
    const r = npm(["ci", "--no-audit", "--no-fund"], 600000);
    assert.strictEqual(r.status, 0, komutHatasi("npm ci", r));
  });

  ok("3 npm run typecheck gecti", () => {
    const r = npm(["run", "typecheck"]);
    assert.strictEqual(r.status, 0, komutHatasi("npm run typecheck", r));
  });

  ok(`4 npm test gecti ve en az ${BEKLENEN_ASGARI_TEST} test kostu`, () => {
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

  ok("5 npm run build gecti ve web/dist/index.html olustu", () => {
    const r = npm(["run", "build"]);
    assert.strictEqual(r.status, 0, komutHatasi("npm run build", r));
    const indeks = path.join(WEB, "dist", "index.html");
    assert.ok(fs.existsSync(indeks), "web/dist/index.html yok");
    assert.ok(fs.statSync(indeks).size > 0, "web/dist/index.html bos");
  });

  ok("6 docker build -f deploy/web.Dockerfile . gecti", () => {
    const r = calistir(
      "docker",
      ["build", "-f", "deploy/web.Dockerfile", "-t", IMaj, "."],
      { timeout: 600000 }
    );
    assert.strictEqual(r.status, 0, komutHatasi("docker build", r));
  });

  ok("7 imaj root olmayan kullaniciyla calisiyor ve dist iceriyor", () => {
    const incele = calistir("docker", [
      "image",
      "inspect",
      "--format",
      "{{.Config.User}}",
      IMaj,
    ]);
    assert.strictEqual(incele.status, 0, komutHatasi("docker image inspect", incele));
    const kullanici = incele.stdout.trim();
    assert.notStrictEqual(kullanici, "", "imajda USER bos");
    assert.ok(
      kullanici !== "root" && kullanici !== "0" && !kullanici.startsWith("0:"),
      "imaj root kullanici: " + kullanici
    );

    const calistirR = calistir("docker", [
      "run",
      "--rm",
      "--entrypoint",
      "sh",
      IMaj,
      "-c",
      "id -u && test -f /usr/share/nginx/html/index.html && echo dist-var",
    ]);
    assert.strictEqual(calistirR.status, 0, komutHatasi("docker run", calistirR));
    const uid = calistirR.stdout.trim().split(/\r?\n/)[0];
    assert.ok(uid !== "0", "konteyner root olarak calisti (uid 0)");
    assert.match(calistirR.stdout, /dist-var/, "imajda index.html yok");
  });

  console.log(`\nfaz14: ${n} kontrol gecti (${testSayisi} vitest testi)`);
} catch (e) {
  console.error("\nHATA: " + (e && e.message ? e.message : e));
  process.exitCode = 1;
} finally {
  imajKaldir();
}
