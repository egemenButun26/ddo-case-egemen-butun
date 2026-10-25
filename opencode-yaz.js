// opencode-yaz.js — sefin brifini bir git calisma kopyasina koyar ve orada
// opencode (varsayilan DeepSeek) ile kodlayici modeli calistirir. Sonra ne
// degistigini raporlar. "Plan Claude'da, kod DeepSeek'te" akisinin aracidir.
//
// Kullanim:
//   node opencode-yaz.js --brif <brif.md> --worktree <klasor>
//        [--model <saglayici/model>] [--sure-dk <n>] [--dene]
//
// Betik ASLA commit, push, stash, reset yapmaz. Gizli dosyalar (.env,
// auth.json vb.) okunmaz, ortam degiskenleri loga yazilmaz.

"use strict";

const fs = require("fs");
const path = require("path");
const { spawn, spawnSync } = require("child_process");

const SABIT_ISTEM = "Read OPENCODE-BRIF.md in this folder and complete the task it describes. Do not modify or delete OPENCODE-BRIF.md. Do not run git commit, git push, git reset, git rebase or git stash.";
const VARSAYILAN_MODEL = "opencode-go/deepseek-v4.1-flash";
const VARSAYILAN_SURE_DK = 20;

const KULLANIM = [
  "Kullanim:",
  "  node opencode-yaz.js --brif <brif.md> --worktree <klasor> [--model <saglayici/model>] [--sure-dk <n>] [--dene]",
  "",
  "  --brif      Zorunlu. Var olan brif dosyasi.",
  "  --worktree  Zorunlu. git worktree ile acilmis ek calisma kopyasi.",
  "  --model     Varsayilan: " + VARSAYILAN_MODEL,
  "  --sure-dk   opencode zaman asimi, dakika. Varsayilan: " + VARSAYILAN_SURE_DK,
  "  --dene      Kuru calisma: brifi kopyalar, komutu basar, opencode'u cagirmaz.",
].join("\n");

function zamanDamgasi(t) {
  const d = t || new Date();
  const p = n => String(n).padStart(2, "0");
  return d.getFullYear() + p(d.getMonth() + 1) + p(d.getDate()) + "-" +
    p(d.getHours()) + p(d.getMinutes()) + p(d.getSeconds());
}

function argumanAyir(argv) {
  const sonuc = {
    brif: null,
    worktree: null,
    model: VARSAYILAN_MODEL,
    sureDk: VARSAYILAN_SURE_DK,
    dene: false,
  };
  const degerli = ["--brif", "--worktree", "--model", "--sure-dk"];

  for (let i = 0; i < argv.length; i++) {
    const b = argv[i];
    if (b === "--dene") { sonuc.dene = true; continue; }
    if (degerli.indexOf(b) === -1) return { hata: "bilinmeyen bayrak: " + b };

    const deger = argv[i + 1];
    if (deger === undefined || deger.startsWith("--")) return { hata: b + " icin deger yok" };
    i++;

    if (b === "--brif") sonuc.brif = deger;
    else if (b === "--worktree") sonuc.worktree = deger;
    else if (b === "--model") sonuc.model = deger;
    else {
      const n = Number(deger);
      if (!Number.isFinite(n) || n <= 0) return { hata: "--sure-dk pozitif sayi olmali: " + deger };
      sonuc.sureDk = n;
    }
  }

  if (!sonuc.brif) return { hata: "--brif zorunlu" };
  if (!sonuc.worktree) return { hata: "--worktree zorunlu" };

  let st;
  try { st = fs.statSync(sonuc.brif); } catch (e) { st = null; }
  if (!st || !st.isFile()) return { hata: "brif dosyasi yok: " + sonuc.brif };

  if (!/^[A-Za-z0-9][A-Za-z0-9._\/@:+~-]*$/.test(sonuc.model)) return { hata: "gecersiz model: " + sonuc.model };

  return sonuc;
}

function komutKur(ayar) {
  const model = (ayar && ayar.model) || VARSAYILAN_MODEL;
  return { komut: "opencode", argumanlar: ["run", "-m", model, SABIT_ISTEM] };
}

function gitCikti(yol, argumanlar) {
  const r = spawnSync("git", ["-C", yol].concat(argumanlar), { encoding: "utf8", windowsHide: true });
  if (r.error || r.status !== 0) return null;
  return String(r.stdout || "").trim();
}

function gercekYol(p) {
  try { return fs.realpathSync(p); } catch (e) { return path.resolve(p); }
}

function ayniYol(a, b) {
  const x = gercekYol(a), y = gercekYol(b);
  return process.platform === "win32" ? x.toLowerCase() === y.toLowerCase() : x === y;
}

function worktreeKontrol(yol) {
  const tam = path.resolve(yol);

  let st;
  try { st = fs.statSync(tam); } catch (e) { return { tamam: false, neden: "klasor yok: " + yol }; }
  if (!st.isDirectory()) return { tamam: false, neden: "klasor degil: " + yol };

  const ust = gitCikti(tam, ["rev-parse", "--show-toplevel"]);
  if (!ust) return { tamam: false, neden: "git calisma agaci degil: " + yol };
  if (!ayniYol(ust, tam)) {
    return { tamam: false, neden: "worktree koku verilmeli: " + tam + " (git koku: " + ust + ")" };
  }

  const gitDir = gitCikti(tam, ["rev-parse", "--git-dir"]);
  const ortakDir = gitCikti(tam, ["rev-parse", "--git-common-dir"]);
  if (!gitDir || !ortakDir) return { tamam: false, neden: "git bilgisi okunamadi: " + yol };

  const gd = path.isAbsolute(gitDir) ? gitDir : path.resolve(tam, gitDir);
  const od = path.isAbsolute(ortakDir) ? ortakDir : path.resolve(tam, ortakDir);
  if (ayniYol(gd, od)) {
    return { tamam: false, neden: "ana kopya: yalnizca git worktree ile acilmis ek kopyada calisilir" };
  }

  const dal = gitCikti(tam, ["rev-parse", "--abbrev-ref", "HEAD"]);
  if (!dal) return { tamam: false, neden: "gecerli dal okunamadi: " + yol };
  if (dal === "HEAD") return { tamam: false, neden: "ayrik HEAD (detached): bir dalda olmali" };
  if (dal === "main" || dal === "master") {
    return { tamam: false, neden: "main/master dalinda calisilmaz: " + dal };
  }

  return { tamam: true, dal };
}

function tirnakla(s) {
  return '"' + s + '"';
}

function kabukArguman(s) {
  return /\s/.test(s) ? tirnakla(s) : s;
}

function komutSatiri(komut, argumanlar) {
  return [komut].concat(argumanlar.map(kabukArguman)).join(" ");
}

function spawnSecenekleri(worktree) {
  // PWD'yi acikca vermezsek Git Bash'ten miras kalan PWD yuzunden opencode
  // proje klasorunu yanlis yerde arar. process.env yerinde degistirilmez.
  const env = Object.assign({}, process.env);
  env.PWD = path.resolve(worktree);

  return {
    cwd: worktree,
    env: env,
    shell: true,
    windowsHide: true,
    stdio: ["ignore", "pipe", "pipe"],
  };
}

function oldur(cocuk) {
  if (!cocuk || cocuk.killed) return;
  if (process.platform === "win32") {
    spawnSync("taskkill", ["/pid", String(cocuk.pid), "/T", "/F"], { stdio: "ignore", windowsHide: true });
  }
  try { cocuk.kill(); } catch (e) { /* zaten kapandi */ }
}

function ozetYaz(worktree, gecenSn, kod) {
  console.log("");
  console.log("gecen sure: " + gecenSn.toFixed(1) + " sn");
  console.log("opencode cikis kodu: " + kod);

  const durum = gitCikti(worktree, ["status", "--short"]);
  console.log("--- git status --short ---");
  console.log(durum === null ? "(okunamadi)" : (durum || "(temiz)"));

  const fark = gitCikti(worktree, ["diff", "--stat"]);
  console.log("--- git diff --stat ---");
  console.log(fark === null ? "(okunamadi)" : (fark || "(degisiklik yok)"));
}

function calistir(worktree, model, sureDk) {
  const k = komutKur({ model });
  const logYolu = path.join(worktree, "opencode-" + zamanDamgasi() + ".log");
  const log = fs.createWriteStream(logYolu, { flags: "w" });
  const bas = Date.now();

  return new Promise(resolve => {
    let bitti = false;
    let zamanAsimi = false;
    let cocuk;

    const logla = d => { try { log.write(d); } catch (e) { /* log kapandi */ } };
    const ekran = (akis, d) => { akis.write(d); logla(d); };

    const tamamla = kod => {
      const gecenSn = (Date.now() - bas) / 1000;
      log.end(() => resolve({ kod, gecenSn, logYolu }));
    };

    try {
      cocuk = spawn(komutSatiri(k.komut, k.argumanlar), spawnSecenekleri(worktree));
    } catch (e) {
      const m = "opencode baslatilamadi: " + e.message + "\n";
      process.stderr.write(m);
      logla(m);
      return tamamla(1);
    }

    const zaman = setTimeout(() => {
      zamanAsimi = true;
      const m = "\nzaman asimi: " + sureDk + " dk doldu, surec olduruluyor\n";
      process.stderr.write(m);
      logla(m);
      oldur(cocuk);
    }, sureDk * 60000);

    if (cocuk.stdout) cocuk.stdout.on("data", d => ekran(process.stdout, d));
    if (cocuk.stderr) cocuk.stderr.on("data", d => ekran(process.stderr, d));

    cocuk.on("error", e => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      const m = "opencode baslatilamadi: " + e.message + "\n";
      process.stderr.write(m);
      logla(m);
      tamamla(1);
    });

    cocuk.on("close", kod => {
      if (bitti) return;
      bitti = true;
      clearTimeout(zaman);
      if (zamanAsimi) return tamamla(4);
      tamamla(kod === null ? 1 : kod);
    });
  });
}

async function main() {
  const ayar = argumanAyir(process.argv.slice(2));
  if (ayar.hata) {
    console.error(ayar.hata);
    console.error("");
    console.error(KULLANIM);
    process.exitCode = 2;
    return;
  }

  const kontrol = worktreeKontrol(ayar.worktree);
  if (!kontrol.tamam) {
    console.error("✗ " + kontrol.neden);
    process.exitCode = 3;
    return;
  }

  const hedef = path.join(ayar.worktree, "OPENCODE-BRIF.md");
  if (ayniYol(ayar.brif, hedef)) {
    console.log("brif zaten hedefte: " + hedef + " (dal: " + kontrol.dal + ")");
  } else {
    fs.copyFileSync(ayar.brif, hedef);
    console.log("brif kopyalandi: " + hedef + " (dal: " + kontrol.dal + ")");
  }

  const k = komutKur({ model: ayar.model });
  const satir = komutSatiri(k.komut, k.argumanlar);

  if (ayar.dene) {
    console.log("dene: " + satir);
    console.log("calisma klasoru: " + path.resolve(ayar.worktree));
    process.exitCode = 0;
    return;
  }

  console.log("calisiyor: " + satir);
  const sonuc = await calistir(ayar.worktree, ayar.model, ayar.sureDk);
  console.log("log: " + sonuc.logYolu);
  ozetYaz(ayar.worktree, sonuc.gecenSn, sonuc.kod);
  process.exitCode = sonuc.kod;
}

module.exports = {
  argumanAyir,
  komutKur,
  worktreeKontrol,
  spawnSecenekleri,
  SABIT_ISTEM,
  VARSAYILAN_MODEL,
};

if (require.main === module) main();
