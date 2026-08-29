(() => {
  const btn = document.querySelector("[data-menu]");
  const links = document.querySelector("[data-links]");
  btn?.addEventListener("click", () => {
    const open = links?.classList.toggle("open");
    btn.setAttribute("aria-expanded", String(Boolean(open)));
  });

  async function copyText(text) {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(text);
      return;
    }
    const ta = document.createElement("textarea");
    ta.value = text;
    ta.setAttribute("readonly", "");
    ta.style.position = "fixed";
    ta.style.left = "-9999px";
    document.body.appendChild(ta);
    ta.select();
    document.execCommand("copy");
    ta.remove();
  }

  function wireCopy(host, source) {
    let button = host.querySelector("[data-copy]");
    if (!button) {
      button = document.createElement("button");
      button.type = "button";
      button.className = "copy-btn";
      button.dataset.copy = "";
      button.textContent = "Copy";
      host.appendChild(button);
    }
    button.addEventListener("click", async () => {
      const text = (source.innerText || source.textContent || "").replace(/\n$/, "");
      try {
        await copyText(text);
        const prev = button.textContent;
        button.textContent = "Copied";
        window.setTimeout(() => {
          button.textContent = prev;
        }, 1600);
      } catch {
        button.textContent = "Copy failed";
        window.setTimeout(() => {
          button.textContent = "Copy";
        }, 1600);
      }
    });
  }

  document.querySelectorAll(".hero-code, .code-block").forEach((block) => {
    const source = block.querySelector("pre, code");
    if (source) wireCopy(block, source);
  });

  document.querySelectorAll("pre").forEach((pre) => {
    if (pre.closest(".hero-code, .code-block")) return;
    const wrap = document.createElement("div");
    wrap.className = "code-block";
    pre.parentNode.insertBefore(wrap, pre);
    wrap.appendChild(pre);
    wireCopy(wrap, pre);
  });
})();
