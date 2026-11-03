// Tells CERBERUS a person is here while they read or type and send nothing, as its
// docs/JOINING.md gives it. Typing, clicking, scrolling and touching count; moving the
// mouse alone does not. Only a person's action sends this, never a timer. The address
// is read at each sending, as a boosted page swaps the note with the page's own part.
(function () {
  var every = 60000;
  var last = Date.now();
  function felt(event) {
    var now = Date.now();
    var note = document.getElementById("activity");
    if (!note || !note.hidden || !event.isTrusted || document.hidden || now - last < every) return;
    last = now;
    fetch(note.dataset.url, { method: "POST", credentials: "same-origin" })
      .then(function (answer) {
        if (answer.status === 401) note.hidden = false; // what they typed stays on the page
      })
      .catch(function () {});
  }
  ["keydown", "input", "pointerdown", "wheel", "touchstart", "scroll"].forEach(function (name) {
    addEventListener(name, felt, { capture: true, passive: true });
  });
})();
