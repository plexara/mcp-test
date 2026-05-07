/**
 * Portal-screenshots carousel + lightbox.
 *
 * Carousel: slides the track via `transform: translateX` with a CSS
 * transition for the actual animation; JS just tracks the current index
 * and computes the offset. Wrap-around is implicit; buttons stay enabled.
 *
 * Lightbox: clicking any frame opens a near-full-screen modal showing
 * the screenshot at full size. ESC closes; arrow keys step between
 * shots. Backdrop click closes. Focus is captured on open and restored
 * on close.
 *
 * Material's instant-nav can re-mount the page; we guard re-binding via
 * data attributes on each subscribed root.
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

      var counter = root.querySelector("[data-shots-counter]");
      var lightbox = root.querySelector("[data-shots-lightbox]");

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
        if (counter) {
          counter.innerHTML =
            "<strong>" + String(index + 1).padStart(2, "0") + "</strong>" +
            " / " + String(slides.length).padStart(2, "0");
        }
      }

      function go(delta) {
        index = (index + delta + slides.length) % slides.length;
        apply();
      }

      next.addEventListener("click", function () { go(1); });
      prev.addEventListener("click", function () { go(-1); });

      // Keyboard nav on the carousel itself: when focus lives within the
      // stage, arrow keys advance. The lightbox owns its own keyboard
      // handler when open, so this only fires while it's closed.
      root.addEventListener("keydown", function (e) {
        if (lightbox && !lightbox.hidden && lightbox.classList.contains("is-open")) return;
        if (e.target.closest(".plex-shots__frame")) {
          // Don't hijack Enter/Space on the frame button: those
          // legitimately open the lightbox.
          if (e.key === "Enter" || e.key === " ") return;
        }
        if (e.key === "ArrowLeft")  { go(-1); }
        else if (e.key === "ArrowRight") { go(1); }
      });

      // Keep the offset correct across resizes (slide widths are vw-based).
      var resizeRaf;
      window.addEventListener("resize", function () {
        if (resizeRaf) cancelAnimationFrame(resizeRaf);
        resizeRaf = requestAnimationFrame(apply);
      });

      apply();
      bindLightbox(root, slides, lightbox, function (newIndex) {
        // When the lightbox navigates, sync the carousel so closing
        // returns the user to the slide they were last viewing.
        index = newIndex;
        apply();
      }, function () { return index; });
    });
  }

  function bindLightbox(root, slides, lightbox, onIndex, getIndex) {
    if (!lightbox || lightbox.dataset.lightboxBound === "1") return;
    lightbox.dataset.lightboxBound = "1";

    var imgLight = lightbox.querySelector("[data-lightbox-img-light]");
    var imgDark  = lightbox.querySelector("[data-lightbox-img-dark]");
    var titleEl  = lightbox.querySelector("#plex-lightbox-title");
    var bodyEl   = lightbox.querySelector("[data-lightbox-body]");
    var countEl  = lightbox.querySelector("[data-lightbox-count]");
    var prevBtn  = lightbox.querySelector("[data-lightbox-prev]");
    var nextBtn  = lightbox.querySelector("[data-lightbox-next]");
    // Note: data-shots-close lives on BOTH the backdrop div and the close
    // button; the close button is what we focus on open (the backdrop
    // isn't focusable). The dismiss handler iterates closeBtns to attach
    // click handlers to both.
    var closeBtns = lightbox.querySelectorAll("[data-shots-close]");
    var closeBtn = lightbox.querySelector("button[data-shots-close]");

    var lastFocus = null;
    var current = 0;

    function fillFromSlide(slideIndex) {
      var slide = slides[slideIndex];
      var frame = slide && slide.querySelector(".plex-shots__frame");
      if (!frame) return;
      current = slideIndex;

      var title = frame.getAttribute("data-zoom-title") || "";
      var body  = frame.getAttribute("data-zoom-body")  || "";
      var light = frame.getAttribute("data-zoom-light") || "";
      var dark  = frame.getAttribute("data-zoom-dark")  || "";

      if (titleEl) titleEl.textContent = title;
      if (bodyEl)  bodyEl.textContent  = body;
      if (imgLight) {
        imgLight.src = light;
        imgLight.alt = "Portal " + title + " screen, light theme";
      }
      if (imgDark) {
        imgDark.src = dark;
        imgDark.alt = "Portal " + title + " screen, dark theme";
      }
      if (countEl) {
        countEl.textContent =
          String(slideIndex + 1).padStart(2, "0") +
          " / " +
          String(slides.length).padStart(2, "0");
      }
    }

    function open(slideIndex) {
      // If a previous close() is still in its 260ms post-fade hide
      // window, cancel the pending hide so the freshly-opened modal
      // doesn't get yanked back to hidden mid-display.
      if (lightbox._hideTimer) {
        clearTimeout(lightbox._hideTimer);
        lightbox._hideTimer = null;
      }
      lastFocus = document.activeElement;
      fillFromSlide(slideIndex);
      lightbox.hidden = false;
      // Force a reflow so the transition runs on the next paint.
      void lightbox.offsetWidth;
      lightbox.classList.add("is-open");
      document.documentElement.classList.add("plex-lightbox-open");
      // Focus the close button so ESC and Enter both work immediately
      // and screen-reader users land inside the dialog.
      if (closeBtn) closeBtn.focus();
      window.addEventListener("keydown", onKey, true);
    }

    function close() {
      lightbox.classList.remove("is-open");
      document.documentElement.classList.remove("plex-lightbox-open");
      window.removeEventListener("keydown", onKey, true);
      // After the fade completes, hide outright so the modal can't
      // catch tab focus or screen-reader attention.
      lightbox._hideTimer = setTimeout(function () {
        lightbox.hidden = true;
      }, 260);
      onIndex(current);
      if (lastFocus && typeof lastFocus.focus === "function") {
        lastFocus.focus();
      }
      lastFocus = null;
    }

    function onKey(e) {
      if (e.key === "Escape") {
        e.stopPropagation();
        close();
      } else if (e.key === "ArrowLeft") {
        e.stopPropagation();
        e.preventDefault();
        fillFromSlide((current - 1 + slides.length) % slides.length);
      } else if (e.key === "ArrowRight") {
        e.stopPropagation();
        e.preventDefault();
        fillFromSlide((current + 1) % slides.length);
      }
    }

    // Click handlers on the carousel frames.
    slides.forEach(function (slide, i) {
      var frame = slide.querySelector("[data-shots-zoom]");
      if (!frame) return;
      frame.addEventListener("click", function (e) {
        e.preventDefault();
        // Sync the carousel to the clicked slide first so the
        // background reflects the lightbox's starting frame. open()
        // handles its own pending-hide-timer cleanup.
        if (i !== getIndex()) onIndex(i);
        open(i);
      });
    });

    // Lightbox buttons.
    if (prevBtn) prevBtn.addEventListener("click", function () {
      fillFromSlide((current - 1 + slides.length) % slides.length);
    });
    if (nextBtn) nextBtn.addEventListener("click", function () {
      fillFromSlide((current + 1) % slides.length);
    });
    closeBtns.forEach(function (btn) {
      btn.addEventListener("click", function (e) {
        e.preventDefault();
        close();
      });
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
