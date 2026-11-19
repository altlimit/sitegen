// SiteGen starter script: theme toggle, mobile menu, copy buttons, heading
// anchors and the docs "On this page" list. No dependencies.
(function () {
    "use strict";

    var root = document.documentElement;

    function store(key, value) {
        try {
            localStorage.setItem(key, value);
        } catch (e) { /* storage may be blocked */ }
    }

    function isDark() {
        if (root.dataset.theme) return root.dataset.theme === "dark";
        return window.matchMedia("(prefers-color-scheme: dark)").matches;
    }

    function copyText(text, done) {
        if (navigator.clipboard && window.isSecureContext) {
            navigator.clipboard.writeText(text).then(done, function () {});
            return;
        }
        var area = document.createElement("textarea");
        area.value = text;
        area.style.position = "fixed";
        area.style.opacity = "0";
        document.body.appendChild(area);
        area.select();
        try {
            document.execCommand("copy");
            done();
        } catch (e) { /* nothing to do */ }
        area.remove();
    }

    function flash(el, cls) {
        el.classList.add(cls);
        setTimeout(function () {
            el.classList.remove(cls);
        }, 1600);
    }

    // Theme toggle. The <head> script applies a saved choice before paint.
    document.querySelectorAll("[data-theme-toggle]").forEach(function (btn) {
        btn.addEventListener("click", function () {
            var next = isDark() ? "light" : "dark";
            root.dataset.theme = next;
            store("theme", next);
        });
    });

    // Mobile menu.
    var toggle = document.querySelector(".nav-toggle");
    var nav = document.getElementById("site-nav");
    if (toggle && nav) {
        toggle.addEventListener("click", function () {
            var open = nav.classList.toggle("open");
            toggle.setAttribute("aria-expanded", open ? "true" : "false");
        });
    }

    // Copy buttons next to install commands.
    document.querySelectorAll("[data-copy-root]").forEach(function (box) {
        var code = box.querySelector("[data-copy]");
        var btn = box.querySelector(".copy-btn");
        if (!code || !btn) return;
        btn.addEventListener("click", function () {
            copyText(code.textContent.trim(), function () {
                flash(btn, "copied");
            });
        });
    });

    // Copy buttons on code blocks in long-form content.
    document.querySelectorAll(".prose pre, .page-body pre").forEach(function (pre) {
        var btn = document.createElement("button");
        btn.type = "button";
        btn.className = "code-copy";
        btn.textContent = "Copy";
        btn.addEventListener("click", function () {
            var code = pre.querySelector("code") || pre;
            copyText(code.textContent, function () {
                btn.textContent = "Copied";
                setTimeout(function () {
                    btn.textContent = "Copy";
                }, 1600);
            });
        });
        pre.appendChild(btn);
    });

    // Anchor links on headings. Markdown headings already have ids.
    var headings = document.querySelectorAll(".prose h2[id], .prose h3[id], .page-body h2[id], .page-body h3[id]");
    headings.forEach(function (h) {
        var a = document.createElement("a");
        a.className = "anchor";
        a.href = "#" + h.id;
        a.setAttribute("aria-label", "Link to this section");
        a.textContent = "#";
        h.insertBefore(a, h.firstChild);
    });

    // Docs "On this page" list, built from the h2 headings.
    var toc = document.querySelector("[data-toc]");
    if (toc) {
        var sections = Array.prototype.filter.call(headings, function (h) {
            return h.tagName === "H2";
        });
        if (sections.length > 1) {
            var links = sections.map(function (h) {
                var li = document.createElement("li");
                var a = document.createElement("a");
                a.href = "#" + h.id;
                a.textContent = h.textContent.replace(/^#/, "");
                li.appendChild(a);
                toc.appendChild(li);
                return a;
            });
            toc.closest(".docs-toc").hidden = false;

            if ("IntersectionObserver" in window) {
                var observer = new IntersectionObserver(function () {
                    var current = 0;
                    sections.forEach(function (h, i) {
                        if (h.getBoundingClientRect().top < 140) current = i;
                    });
                    links.forEach(function (a, i) {
                        a.classList.toggle("active", i === current);
                    });
                }, { rootMargin: "0px 0px -60% 0px" });
                sections.forEach(function (h) {
                    observer.observe(h);
                });
            }
        }
    }
})();
