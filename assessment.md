# Savannah Informatics

**5th Floor,**  
**One Padmore Place, off George Padmore Road.**  
**Nairobi, Kenya**  
**P.O. Box 5980-00200.**

---

# BACKEND DEVELOPER TAKE-HOME ASSESSMENT

This assessment includes:
* **Time limit:** 3-5-days
* **AI Tools:** Permitted & Expected
* **Tech Stack:** Python (FastAPI or Django) or Go
* **Structure:** 4 sections - all connected
* **Scenario:** Single Scenario - Clinic booking system
* **Cloud deployment:** Provider of your choice
* **CI/CD:** Required - Auto-deploy on merge

All four sections are built around a single scenario. What you design in Section 1 is what you build in Section 2 and deploy in Section 3.

---

## INSTRUCTIONS

### What to Submit
* A link to your GitHub or GitLab repository (public or shared with us).
* A link to your deployed, running application.
* A README covering your design decisions, how to run locally, and a brief note on your CI/CD setup. 
* Your Section 4 reflection - in your README or as a separate file.

### Rules
* AI tools are permitted. You will be asked to reflect on how you used them.
* Use any database you prefer.
* When requirements are ambiguous, make a decision and note it in your README. Clear reasoning matters as much as working code.

**NOTE:** We evaluate how you think, not just what you produce. A well-reasoned README alongside functional code carries significant weight.

---

## SECTION 1: SYSTEM DESIGN

### The Scenario
"We run a small clinic with 5 doctors. Patients need to book appointments online. Each doctor has set working hours and works in 30-minute slots. A patient should see which slots are free for a given doctor on a given day, pick one, and book it. Once booked, that slot must not be available to others. Patients should also be able to cancel. We're starting small but want to grow."

Before writing any code, document your system design in your README. Cover the models and components you identified, the key decisions you made, and any trade-offs you considered.

---

## SECTION 2: API IMPLEMENTATION

Build a REST API for the clinic booking system using FastAPI, Django REST Framework, or Go. Use any database you prefer.

### Required Endpoints
* `POST /appointments` - Book a slot. Validate that it falls within the doctor's working hours, is not in the past, and is not already taken.
* `GET /doctors/{id}/availability` - Return all available 30-minute slots for a doctor on a given date.
* `PATCH /appointments/{id}/cancel` - Cancel an appointment with a reason. The slot must become bookable again. Return an error if already cancelled.
* `PATCH /appointments/{id}/reschedule` - Move an appointment to a new slot. The original slot must become available again, and the new slot must be validated as you would for a fresh booking. Return an error if the appointment is cancelled.

### Constraints
* Validation failures must return meaningful error messages with correct HTTP status codes. 
* Structure your code sensibly - not everything in a single file.
* Include at least basic test coverage for the booking logic.

### Bonus
* `GET /patients/{id}/appointments` - upcoming appointments sorted by date. 
* Prevention of bookings within 1 hour of now.

---

## SECTION 3: DEPLOYMENT & CI/CD

Deploy your application to a cloud provider of your choice (e.g., AWS, GCP, Azure, Render, Fly.io, Railway). The application must be reachable via a public URL at the time of submission.

### CI/CD Pipeline
Set up a pipeline using any CI/CD tool (GitHub Actions, GitLab CI, CircleCI, etc.) that: 
* Runs your test suite on every pull request.
* Automatically deploys when a PR is merged into a designated branch.

### What to Include in Your README
* The public URL of your deployed application.
* Which branch triggers a deployment and how.
* A brief description of what your pipeline does.

---

## SECTION 4: AI REFLECTION

Answer the following in your README or as a separate file. Bullet points are fine.

1. What did you use AI for across the four sections?
2. Give one example where an AI suggestion improved your work. What did you prompt it with? 
3. Give one example where AI output was wrong or incomplete and how you caught it. 
4. Name two decisions you made without AI. Why did you trust your own judgment there?

**NOTE:** We use AI tools on this team daily. Honesty here matters more than the answer itself.