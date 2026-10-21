# Design System — Clinic Booking

The visual and interaction language for the patient-facing clinic booking site.

This is not a mood board. Every colour, size and pairing below is a **decision with a reason**,
and the colour decisions are **machine-checked**: `design/tokens.json` is the source of truth and
`design/tokens_test.go` fails the build if any documented pairing drops below its WCAG 2.2
contrast requirement.

```bash
go test ./design/ -v     # prints the verified contrast table
```

Change a colour there, run the test, and the table below regenerates. Nothing in this document is
allowed to drift from the tokens.

---

## 1. Principles

1. **Calm before clever.** The largest health brands read as calm: generous space, one accent
   colour, no decoration. A patient booking a doctor is often already anxious. The interface
   should lower the temperature.
2. **Colour is a scarce resource.** Every hue on a screen costs something. In the slot grid, the
   only colour is the selection. Colour marks *state*, never decoration.
3. **Accessible by construction, not by audit.** Contrast is enforced by a test, not by a
   checklist someone ticks. See §10.
4. **Never use colour alone.** Every status carries an icon and a word. Around 1 in 12 men has a
   red-green colour vision deficiency, and green-versus-red is exactly the pair they cannot
   separate. See §4.2.
5. **Plain language.** Say "appointment", not "booking slot". Say "9:30 AM", not "09:30". The
   GOV.UK and NHS service manuals are the reference here: short sentences, active voice, everyday
   words.
6. **Honest about availability.** No fake scarcity. If twelve slots are free, say twelve are free.

---

## 2. Reference sites

What each of these teaches us, and what we deliberately do **not** copy.

| Site | What we take | What we avoid |
|---|---|---|
| **NHS** (nhs.uk) | The reference for accessible public health services. A palette built so that *"all colours offer at least an AA accessibility rating"*. Pale grey page background, a single strong blue, generous type. | The cookie banner on arrival, which pushes the booking task behind a wall. |
| **Cleveland Clinic** (my.clevelandclinic.org) | The **primary / secondary / supporting** palette structure — a small dominant set, accents that never dominate, and neutrals that carry status. Also their "Find a Provider" pattern, which is exactly our doctor picker. | A footer that carries a dozen competing navigation trees. |
| **Mayo Clinic** (mayoclinic.org) | Editorial restraint. Generous measure, real hierarchy, content that does not shout. | Serif display type throughout, which costs legibility at small sizes. |
| **Healthline** (healthline.com) | Content-first information design, and the medical-content colour discipline of never using red for non-urgent states. | Article-length pages for a task that should take three clicks. |
| **GOV.UK Design System** | The gold standard for accessible, plain-English public services. Focus states that are impossible to miss, error messages that say what to do next, one primary action per screen. | Not an aesthetic, but a set of interaction rules worth stealing wholesale. |

The common thread: **blue-and-white dominates healthcare web design**, and a survey of healthcare
sites confirms blue and white is the most common scheme. We take that signal and shift it
slightly toward teal, which reads more clinical than corporate blue and is less likely to be
confused with a hospital's corporate identity.

---

## 3. Colour

### 3.1 The palette

Six scales, each running light to dark. Every scale has the same shade names so that a component
never has to know which ramp it is on.

**Brand — Clinical Teal.** The accent. Buttons, links, the selected slot, the focus ring.

| | 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| `brand` | `#F2F9FB` | `#DDF0F5` | `#BCE1EA` | `#8CCBDC` | `#52AFC7` | `#2A93AE` | `#1C778F` | `#1A6073` | `#1A4E5D` | `#1A424F` | `#0D2A33` |

**Neutral — Cool Slate.** The substrate. Cool rather than warm, so it sits with the teal rather
than against it. Note `neutral.950` is a soft near-black, not `#000`: NHS Scotland's guidelines
introduce *"a dark grey as an alternative to black as it is less harsh"* and we agree. Pure black
on white is uncomfortably high contrast for body text.

| | 0 | 25 | 50 | 100 | 200 | 300 | 400 | 500 | 600 | 700 | 800 | 900 | 950 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| `neutral` | `#FFFFFF` | `#FBFCFD` | `#F5F8F9` | `#ECF1F3` | `#DCE5E9` | `#C1CED5` | `#96A8B1` | `#63767F` | `#556871` | `#45555D` | `#303A40` | `#212A2F` | `#12181C` |

**`success`, `warning`, `danger`, `info`** carry the full ramp in `design/tokens.json` and are
summarised by their working shades in §4.

### 3.2 Which shade to use

| Role | Token | Notes |
|---|---|---|
| Page background | `neutral.0` | White. Cards sit on `neutral.50` if they need to lift. |
| Section background | `neutral.50` | The NHS-style pale grey band. |
| Primary text | `neutral.900` | 14.61:1 on white. |
| Secondary text | `neutral.600` | 5.83:1. |
| Muted text | `neutral.500` | 4.74:1. **This is the floor.** |
| Link | `brand.700` | 7.08:1. Never `brand.500` — that fails on white. |
| Primary button | `brand.600` fill, `neutral.0` label | 5.14:1. |
| Primary button hover | `brand.700` fill | 7.08:1. |
| Input border | `neutral.500` | 4.74:1, clears the 3:1 non-text requirement with room to spare. |
| Focus ring | `brand.600` | 5.14:1 against the page, and 4.82:1 against a subtle surface. |
| Divider | `neutral.300` | Decorative only. Never the only thing separating two things. |

### 3.3 A note on `neutral.500`

The muted-text grey is the single most common accessibility failure in a design system, because
it *looks* fine on screen. The first draft of this palette used `#6E828D`, which measured
**4.01:1** — below the 4.5:1 required for body text. The token test caught it and the shade was
darkened to `#63767F` at 4.74:1.

The lesson generalises: **a grey that "looks like disabled text" is usually failing.** If it
cannot pass at 4.5:1, it is not muted text, it is illegible text.

---

## 4. Semantic colour

### 4.1 The mapping

| State | Treatment | Token | Icon | Text |
|---|---|---|---|---|
| **Available slot** | Neutral surface, neutral border | `neutral.0` / `neutral.500` | — | `neutral.900` |
| **Selected slot** | Brand fill | `brand.600` | ✓ | `neutral.0` |
| **Taken slot** | Muted, disabled | `neutral.100` / `neutral.300` | — | `neutral.600` |
| **Too soon to book** | Muted, disabled, explained | `neutral.100` | ⏱ | `neutral.600` |
| **Booked appointment** | Brand tint badge | `brand.800` on `brand.50` | ✓ | "Booked" |
| **Cancelled appointment** | Danger tint badge | `danger.800` on `danger.50` | ✕ | "Cancelled" |
| **Confirmation** | Success banner | `success.800` on `success.50` | ✓ | "Booked" |
| **Warning / limit reached** | Warning banner | `warning.800` on `warning.50` | ⚠ | "Only one slot left" |
| **Error** | Danger text and border | `danger.700` / `danger.600` | ✕ | The error message |
| **Information** | Info banner | `info.800` on `info.50` | ℹ | The message |

### 4.2 Why "available" is not a colour

This is the decision most likely to be argued with, so here is the reasoning.

**Argument for green available slots.** It is immediately readable. "Green means free."

**Why we rejected it.**

1. **Sixteen green slots is a wall of green.** A doctor's day is 16 slots. If every one is a
   filled green chip, the page becomes a solid green field, green stops carrying meaning, and the
   one element that genuinely needs to stand out — the *selected* slot and the *Book* button —
   has to fight it for attention.
2. **Green and red are the one pair a colour-blind reader cannot separate.** Roughly 1 in 12 men
   has a red-green colour vision deficiency. If green means "available" and red means
   "cancelled", those two states are indistinguishable for a meaningful slice of users, and
   the difference between them is the difference between a free appointment and a lost booking.
3. **Green is a reward colour.** It should mark a *completed* action — the confirmation banner —
   not the default state of a field.

So: **available is the absence of colour.** A free slot is a quiet, bordered, neutral button. The
selection is the only filled thing in the grid. The grid reads as a calendar, not as a traffic
light.

### 4.3 Red is reserved

In a health context red means *medical emergency*. A cancelled appointment is a neutral
administrative fact, not an emergency. It is therefore shown in the muted `danger` tint with an
icon and the word "Cancelled" — never a red banner, never a red page.

Red on this site means exactly two things: **an error that stopped the user**, and **a
destructive action about to happen**.

### 4.4 Verified contrast

Generated by `go test ./design/ -v`. Every row is enforced by that test.

| Foreground | Background | Contrast | Required | Use |
|---|---|---|---|---|
| `neutral.900` | `neutral.0` | **14.61:1** | 4.5:1 | Primary body text on a page |
| `neutral.600` | `neutral.0` | **5.83:1** | 4.5:1 | Secondary text, labels, helper copy |
| `neutral.500` | `neutral.0` | **4.74:1** | 4.5:1 | Muted text, timestamps, placeholders |
| `neutral.900` | `neutral.50` | **13.69:1** | 4.5:1 | Primary text on a subtle surface |
| `neutral.700` | `neutral.100` | **6.80:1** | 4.5:1 | Badge text on a neutral badge |
| `neutral.900` | `neutral.0` | **14.61:1** | 4.5:1 | Available slot label, the default unremarkable state |
| `brand.700` | `neutral.0` | **7.08:1** | 4.5:1 | Link text |
| `brand.800` | `neutral.0` | **9.16:1** | 4.5:1 | Visited link text |
| `brand.800` | `brand.50` | **8.60:1** | 4.5:1 | Booked appointment badge |
| `brand.700` | `brand.50` | **6.65:1** | 4.5:1 | Brand text on a brand tint |
| `neutral.0` | `brand.600` | **5.14:1** | 4.5:1 | Primary button label |
| `neutral.0` | `brand.700` | **7.08:1** | 4.5:1 | Primary button label, hover |
| `neutral.0` | `brand.900` | **10.85:1** | 4.5:1 | Footer band text |
| `neutral.300` | `neutral.0` | **1.61:1** | 1.0:1 | Divider and card border, decorative only |
| `neutral.500` | `neutral.0` | **4.74:1** | 3.0:1 | Input border |
| `neutral.500` | `neutral.0` | **4.74:1** | 3.0:1 | Available slot border |
| `brand.600` | `neutral.0` | **5.14:1** | 3.0:1 | Focus ring and selected slot border |
| `brand.600` | `neutral.50` | **4.82:1** | 3.0:1 | Selected slot border on a subtle surface |
| `success.800` | `success.50` | **7.86:1** | 4.5:1 | Confirmation banner |
| `success.700` | `neutral.0` | **6.46:1** | 4.5:1 | Success text |
| `warning.800` | `neutral.0` | **8.98:1** | 4.5:1 | Too soon to book warning text |
| `warning.800` | `warning.50` | **8.23:1** | 4.5:1 | Warning badge text |
| `danger.700` | `neutral.0` | **7.99:1** | 4.5:1 | Error text |
| `danger.800` | `danger.50` | **8.98:1** | 4.5:1 | Cancelled badge text |
| `danger.600` | `neutral.0` | **5.99:1** | 3.0:1 | Destructive action border |
| `info.800` | `info.50` | **8.47:1** | 4.5:1 | Information badge text |
| `info.700` | `neutral.0` | **7.41:1** | 4.5:1 | Information text |

---

## 5. Typography

### 5.1 The stack

```
font-family: system-ui, -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
```

**System fonts, not a webfont.** Three reasons, in order of weight:

1. **OS text sizing is an accessibility feature.** When a patient sets their phone to a larger
   font, system fonts scale. A webfont does not, unless it is loaded as a variable font and
   scaled manually. For a service used heavily by older patients, this matters more than
   distinctiveness.
2. **Zero requests, zero layout shift.** No webfont means no FOUT and no third-party origin.
3. **The aesthetic cost is small.** A well-set system stack with a strong hierarchy looks
   completely credible. Mayo Clinic's serif is a brand asset; ours is not.

If a distinctive face is wanted later, Inter is the safe pick — it is a variable font with a
large optical size range, so it scales well — but it must be loaded with `font-display: swap` and
preloaded, and the stack must keep the system fallback so the page is readable before it
arrives.

**Tabular numerals for every time.** Slot times are a grid; proportional figures make the
columns ragged. `font-variant-numeric: tabular-nums` on anything that renders a time.

### 5.2 The scale

A 1.25 (major third) scale on a 16px base.

| Token | Size | Line height | Use |
|---|---|---|---|
| `xs` | 12px | 16px | Legal, timestamps only. Never the only copy. |
| `sm` | 14px | 20px | Secondary labels, helper text |
| `base` | 16px | 24px | Body. **The floor for anything a patient must read.** |
| `lg` | 20px | 28px | Lead paragraph, card titles |
| `xl` | 24px | 32px | Section headings |
| `2xl` | 30px | 36px | Page titles |
| `3xl` | 36px | 44px | Marketing and empty-state headings |
| `4xl` | 48px | 56px | Rare. Hero only. |

Weights: `400` body, `500` labels and buttons, `600` headings, `700` used sparingly.

**Never below 16px for content a patient must act on.** Clinic booking sites routinely use 12px
and 14px for the details that matter — the doctor's hours, the appointment time. Those are the
details that matter most. `xs` is for legal and footer text only.

### 5.3 Measure and rhythm

- Body copy: **60–75 characters** per line. `max-width: 65ch`.
- Headings: `text-wrap: balance`. Body: `text-wrap: pretty`.
- Line height 1.5 for body, 1.2 for headings, 1.4 for small text.

---

## 6. Spacing, layout and shape

**Spacing scale** — a 4px base, everything else a multiple of 8.

```
4  8  12  16  24  32  40  48  64  80  96  128
```

**Layout**

- Container: `1200px` max, `24px` gutter on mobile, `32px` above 768px.
- Grid: 4 columns on mobile, 8 at 768px, 12 at 1024px.
- Breakpoints: `480` / `768` / `1024` / `1280`.
- **Mobile first.** The majority of clinic traffic is a phone, often on mobile data, often
  outdoors.

**Radius** — `4px` small controls, `8px` inputs and buttons, `12px` cards and panels, `999px`
pills.

**Elevation** — three levels, all very soft. Health sites that float cards around look like a
fintech app.

```
level 1  0 1px 2px  rgba(18, 24, 28, 0.06)
level 2  0 2px 8px  rgba(18, 24, 28, 0.08)
level 3  0 8px 24px rgba(18, 24, 28, 0.10)
```

**Motion** — `150ms` for hover and focus, `200ms` for panels, `ease-out`. Nothing animates on
page load; a slot grid that fades in delays the one thing the patient came to do. Honour
`prefers-reduced-motion` by dropping durations to `0.01ms` rather than removing transitions
blindly.

---

## 7. Components

Each component names the API it is built from, so the design and the backend cannot drift apart.

### 7.1 Doctor card — `GET /doctors`

The Cleveland Clinic "Find a Provider" pattern.

- Photo, name, specialty, and working hours from the API.
- Working hours shown as a plain range: **"9:00 AM – 5:00 PM"**, never "09:00–17:00".
- The whole card is one target. There is no separate small "Book" link — a nested link inside a
  clickable card is a keyboard trap.
- One primary action per card: **View availability**.

### 7.2 Date strip — `GET /doctors/{id}/availability?date=`

- A horizontal scroller of the next 14 days on mobile, a week grid on desktop.
- The selected date is the only filled item.
- Keyboard: arrow keys move between dates, `Enter` selects, the group is a labelled radio group.
- Days with no availability are still shown, disabled, with "No slots" as the accessible name.
  Hiding them makes the calendar look broken.

### 7.3 Slot grid — `GET /doctors/{id}/availability?date=`

The centrepiece. It is a **grid of buttons**, never a `<select>`: a dropdown with 16 options is
a poor comparison surface, and it hides the shape of the day.

| Aspect | Decision |
|---|---|
| Layout | 3 columns at 320px, 4 at 480px, 6 at 1024px. `gap: 8px`. |
| Target size | **44 × 44px minimum.** WCAG 2.2 requires 24 × 24 (2.5.8); 44 is the practical figure for a stressed or older user on a phone. |
| Time format | **"9:30 AM"** in 12-hour form. The API speaks 24-hour `09:30`; the conversion happens at the edge, and only for display. |
| Available | Neutral surface, `neutral.500` border, `neutral.900` label. No fill. |
| Selected | `brand.600` fill, white label, ✓ icon. `aria-pressed="true"`. |
| Taken | `neutral.100` fill, `neutral.300` border, `neutral.600` label, `disabled`. |
| Too soon | Same as taken, plus a clock icon and the accessible name "9:30 AM, too soon to book". |
| Focus | 2px `brand.600` ring, 2px offset. Never removed. |
| Live updates | When availability changes, announce it in an `aria-live="polite"` region: "8 slots available". |

**The one-hour rule must be explained, not just enforced.** When the API refuses a slot inside
the lead time, the interface shows those slots disabled and states the rule plainly above the
grid: *"Appointments must be booked at least 1 hour in advance."* A rule the patient cannot see
is a rule they will think is a bug.

### 7.4 Booking summary and confirmation — `POST /appointments`

- A sticky summary panel on desktop, a collapsible summary above the button on mobile.
- Shows: doctor, full date ("Thursday 22 October 2026"), time, and the clinic's contact details.
- The confirmation screen leads with the **success banner** and a reference — the appointment id
  from the API — because that is what a patient reads out over the phone.

### 7.5 Appointment card — `GET /patients/{id}/appointments`

- Status badge, doctor, date and time, and the actions for that state.
- **Booked** → *Cancel* and *Reschedule*.
- **Cancelled** → no actions, and the reason if there is one.
- A cancelled appointment in the patient's history is shown, not hidden. Hiding it makes patients
  call the clinic to ask what happened.
- `?include_cancelled=true` is the default on this screen, so the reschedule trail is visible.

### 7.6 Buttons

| Variant | Fill | Use |
|---|---|---|
| Primary | `brand.600` | The one action per screen. |
| Secondary | Transparent, `neutral.500` border | Cancel, Back. |
| Ghost | No border | Tertiary, in-card links. |
| Destructive | `danger.600` border, `danger.700` label | Cancel an appointment. Confirm with a second step, never a single click. |

### 7.7 Forms and errors

- Every field has a visible label. Placeholder text is not a label; it disappears on focus.
- Errors appear **below** the field, in `danger.700`, with the field marked `aria-invalid` and
  the message linked by `aria-describedby`.
- Error text says **what to do next**: "Enter a date as YYYY-MM-DD", not "Invalid date".
- On submit failure, focus moves to the first invalid field and the summary lists every problem.
- Nothing is submitted on blur. The API is authoritative; the client validates only what it can
  check instantly.

### 7.8 The one interaction that needs care

`GET /availability` is **advisory** — it can be stale by the time the patient presses *Book*, and
the API enforces the real rule at insert time. So:

- On a **409 `SLOT_TAKEN`**, do not show a generic error. Re-fetch availability, mark the
  offending slot as taken, keep the rest of the selection, and say: *"That slot was just taken.
  Here are the next available times."* The patient loses their place in no way at all.
- On a **422 `TOO_SOON`**, re-fetch availability and explain the lead-time rule.
- The availability response is never cached across a date change.

---

## 8. The booking flow

Three steps and a confirmation. Not four steps and a confirmation.

```
1. Choose a doctor  →  2. Choose a date and time  →  3. Confirm details  →  ✓ Confirmed
```

- Show progress, and let the patient go back without losing their selection.
- **One primary action per screen.** The "Book" button is unambiguous about what it does.
- The confirmation step is a page, not a modal. A modal can be dismissed by accident and the
  patient loses the booking they just made.
- **Never block the flow with a consent or cookie banner.** Show it inline, below the fold, and
  let the patient book. This is the single biggest conversion leak on healthcare sites.

---

## 9. Content and tone

Plain English, in the GOV.UK/NHS tradition: short sentences, active voice, everyday words, no
jargon.

| Instead of | Write |
|---|---|
| "Booking slot" | "Appointment" |
| "Please select an available time slot" | "Choose a time" |
| "Your request has been successfully processed" | "Your appointment is booked" |
| "09:30 – 10:00" | "9:30 AM – 10:00" |
| "No appointments available" | "No appointments available on this date. Try the next day." |
| "An unexpected error occurred" | "We could not load the times. Try again, or call the clinic." |

**Errors in a health context should offer a way out.** Every error state needs a human next
step: retry, choose another time, or a phone number.

---

## 10. Accessibility

Target: **WCAG 2.2 level AA**. The colour half is machine-checked; the rest is a checklist.

**Colour** — enforced by `go test ./design/`.

- 1.4.3 Contrast (minimum), text: ≥ 4.5:1. Verified for every pair in §4.4.
- 1.4.11 Non-text Contrast: ≥ 3:1 for input borders, focus rings and component boundaries.
- 1.4.1 Use of Colour: no state is signalled by colour alone. Every status has an icon and a word.

**Structure**

- 1.3.1 Info and Relationships: semantic landmarks, real headings in order, real lists.
- 3.1.2 Language: set `lang="en"` on the root.
- 2.4.1 Bypass Blocks: a skip link to the main content.

**Keyboard**

- 2.4.7 Focus Visible: a 2px ring, never removed.
- 2.4.11 Focus Not Obscured *(new in 2.2)*: sticky headers must not cover the focused element.
- 2.1.1 / 2.1.2: everything operable by keyboard, no keyboard traps. The doctor card and the
  slot grid must both be fully usable without a mouse.
- The slot grid is a group of toggle buttons; `Tab` moves between groups, arrow keys within.

**Forms**

- 3.3.1 Error Identification, 3.3.2 Labels or Instructions, 3.3.3 Error Suggestion.
- 3.3.7 Redundant Entry *(new in 2.2)*: do not ask for the same information twice across the
  booking flow.
- 4.1.3 Status Messages *(new in 2.2)*: availability changes and submit results are announced
  through an `aria-live="polite"` region.

**Target size and reflow**

- 2.5.8 Target Size (Minimum): 24 × 24 CSS px is the requirement; **44 × 44 is the target** for
  every interactive element, including the slot buttons.
- 1.4.10 Reflow: usable at 320px wide with no horizontal scrolling of the page.
- 1.4.12 Text Spacing: the layout survives user-applied spacing overrides.

**Motion and media**

- 2.3.3 Animation from Interactions: honour `prefers-reduced-motion`.
- 1.4.2 Audio Control: no audio.
- No autoplaying video, no carousels.

**The audience**

Clinic patients skew older, and many will be reading on a phone in a waiting room with one hand
or on a slow connection. Assume: a 320px viewport, a large OS font setting, a slow network, and
someone who is not confident online. Every one of those assumptions pushes toward bigger targets,
more contrast, less motion and fewer steps.

---

## 11. Trust and credibility

A patient is handing over their health. The interface has to earn that.

- **Show the doctor's name, photo and hours on the booking screen**, not just on a list page.
- **Show the clinic's address and phone number** in the footer and in the confirmation.
- **Give a reference.** The appointment id from the API is the reference. Display it.
- **Be honest about availability.** No fake scarcity.
- **Do not invent accreditations, ratings or patient testimonials.** If the clinic has them, they
  are real data; if not, they are not on the page.
- **Say what happens to the patient's data.** One plain sentence. This site has no
  authentication and no persistence of patient identity beyond the appointment itself — that is
  a stated limitation and the site should not imply otherwise.

---

## 12. Performance

The site sits in front of an API that returns everything the booking flow needs in one or two
requests. The performance budget is therefore about not wasting what the API gives us.

- Target **LCP under 2.5s** on a slow 4G connection, and under 1.5s on a good one.
- **No webfonts** means no font request and no layout shift.
- **One request for doctors, one for availability.** `GET /doctors` once on load;
  `GET /doctors/{id}/availability` per date change. No waterfall.
- Do not poll availability. Fetch on demand.
- Render the slot grid as soon as availability arrives, with a skeleton for the grid only — never
  a full-page spinner.
- The `Dockerfile` already produces a 29 MB image; the front end should ship as static assets
  with cache headers keyed on content hash.

---

## 13. Anti-patterns

Things that go wrong on healthcare sites, with the reason.

| Anti-pattern | Why it is wrong |
|---|---|
| Red for a cancelled appointment | Red means medical emergency. A cancelled booking is administrative. |
| Green for every available slot | A wall of green destroys the hierarchy and collides with the red-green colour-blindness problem. |
| Status shown by colour alone | Fails 1.4.1, and fails ~8% of men outright. |
| Muted grey below 4.5:1 | Illegible. This exact mistake was caught in this palette's first draft. |
| 12px or 14px for appointment details | The details that matter most are the ones shrunk. |
| A `<select>` for the time | 16 options in a dropdown is a poor comparison surface and hides the shape of the day. |
| A modal for confirmation | Dismissable by accident. A lost booking is the worst possible outcome. |
| A cookie or consent wall before the booking flow | The biggest conversion leak on healthcare sites. |
| "Only 2 slots left!" when twelve are free | Manufactured urgency. Patients notice, and they stop trusting the site. |
| Red blinking badges or carousel autoplay | Motion and noise in a clinical context. WCAG 2.2 has new criteria on this. |
| Autoplaying video | Unwanted, and a data cost the patient did not agree to. |
| Hidden cancelled appointments | Makes patients call the clinic to ask what happened. |
| A generic "Something went wrong" | A dead end. Every error needs a next step. |

---

## 14. Governance

**`design/tokens.json` is the source of truth.** A component never contains a hex value; it
references a token. Change the token, run the test, and the contrast table in §4.4 regenerates.

```bash
go test ./design/ -v        # verifies every pairing, prints the table
```

The test suite also fails on a malformed hex value, on two token names sharing a colour, and on a
scale whose shades run the wrong way round. Those are the three ways a hand-edited ramp goes
wrong.

**When adding a colour**, add it to an existing scale. A new hue needs a reason that survives the
question "why is this not `brand.500` or `neutral.600`?"

**When adding a pairing**, add it to `pairs` with its WCAG requirement. If it cannot meet the
requirement, the design is wrong, not the test.

**When changing a colour**, re-run the test. The contrast table in this document is generated
output and must be regenerated with it — never hand-edited.

---

## 15. Open questions

Things a real project would settle before building, listed so they are not lost:

- **Does the site need authentication?** The API has none. If patients are to see their own
  appointments rather than all of them, `GET /patients/{id}/appointments` needs an identity
  behind it. This is the largest open question and it affects the whole flow.
- **Is there a waitlist?** The API models availability as free or taken. A waitlist is a
  different data model and a different flow.
- **Which timezone is shown to a patient travelling?** The API stores clinic wall-clock time.
  A patient booking from another timezone sees clinic time, which is correct but should be
  labelled as such.
- **Are appointments ever longer than 30 minutes?** The API fixes every appointment at 30
  minutes. A 45-minute consultation would need a schema change, not just a UI change.
