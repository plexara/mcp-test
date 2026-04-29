/**
 * Portal-screenshots carousel. Slides the track via `transform: translateX`
 * with a CSS transition for the actual animation; JS just tracks the
 * current index and computes the offset.
 *
 * Wraparound: clicking next at the last slide returns to 0; prev at 0
 * jumps to the last slide. Buttons stay enabled the whole time.
 */
(function () {
  function init() {
    document.querySelectorAll(".plex-shots").forEach(function (root) {
      if (root.dataset.shotsBound === "1") return;
      root.dataset.shotsBound = "1";

      var track = root.querySelector("[data-shots-track]");
      var prev = root.querySelector("[data-shots-prev]");
      var next = root.querySelector("[data-shots-next]");
      if (!track || !prev || !next) return;

      var slides = Array.from(track.querySelectorAll(".plex-shots__slide"));
      if (slides.length === 0) return;

      var index = 0;

      function step() {
        var styles = window.getComputedStyle(track);
        var gap = parseFloat(styles.columnGap || styles.gap || "0") || 0;
        return slides[0].getBoundingClientRect().width + gap;
      }

      function apply() {
        var offset = -index * step();
        track.style.transform = "translate3d(" + offset + "px, 0, 0)";
        slides.forEach(function (s, i) {
          s.classList.toggle("is-active", i === index);
        });
      }

      next.addEventListener("click", function () {
        index = (index + 1) % slides.length;
        apply();
      });
      prev.addEventListener("click", function () {
        index = (index - 1 + slides.length) % slides.length;
        apply();
      });

      // Keep the offset correct across resizes (slide widths are vw-based).
      var resizeRaf;
      window.addEventListener("resize", function () {
        if (resizeRaf) cancelAnimationFrame(resizeRaf);
        resizeRaf = requestAnimationFrame(apply);
      });

      apply();
    });
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", init);
  } else {
    init();
  }
  // Re-init on Material instant-nav.
  if (typeof window.document$ !== "undefined" && window.document$.subscribe) {
    window.document$.subscribe(init);
  }
})();
