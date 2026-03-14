# InfoForms

InfoForms are interactive questionnaires that users fill out online — a modernization of ViSiON/2's infoform system from `RUMORS.PAS` and `OVERRET1.PAS`. SysOps create template files with questions, and users complete them by typing answers at each prompt. Completed forms can be viewed by the user, by SysOps, and during New User Voting.

---

## How It Works

The SysOp creates **template files** — plain text files with questions and `*` markers where the user types an answer. When a user fills out a form, the system walks through the template, displays the text, pauses at each `*` for input, and saves all answers as a JSON response file.

Up to **5 forms** can be configured (matching ViSiON/2). Each form has:

| Setting | Description |
|---------|-------------|
| **Description** | Human-readable name shown in the form listing |
| **Min Level** | Minimum access level required to see/fill the form (0 = everyone) |
| **Required** | Whether the form must be completed before the user can proceed |

Forms can be marked as **required** — users cannot bypass them during login until all required forms are completed.

---

## Template Format

Templates are plain text files stored in `data/infoforms/templates/`. Each form is named `form_<n>.txt` where `n` is 1–5.

### Markers & Codes

| Marker | Description |
|--------|-------------|
| `*` | Input field — the system pauses here for user input |
| `*!` | Required input field — the user must enter a non-empty answer |
| `\|B<n>;` | Set maximum input length to `n` characters for the next `*` field (1–255) |
| `\|VN` | Replaced with the BBS version number |
| `\|CL` | Clear screen and home cursor |
| `\|01`–`\|15` | Standard pipe color codes for colored text |

All standard pipe codes are supported in templates (color codes, `\|CL`, `\|CR`, etc.) — template text is processed through the same pipe code engine used everywhere else in the BBS.

### Example Template (`form_1.txt`)

```text
|09New User Application
|08--------------------

|07What is your real name?
|03> *

|07What city/state do you live in?
|03> *

|07How did you hear about this BBS?
|03> *

|07What are your interests?
|03> *

|07Any other comments for the SysOp?
|03> *
```

### Example with Input Length Limits

```text
|CL|09Favorite Things Survey v|VN
|08------------------------------

|07Favorite color?
|03> |B20;*

|07Favorite BBS software? |08(Mystic, Enigma, Talisman, etc.)
|03> |B30;*

|07Tell us more about yourself:
|03> *
```

- `|CL` clears the screen before the form starts
- `|VN` inserts the current BBS version number
- `|B20;` before the first `*` limits the answer to 20 characters
- `|B30;` limits the next answer to 30 characters
- The final `*` has no `|B` code, so input length is unlimited

---

## Configuration

InfoForms configuration is stored in `data/infoforms/config.json`:

```json
{
    "descriptions": [
        "New User Application",
        "",
        "",
        "",
        ""
    ],
    "min_levels": [0, 0, 0, 0, 0],
    "required_forms": "1"
}
```

| Field | Description |
|-------|-------------|
| `descriptions` | Array of 5 strings — display name for each form (empty = no form) |
| `min_levels` | Array of 5 integers — minimum access level to see each form (0 = everyone) |
| `required_forms` | String of form numbers that are mandatory, e.g. `"1"` = form 1 required, `"135"` = forms 1, 3, and 5 required |

A form only appears in the listing if:
1. Its template file exists (`data/infoforms/templates/form_<n>.txt`)
2. The user's access level meets the form's minimum level

---

## User Interface

### InfoForms Menu (`RUN:INFOFORMS`)

The main infoforms screen lists all available forms with their status:

```text
 #  Description                    Required   Status
──────────────────────────────────────────────────────────────────────────
1   New User Application           Required   Completed..
```

The prompt depends on user type:

- **Validated users:** `InfoForms (V)iew (Q)uit or #:` — can view completed forms or fill out by number
- **Unvalidated users:** `Newuser Forms (Q)uit or #:` — can only fill out forms, no view option

**Quit enforcement:** If required forms are incomplete, the user sees "You still must complete Infoform #N" and cannot quit until all required forms are done.

**Default menu binding:** `I` on the Main Menu → `GOTO:INFORMM` → `I` on InfoForms Menu.

---

### Filling Out a Form

When a user selects a form number:

1. If already completed, prompts "You have already filled out form #N! Replace it?" — the old response is preserved until the new one is fully saved.
2. The template is displayed line by line. At each `*`, the user types their answer.
3. All answers are saved as a JSON response file with a timestamp (atomic write via temp file + rename).
4. If the user disconnects mid-form, nothing is saved and the old response (if any) remains intact.

---

### Viewing a Completed Form (`RUN:INFOFORMVIEW`)

Replays the template with the user's stored answers interpolated at each `*` marker. Shows the completion date at the top. Empty answers display as "No answer". Output is paged — the viewer pauses at each screenful and prompts to continue or quit.

For security, pipe codes in stored answers are escaped when displayed, so users cannot inject color codes or control sequences through their responses.

**Default menu binding:** `V` on the InfoForms Menu.

---

### InfoForm Hunt — SysOp (`RUN:INFOFORMHUNT`)

SysOp-only command that prompts for a form number (1–5) and displays all users' completed responses for that form. Each response shows the user's handle and their answers.

**Default menu binding:** `H` on the InfoForms Menu (ACS: `S255`).

---

### Nuke InfoForms — SysOp (`RUN:INFOFORMNUKE`)

SysOp-only command that deletes all form responses for a specific user. Prompts for a handle and confirms before deleting.

**Default menu binding:** `*` on the InfoForms Menu (ACS: `S255`, hidden).

---

### InfoForm Browser (User Editor / Validate)

When editing a user in the online User Editor or Validate screen, SysOps can browse that user's infoforms interactively. This displays a list of the user's forms with completion status, and pressing `1`–`5` views the selected form. Press `Q` or `Esc` to return.

This is not a standalone menu command — it is accessed from within the user editor screens.

---

### Required Forms at Login (`INFOFORMREQUIRED`)

A login sequence command that checks if the current user has any incomplete required forms and forces them to fill out each one. If a required form is not successfully completed (e.g., save failure or disconnect), the user is disconnected. This runs automatically during login if configured.

Add to `configs/login.json`:

```json
{
    "command": "INFOFORMREQUIRED",
    "clear_screen": true
}
```

This is typically placed near the end of the login sequence, after user validation but before the main menu.

---

## Menu Configuration

### INFORMM Menu

The InfoForms feature has its own submenu (`INFORMM`), accessed from the Main Menu via the `I` key.

**Main Menu entry** (`menus/v3/cfg/MAIN.CFG`):

```json
{
    "KEYS": "I",
    "CMD": "GOTO:INFORMM",
    "ACS": "*",
    "HIDDEN": false,
    "NODE_ACTIVITY": "InfoForms"
}
```

**InfoForms Menu commands** (`menus/v3/cfg/INFORMM.CFG`):

| Key | Command | ACS | Description |
|-----|---------|-----|-------------|
| `I` | `RUN:INFOFORMS` | `*` | Main infoforms menu (list/fill/view) |
| `V` | `RUN:INFOFORMVIEW` | `*` | View own completed form |
| `H` | `RUN:INFOFORMHUNT` | `S255` | SysOp: browse all users' forms |
| `*` | `RUN:INFOFORMNUKE` | `S255` | SysOp: delete all forms for a user |
| `Q` | `GOTO:MAIN` | `*` | Return to main menu |

### ANSI Screen

The ANSI art screen `menus/v3/ansi/INFORMM.ANS` is displayed when entering the InfoForms Menu. Edit this file to customize the appearance.

---

## Customizable Strings

The following prompts can be customized in `configs/strings.json`:

| Key | Default | Description |
|-----|---------|-------------|
| `infoformPrompt` | `InfoForms (V)iew (Q)uit or #:` | Prompt for existing users |
| `newInfoFormPrompt` | `Newuser Forms (Q)uit or #:` | Prompt for new users |
| `viewWhichForm` | `View which Form? (#) :` | Prompt when viewing a form |

---

## Data Storage

### Response Files

User responses are stored as individual JSON files in `data/infoforms/responses/`:

```
data/infoforms/responses/
  1_1.json    ← User ID 1, Form 1
  1_2.json    ← User ID 1, Form 2
  5_1.json    ← User ID 5, Form 1
```

Each response file:

```json
{
    "user_id": 1,
    "handle": "J0hnDoe",
    "form_num": 1,
    "filled_out_at": "2026-03-08T14:30:00Z",
    "answers": [
        "John Doe",
        "Portland, OR",
        "Found it on a BBS list",
        "Retro computing, ANSI art",
        "Great board!"
    ]
}
```

### Directory Structure

```
data/infoforms/
  config.json                    ← Form descriptions, levels, required forms
  templates/
    form_1.txt                   ← Template for form 1 (gaps are OK — e.g., form_3.txt with no form_2.txt)
  responses/
    <userID>_<formNum>.json      ← Per-user response files
```

The `responses/` directory is created automatically when the first form is completed (with `0700` permissions). Response files are written with `0600` permissions and use atomic writes (temp file + rename) for concurrent access safety.

---

## ViSiON/2 Compatibility

| V2 Feature | V3 Equivalent |
|------------|---------------|
| `INFOFORM.1`–`INFOFORM.5` template files | `data/infoforms/templates/form_1.txt`–`form_5.txt` |
| `Cfg.InfoformStr[1..5]` | `config.json` → `descriptions` |
| `Cfg.Infoformlvl[1..5]` | `config.json` → `min_levels` |
| `Cfg.RequiredForms` | `config.json` → `required_forms` |
| `Urec.Infoform[1..5]` (LongInt pointers) | File-based existence check (no user record field) |
| `FORMS.TXT` / `FORMS.MAP` (shared binary) | Per-user JSON files in `responses/` |
| `*` input marker in templates | Same — `*` marks input fields |
| `\|B<n>;` buffer length code | Same — sets max input length |
| `Infoforms` procedure (RUMORS.PAS) | `RUN:INFOFORMS` |
| `showinfoforms` (SUBSOVR.PAS) | `RUN:INFOFORMVIEW` |
| `InfoFormHunt` (MAINMENU.PAS) | `RUN:INFOFORMHUNT` |
| Required forms check (GETLOGIN.PAS) | `INFOFORMREQUIRED` login sequence command |
| `FORMS.TOP` / `FORMS.MID` / `FORMS.BOT` | Inline columnar listing (no template files needed) |

---

## See Also

- [Menu System](menus/menu-system.md) — menu and command file configuration
- [Login Sequence](users/login-sequence.md) — configuring login steps
- [New User Voting (NUV)](users/nuv.md) — community-based user approval
- [Rumors](menus/rumors.md) — similar community feature
- [String Editor](advanced/string-editor.md) — customizing prompt strings
