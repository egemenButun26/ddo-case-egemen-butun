// test/faz2.js — uctan uca kabul testi: Docker Compose altyapisi, API, web, migrasyon.
//   a) statik kontroller | b) Go (docker) | c) compose + canli kontroller
//   c) icinde: saglik, 404, web, migrasyon, imaj kullanicisi, dusus, toparlanma, kapanis
// Docker yoksa test ATLANMAZ; acik mesajla basarisiz olur.
// Yalnizca Node standart kutuphanesi kullanilir. Komutlar shell'siz spawnSync ile calisir.
"use strict";
const assert = require("assert");
const fs = require("fs");
const path = require("path");
const http = require("http");
const { spawnSync } = require("child_process");

process.on("unhandledRejection", (e) => {
  console.error("yakalanmamis hata:", e);
  process.exitCode = 1;
});

const BEKLENEN_KONTROL = 21;

const KOK = path.join(__dirname, "..");
const KOK_MOUNT = KOK.replace(/\\/g, "/");
const PROJE = "parkakil-faz2-test";
const API_PORT = 18080;
const WEB_PORT = 13000;
const API_URL = `http://127.0.0.1:${API_PORT}`;
const WEB_URL = `http://127.0.0.1:${WEB_PORT}`;
const COMPOSE = ["compose", "-p", PROJE, "--env-file", ".env.example"];

let n = 0;
const ok = (ad, f) => {
  f();
  n++;
  console.log("  ok  " + ad);
};
const okAsync = async (ad, f) => {
  await f();
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
    env: s.env || process.env,
    maxBuffer: 32 * 1024 * 1024,
  });
}

function kirp(metin, satirSayisi) {
  const satirlar = String(metin || "").trim().split(/\r?\n/);
  return satirlar.slice(-(satirSayisi || 60)).join("\n");
}

function dockerDurumu() {
  const r = calistir(
    "docker",
    COMPOSE.concat(["ps", "-a"]),
    { timeout: 30000, env: composeEnv }
  );
  if (r.error) return "docker compose ps -a calistirilamadi: " + r.error.message;
  return kirp((r.stdout || "") + "\n" + (r.stderr || ""), 40);
}

function komutHatasi(komut, args, r) {
  const parcalar = [];
  if (r.error) {
    parcalar.push(`${komut} ${args[0] || ""} calistirilamadi: ${r.error.message}`);
  } else {
    parcalar.push(`${komut} ${args.slice(0, 4).join(" ")} basarisiz (kod ${r.status})`);
  }
  if ((r.stderr || "").trim()) {
    parcalar.push("stderr (son 20 satir):\n" + kirp(r.stderr, 20));
  }
  if ((r.stdout || "").trim()) {
    parcalar.push("stdout (son 60 satir):\n" + kirp(r.stdout, 60));
  }
  if (komut === "docker") {
    parcalar.push("docker compose ps -a:\n" + dockerDurumu());
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

const composeEnv = Object.assign({}, process.env, {
  API_HOST_PORT: String(API_PORT),
  WEB_HOST_PORT: String(WEB_PORT),
});

function compose(args, secenek) {
  const s = Object.assign({ env: composeEnv }, secenek || {});
  return basarili("docker", COMPOSE.concat(args), s);
}

function envDegeri(anahtar) {
  const metin = fs.readFileSync(path.join(KOK, ".env.example"), "utf8");
  for (const satir of metin.split(/\r?\n/)) {
    const m = satir.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)\s*$/);
    if (m && m[1] === anahtar) {
      return m[2].replace(/^["']|["']$/g, "");
    }
  }
  return "";
}

function servisBloklari(metin) {
  const satirlar = metin.split(/\r?\n/);
  const sonuc = {};
  let servislerde = false;
  let aktif = null;
  for (const satir of satirlar) {
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
    const m = satir.match(/^  ([A-Za-z0-9_.-]+):\s*$/);
    if (m) {
      aktif = m[1];
      sonuc[aktif] = [];
      continue;
    }
    if (aktif !== null) sonuc[aktif].push(satir);
  }
  return sonuc;
}

function istek(url) {
  return new Promise((resolve, reject) => {
    const u = new URL(url);
    const req = http.get(
      {
        hostname: u.hostname,
        port: u.port,
        path: u.pathname + u.search,
        timeout: 10000,
        headers: { Accept: "*/*", Connection: "close" },
      },
      (res) => {
        let govde = "";
        res.setEncoding("utf8");
        res.on("data", (d) => {
          govde += d;
        });
        res.on("end", () => {
          resolve({ durum: res.statusCode, basliklar: res.headers, govde });
        });
      }
    );
    req.on("timeout", () => {
      req.destroy(new Error("istek zaman asimi"));
    });
    req.on("error", reject);
  });
}

function bekle(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

async function statikKontroller() {
  const composeYolu = path.join(KOK, "docker-compose.yml");

  ok("a1 docker-compose.yml var, name: parkakil, container_name yok", () => {
    assert.ok(fs.existsSync(composeYolu), "docker-compose.yml yok");
    const metin = fs.readFileSync(composeYolu, "utf8");
    assert.match(metin, /^name:\s*parkakil\s*$/m, "name: parkakil bulunamadi");
    assert.ok(!/container_name\s*:/.test(metin), "container_name kullanilmamali");
  });

  const metin = fs.readFileSync(composeYolu, "utf8");
  const bloklar = servisBloklari(metin);

  ok("a2 postgres servisi host'a port acmiyor", () => {
    assert.ok(bloklar.postgres, "postgres servisi yok");
    assert.ok(
      !bloklar.postgres.some((s) => /^\s{4}ports\s*:/.test(s)),
      "postgres servisinde ports olmamali"
    );
  });

  ok("a3 migrate disindaki her serviste healthcheck var", () => {
    const adlar = Object.keys(bloklar);
    assert.ok(adlar.length >= 4, "servisler eksik: " + adlar.join(", "));
    for (const ad of adlar) {
      if (ad === "migrate" || ad === "seed") continue;
      assert.ok(
        bloklar[ad].some((s) => /^\s{4}healthcheck\s*:/.test(s)),
        ad + " servisinde healthcheck yok"
      );
    }
  });

  ok("a4 api depends_on service_healthy + service_completed_successfully", () => {
    const blok = (bloklar.api || []).join("\n");
    assert.ok(blok, "api servisi yok");
    assert.match(blok, /service_healthy/, "service_healthy yok");
    assert.match(blok, /service_completed_successfully/, "service_completed_successfully yok");
  });

  ok("a5 .env.example var ve git tarafindan gizlenmiyor", () => {
    assert.ok(fs.existsSync(path.join(KOK, ".env.example")), ".env.example yok");
    const r = calistir("git", ["check-ignore", "-q", ".env.example"]);
    if (r.error) throw new Error("git calistirilamadi: " + r.error.message);
    assert.strictEqual(r.status, 1, "git check-ignore .env.example gizli oldugunu soyledi");
  });

  ok("a6 web dosyalari hazir (AbortController, /api/health)", () => {
    const html = path.join(KOK, "web", "index.html");
    const css = path.join(KOK, "web", "styles.css");
    const js = path.join(KOK, "web", "app.js");
    for (const dosya of [html, css, js]) {
      assert.ok(fs.existsSync(dosya), dosya + " yok");
    }
    const app = fs.readFileSync(js, "utf8");
    assert.match(app, /AbortController/, "app.js AbortController kullanmali");
    assert.match(app, /\/api\/health/, "app.js /api/health cagirmali");
  });

  ok("a7 api.Dockerfile ENTRYPOINT icermiyor, CMD ile basliyor", () => {
    const dockerfile = path.join(KOK, "deploy", "api.Dockerfile");
    assert.ok(fs.existsSync(dockerfile), "deploy/api.Dockerfile yok");
    const icerik = fs.readFileSync(dockerfile, "utf8");
    assert.ok(
      !/^\s*ENTRYPOINT\b/im.test(icerik),
      "api.Dockerfile ENTRYPOINT icermemeli (compose command argv'sini bozar)"
    );
    assert.match(icerik, /^\s*CMD\s+\[\s*"\/app\/api"\s*\]/m, 'api.Dockerfile CMD ["/app/api"] icermeli');
  });
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

async function goKontrolleri() {
  const info = calistir("docker", ["info", "--format", "{{.ServerVersion}}"]);
  if (info.error || info.status !== 0) {
    throw new Error(
      "Docker bulunamadi ya da calismiyor; faz2 testi Docker olmadan dogrulanamaz.\n" +
        kirp((info.stdout || "") + "\n" + (info.stderr || ""))
    );
  }

  const cekme = calistir("docker", ["pull", "golang:1.23-alpine"], { timeout: 600000 });
  if (cekme.error || cekme.status !== 0) {
    throw new Error("golang:1.23-alpine imaji cekilemedi:\n" + kirp(cekme.stderr));
  }

  ok("b1 gofmt -l . bos", () => {
    const r = goBasarili("gofmt -l .");
    assert.strictEqual(r.stdout.trim(), "", "gofmt gereken dosyalar:\n" + r.stdout);
  });

  ok("b2 go vet ./... geciyor", () => {
    goBasarili("go vet ./...");
  });

  ok("b3 go test ./... geciyor", () => {
    goBasarili("go test ./...");
  });
}

async function canliKontrolleri() {
  await okAsync("c2 GET /health -> 200, database ok, X-Request-ID", async () => {
    const y = await istek(`${API_URL}/health`);
    assert.strictEqual(y.durum, 200, "govde: " + y.govde);
    let j;
    try {
      j = JSON.parse(y.govde);
    } catch (e) {
      throw new Error("yanit JSON degil: " + y.govde);
    }
    assert.strictEqual(j.checks && j.checks.database, "ok", "checks.database ok degil");
    assert.ok(y.basliklar["x-request-id"], "X-Request-ID basligi yok");
  });

  await okAsync("c3 GET /yok -> 404 not_found", async () => {
    const y = await istek(`${API_URL}/yok`);
    assert.strictEqual(y.durum, 404, "govde: " + y.govde);
    const j = JSON.parse(y.govde);
    assert.strictEqual(j.error && j.error.code, "not_found");
  });

  await okAsync("c4 GET web / -> 200 text/html", async () => {
    const y = await istek(`${WEB_URL}/`);
    assert.strictEqual(y.durum, 200, "govde: " + y.govde);
    assert.match(String(y.basliklar["content-type"] || ""), /text\/html/);
  });

  await okAsync("c5 GET web /api/health -> 200", async () => {
    const y = await istek(`${WEB_URL}/api/health`);
    assert.strictEqual(y.durum, 200, "govde: " + y.govde);
    const j = JSON.parse(y.govde);
    assert.strictEqual(j.status, "ok");
  });

  ok("c6 migrasyon uygulanmis ve app_meta tablosu var", () => {
    const kullanici = envDegeri("POSTGRES_USER") || "parkakil";
    const surum = compose([
      "exec",
      "-T",
      "postgres",
      "psql",
      "-U",
      kullanici,
      "-tAc",
      "SELECT count(*) FROM goose_db_version WHERE is_applied",
    ]);
    assert.ok(Number(surum.stdout.trim()) >= 1, "uygulanmis surum yok: " + surum.stdout);
    const tablo = compose([
      "exec",
      "-T",
      "postgres",
      "psql",
      "-U",
      kullanici,
      "-tAc",
      "SELECT to_regclass('public.app_meta')",
    ]);
    assert.strictEqual(tablo.stdout.trim(), "app_meta", "app_meta tablosu yok");
  });

  ok("c7 api ve web imajlari root kullanici ile calismiyor", () => {
    for (const servis of ["api", "web"]) {
      const imaj = compose(["images", "-q", servis]).stdout
        .split(/\r?\n/)
        .map((s) => s.trim())
        .filter(Boolean)[0];
      assert.ok(imaj, servis + " imaj id bulunamadi");
      const r = calistir("docker", ["image", "inspect", "--format", "{{.Config.User}}", imaj]);
      assert.strictEqual(r.status, 0, servis + " imaji incelenemedi: " + r.stderr);
      const kullanici = r.stdout.trim();
      assert.notStrictEqual(kullanici, "", servis + " imajinda USER bos");
      assert.ok(
        kullanici !== "root" && kullanici !== "0" && !kullanici.startsWith("0:"),
        servis + " imaji root kullanicida: " + kullanici
      );
    }
  });
}

async function dususVeToparlanma() {
  compose(["stop", "postgres"], { timeout: 60000 });

  await okAsync("c8 postgres durunca /health 15 sn icinde 503 unavailable", async () => {
    const baslangic = Date.now();
    let son = null;
    while (Date.now() - baslangic < 15000) {
      try {
        son = await istek(`${API_URL}/health`);
      } catch (e) {
        son = null;
      }
      if (son && son.durum === 503) break;
      await bekle(500);
    }
    assert.ok(son, "15 sn icinde yanit alinamadi");
    assert.strictEqual(son.durum, 503, "15 sn icinde 503 alinamadi (son " + son.durum + ")");
    const j = JSON.parse(son.govde);
    assert.strictEqual(j.error && j.error.code, "unavailable");
  });

  compose(["start", "postgres"], { timeout: 60000 });

  await okAsync("c9 postgres donunce /health 30 sn icinde tekrar 200", async () => {
    const baslangic = Date.now();
    let son = null;
    while (Date.now() - baslangic < 30000) {
      try {
        son = await istek(`${API_URL}/health`);
      } catch (e) {
        son = null;
      }
      if (son && son.durum === 200) break;
      await bekle(500);
    }
    assert.ok(son, "30 sn icinde yanit alinamadi");
    assert.strictEqual(son.durum, 200, "30 sn icinde 200 alinamadi (son " + son.durum + ")");
  });
}

async function kapanisKontrolu() {
  const konteyner = compose(["ps", "-q", "api"]).stdout
    .split(/\r?\n/)
    .map((s) => s.trim())
    .filter(Boolean)[0];
  assert.ok(konteyner, "api konteyneri bulunamadi");

  compose(["stop", "api"], { timeout: 60000 });

  ok("c10 api duzgun kapandi (log + cikis kodu 0)", () => {
    const kod = calistir("docker", ["inspect", "--format", "{{.State.ExitCode}}", konteyner]);
    assert.strictEqual(kod.status, 0, "konteyner incelenemedi: " + kod.stderr);
    assert.strictEqual(kod.stdout.trim(), "0", "api cikis kodu 0 degil: " + kod.stdout.trim());
    const log = compose(["logs", "--no-color", "api"]).stdout;
    assert.match(log, /kapanis sinyali alindi/, "kapanis sinyali logu yok");
    assert.match(log, /kapandi/, "kapanis tamam logu yok");
  });
}

function temizle() {
  const r = calistir("docker", COMPOSE.concat(["down", "-v", "--remove-orphans"]), {
    timeout: 180000,
    env: composeEnv,
  });
  if (r.error || r.status !== 0) {
    console.error("uyari: temizlik tamamlanamadi:\n" + kirp((r.stdout || "") + "\n" + (r.stderr || "")));
  }
}

let sonAdim = "baslangic";

async function adim(ad, f) {
  sonAdim = ad;
  console.log("\n== " + ad);
  await f();
}

async function main() {
  let hata = null;
  try {
    await adim("a) statik kontroller", statikKontroller);
    await adim("b) go kontrolleri", goKontrolleri);
    await adim("c) imaj derleme ve migrate ikilisi", async () => {
      compose(["build"], { timeout: 360000 });
      ok("c0 imajdaki /app/migrate migrate ikilisini calistiriyor (api degil)", () => {
        const args = ["run", "--rm", "parkakil-api:local", "/app/migrate"];
        const r = calistir("docker", args, { timeout: 120000 });
        if (r.error || r.status !== 1) {
          throw new Error(
            "DATABASE_URL olmadan /app/migrate cikis kodu 1 vermeliydi\n" + komutHatasi("docker", args, r)
          );
        }
        const cikti = (r.stdout || "") + "\n" + (r.stderr || "");
        assert.match(
          cikti,
          /migrasyon/,
          "cikti 'migrasyon' icermeli; imajda api ikilisi calisiyor olabilir"
        );
      });
    });
    await adim("c) compose kaldirma", async () => {
      compose(["up", "-d", "--wait", "--wait-timeout", "300"], { timeout: 360000 });
      n++;
      console.log("  ok  c1 compose ayakta (--wait)");
    });
    await adim("c) canli kontroller", canliKontrolleri);
    await adim("c) dusus ve toparlanma", dususVeToparlanma);
    await adim("c) duzgun kapanis", kapanisKontrolu);
  } catch (e) {
    hata = e;
  } finally {
    temizle();
  }

  if (hata) {
    console.error("\nHATA (" + sonAdim + "): " + hata.message);
    process.exit(1);
  }

  console.log(`\nfaz2: ${n} kontrol gecti`);
  if (n !== BEKLENEN_KONTROL) {
    console.error(`faz2: beklenen ${BEKLENEN_KONTROL} kontrol, gecen ${n}`);
    process.exitCode = 1;
  }
}

main();
