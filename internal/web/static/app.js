// Clinic Booking — progressive enhancement.
//
// The site works without any of this. Every page is a form and a list, and
// every action is a normal request that the server renders. This file only
// removes a click:
//
//   - changing the date submits the date form, so the times reload
//   - changing the patient picker navigates to that patient's appointments
//
// It is loaded with `defer`, so it never blocks the first paint, and every
// behaviour it adds is duplicated by a server-rendered fallback.

(function () {
  "use strict";

  // Submit the form a control belongs to, when it has one.
  function submitFormOf(element) {
    var form = element.form;
    if (form) {
      form.submit();
    }
  }

  // The date strip reloads the times as soon as a day is chosen.
  var dateInputs = document.querySelectorAll('.date-strip input[type="radio"]');
  for (var i = 0; i < dateInputs.length; i++) {
    dateInputs[i].addEventListener('change', function () {
      submitFormOf(this);
    });
  }

  // The "Show times" button only exists for people without JavaScript.
  var hideWithJs = document.querySelectorAll('[data-hide-with-js]');
  for (var j = 0; j < hideWithJs.length; j++) {
    hideWithJs[j].hidden = true;
  }

  // The patient picker navigates to that patient's appointments.
  var patientPicker = document.querySelector('[data-auto-submit]');
  if (patientPicker) {
    patientPicker.addEventListener('change', function () {
      var form = this.form;
      if (!form) {
        return;
      }
      var action = form.getAttribute('action');
      var id = this.value;
      if (action) {
        window.location.href = action.replace(/\/\d+\/appointments$/, '/' + id + '/appointments');
      }
    });
  }

  // Arrow keys move between the chips in the date strip and the slot grid,
  // so the group behaves like a list of options rather than a row of
  // separate controls.
  function wireArrowKeys(container, selector) {
    if (!container) {
      return;
    }
    container.addEventListener('keydown', function (event) {
      if (event.key !== 'ArrowRight' && event.key !== 'ArrowLeft') {
        return;
      }
      var items = Array.prototype.slice.call(container.querySelectorAll(selector));
      var index = items.indexOf(document.activeElement);
      if (index === -1) {
        return;
      }
      event.preventDefault();
      var next = event.key === 'ArrowRight' ? index + 1 : index - 1;
      if (next < 0) {
        next = items.length - 1;
      }
      if (next >= items.length) {
        next = 0;
      }
      items[next].focus();
    });
  }

  wireArrowKeys(document.querySelector('.date-strip__track'), 'input[type="radio"]');
  wireArrowKeys(document.querySelector('.slot-grid'), 'input[type="radio"]');
})();
