// Small enhancements; the page works fully without JavaScript.
const REPO = "jevido/dbird";
const DL = `https://github.com/${REPO}/releases/latest/download/`;

// Recommend the download for the visitor's platform.
const ua = navigator.userAgent;
const isWindows = /Windows/i.test(ua);
// iPads report themselves as Macs; there is no DBird for them.
const isMac = /Macintosh/i.test(ua) && navigator.maxTouchPoints <= 1;
const isLinux = !/Android/i.test(ua) && /Linux|X11/i.test(ua);

const primary = document.querySelector("[data-dl]");
const label = primary.querySelector("[data-label]");
const sub = primary.querySelector("[data-sub]");

function recommend(platform) {
  const card = document.querySelector(`[data-platform="${platform}"]`);
  card.classList.add("recommended");
  document.querySelector(".platforms").prepend(card);
}

if (isWindows) {
  primary.href = DL + "dbird-windows-amd64.zip";
  label.textContent = "Download for Windows";
  sub.textContent = "Windows 10 & 11 · 64-bit";
  recommend("windows");
} else if (isMac) {
  primary.href = DL + "dbird-darwin-universal.zip";
  label.textContent = "Download for Mac";
  sub.textContent = "Apple Silicon & Intel";
  recommend("mac");
} else if (isLinux) {
  primary.href = "#linux";
  label.textContent = "Get DBird for Linux";
  sub.textContent = "One-line install or package";
  recommend("linux");
}

// Linux packages name the architecture two ways: amd64/arm64 (tarball, deb)
// and x86_64/aarch64 (rpm, pacman, AppImage).
const ARCH = { amd64: { deb: "amd64", rpm: "x86_64" }, arm64: { deb: "arm64", rpm: "aarch64" } };
function setArch(arch) {
  document.querySelectorAll("[data-arch]").forEach((b) => b.classList.toggle("on", b.dataset.arch === arch));
  document.querySelectorAll("[data-arch-href]").forEach((a) => {
    a.href = DL + a.dataset.archHref.replace("{deb}", ARCH[arch].deb).replace("{rpm}", ARCH[arch].rpm);
  });
}
document.querySelectorAll("[data-arch]").forEach((b) => b.addEventListener("click", () => setArch(b.dataset.arch)));
if (isLinux && /aarch64|arm64/i.test(ua)) setArch("arm64");

// Copy buttons.
document.querySelectorAll("[data-copy]").forEach((btn) => {
  btn.addEventListener("click", async () => {
    const el = document.querySelector(btn.dataset.copy);
    try {
      await navigator.clipboard.writeText(el.textContent.trim());
      btn.textContent = "Copied!";
      btn.classList.add("done");
    } catch {
      btn.textContent = "Press Ctrl+C";
      const range = document.createRange();
      range.selectNodeContents(el);
      getSelection().removeAllRanges();
      getSelection().addRange(range);
    }
    setTimeout(() => {
      btn.textContent = "Copy";
      btn.classList.remove("done");
    }, 2000);
  });
});

// Show the latest version.
fetch(`https://api.github.com/repos/${REPO}/releases/latest`, { headers: { Accept: "application/vnd.github+json" } })
  .then((r) => (r.ok ? r.json() : Promise.reject()))
  .then((rel) => {
    if (!rel.tag_name) return;
    const date = new Date(rel.published_at).toLocaleDateString(undefined, { day: "numeric", month: "short", year: "numeric" });
    document.querySelector("[data-version]").textContent = `Version ${rel.tag_name.replace(/^v/, "")} · ${date} · open source`;
    document.querySelector("[data-version-footer]").textContent = rel.tag_name;
  })
  .catch(() => {});
