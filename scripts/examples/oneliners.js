// oneliners.js — AmiExpress-style oneliners, styled to match amilc.js
//
// The same scene as the last-callers door: a dotted logo shaded in
// magenta with the star cartoon beside it, a framed table, a magenta
// lightbar and a bottom scroller. Here the table lists the board's
// oneliners, newest first. UP/DOWN (or 8/2) moves the lightbar; A leaves
// a new line; Q or ESC quits.
//
// Reads and writes the real oneliner store through v3.oneliners, so it
// replaces the built-in ONELINERS step. Put it in the logon sequence
// (login.json: {"command": "DOOR:ONELINERS"}) and/or on a menu key.
//
// Configure in doors.json:
//   { "code": "ONELINERS", "name": "Oneliners", "type": "v3_script",
//     "script": "oneliners.js", "working_directory": "scripts/examples",
//     "args": ["40"] }
// The optional argument is how many lines to keep in the list (default 40).

// "1LINERS" in the AMC AAA01 figlet shape, transliterated to the dots
// : ; . , ' . The last two rows are tails that hang into the table's top
// border and header.
var LOGO = [
  "  .::.  :.       .:   .:..::.      .::.   .:..::.      .::.",
  ".::;;:  ::.     .::  .::..:;;.    .;;:.  .::..:;;.    .;;:.",
  " ' :;:  :;:     :;:  :;:   .:;.  .;:'    :;:   .:;.  .;:'",
  "   :;:  :;:     :;:  :;:    :;:  :;:     :;:    :;:  :;:",
  "   :;:  :;:     :;:  :;:    :;:  :;:     :;:    .;:  :;:",
  "   :;:  :;:     :;:  :;:    :;:  :;:.:.  :;:   .:;:  .;:.",
  "   :;:  :;:     :;:  :;:    :;:  :;:.:.  :;:...:::   .:;;:",
  "   :;:  :;:     :;:  :;:    :;:  :;:     :;:..:.;.     .:;:",
  "   :;:  :;.     :;:  :;:    :;:  :;.     :;:   .:;.     :;:",
  "  .:;:  :;:.    :;:  :;:    :;:  :;:.    :;:    :;:    .:;.",
  ".:::::.  :::..  :;:  :;:    :;:   :::..  :;:    :;:  .::;:",
  " `:::'    .::.  :;:  :;:    :::    .::.  :;:    :::  .::'",
  "                :.   :.                  :.",
  "                .    .                   .",
];
var LOGO_BODY = LOGO.length - 2;
var LOGO_W = 0;
for (var _lw = 0; _lw < LOGO.length; _lw++) {
  if (LOGO[_lw].length > LOGO_W) {
    LOGO_W = LOGO[_lw].length;
  }
}
var LOGO_COL = 15; // centred-ish, clearing the star on the left

// The star, rows 3 to 7 at the left, with its motion lines.
var STAR = [
  "       /\\",
  "\\\\ ___/oO\\___",
  " \\\\ \\   ^   /",
  " // /___ ___\\",
  "        \\/",
];
var STAR_ROW = 3,
  STAR_COL = 1;
var TWINKLE_MS = 140;
var SPARKLE = ["*", "+", ".", " ", ".", "+"];
// TING colours: a quick white/cyan/magenta shimmer for a glinting point.
var TING = ["|15", "|11", "|15", "|13"];

var WIDTH = 79;
var VERSION = "1liners v1.0";

// The bottom marquee text. It loops, so it ends with spacing that runs
// back into its start.
var SCROLL =
  "   " +
  VERSION +
  "   ::   " +
  v3.session.bbs.name +
  "   ::   UP/DN move    A add a line    Q quits   ::   ";

// Colours: logo shades, frame, labels.
var C_HI = "|13",
  C_MID = "|05",
  C_LO = "|08";
var C_LABEL = "|07";

// Table columns, 1-based, between borders at 1, 16, 70 and 79.
var COLS = {
  who: { at: 3, w: 12 },
  text: { at: 18, w: 51 },
  when: { at: 72, w: 6 },
};
var BORDERS = [1, 16, 70, 79];

var keep = parseInt(v3.args[0], 10) || 40;
var view = Math.max(1, v3.console.height - LOGO_BODY - 5);

var liners = v3.oneliners.list(keep);
var firstRow = LOGO_BODY + 4; // screen row of the first table row
var scroll = 0;
var pos = 0;
var frame = 0; // animation frame
var blinkL = 0,
  blinkR = 0; // frames each star eye stays closed mid-blink

draw();
lightbar();

// --- drawing -------------------------------------------------------------

// A line is an array of {c, k}: a character and its pipe colour code.
function blank() {
  var l = [];
  for (var i = 0; i < WIDTH; i++) {
    l.push({ c: " ", k: C_LABEL });
  }
  return l;
}

function put(line, col, text, colour) {
  for (var i = 0; i < text.length && col - 1 + i < WIDTH; i++) {
    line[col - 1 + i] = { c: text.charAt(i), k: colour };
  }
}

// render turns a line into pipe-coded text; a literal '|' is written '||'.
function render(line) {
  var out = "",
    cur = "";
  var end = line.length;
  while (end > 0 && line[end - 1].c === " ") {
    end--;
  }
  for (var i = 0; i < end; i++) {
    if (line[i].k !== cur && line[i].c !== " ") {
      cur = line[i].k;
      out += cur;
    }
    out += line[i].c === "|" ? "||" : line[i].c;
  }
  return out;
}

// logoColour shades in three bands, bright at the top, dark at the bottom.
function logoColour(ch, row) {
  var fill = ch === ";" || ch === ",";
  if (row < 5) {
    return fill ? C_MID : C_HI;
  }
  if (row < 9) {
    return fill ? C_LO : C_MID;
  }
  return C_LO;
}

function logoInk(r, c) {
  if (r < 0 || r >= LOGO.length || c < 0) {
    return false;
  }
  return c < LOGO[r].length && LOGO[r].charAt(c) !== " ";
}

function logoInkCount(r, c) {
  return (
    (logoInk(r - 1, c) ? 1 : 0) +
    (logoInk(r + 1, c) ? 1 : 0) +
    (logoInk(r, c - 1) ? 1 : 0) +
    (logoInk(r, c + 1) ? 1 : 0)
  );
}

// logoEdge: a drawn cell bordering empty space, where a glint catches.
function logoEdge(r, c) {
  return logoInk(r, c) && logoInkCount(r, c) < 4;
}

// logoTip: a drawn cell that is a stroke endpoint or isolated dot.
function logoTip(r, c) {
  return logoInk(r, c) && logoInkCount(r, c) <= 1;
}

function putLogoRow(line, row) {
  var text = LOGO[row];
  for (var i = 0; i < text.length; i++) {
    if (text.charAt(i) !== " ") {
      put(line, LOGO_COL + i, text.charAt(i), logoColour(text.charAt(i), row));
    }
  }
}

// The star: white body with grey motion lines, light from the upper left.
function putStarRow(line, row) {
  var s = row - STAR_ROW;
  if (s < 0 || s >= STAR.length) {
    return;
  }
  var text = STAR[s];
  for (var i = 0; i < text.length; i++) {
    var ch = text.charAt(i);
    if (ch === " ") {
      continue;
    }
    var col;
    if (ch === "o" || ch === "O" || ch === "-") {
      col = C_HI;
    } else if (ch === "\\") {
      col = C_LO;
    } else {
      col = "|15";
    }
    put(line, STAR_COL + i, ch, col);
  }
}

// frameColor keeps the box dark grey with a high-magenta glint at the
// upper-left and lower-right corners, fading back to grey a few cells in.
function frameColor(row, col) {
  var up = col - 1 + (row - (firstRow - 3));
  var lr = WIDTH - col + (firstRow + view - row);
  var d = up < lr ? up : lr;
  if (d <= 1) {
    return "|13";
  }
  if (d <= 3) {
    return "|05";
  }
  return "|08";
}

function frameLine(left, mid, end, fill, row) {
  var l = blank();
  for (var i = 1; i <= WIDTH; i++) {
    put(l, i, fill, frameColor(row, i));
  }
  for (var b = 0; b < BORDERS.length; b++) {
    put(
      l,
      BORDERS[b],
      b === 0 ? left : b === BORDERS.length - 1 ? end : mid,
      frameColor(row, BORDERS[b]),
    );
  }
  return l;
}

function putCentered(line, col, text, colour) {
  text = v3.util.stripPipe(
    String(text === undefined || text === null ? "" : text),
  );
  if (text.length > col.w) {
    text = text.substring(0, col.w);
  }
  var off = ((col.w - text.length) / 2) | 0;
  put(line, col.at + off, text, colour);
}

function putHead(head, col, text) {
  putCentered(head, col, text, C_LABEL);
}

function bordered(row) {
  var l = blank();
  for (var b = 0; b < BORDERS.length; b++) {
    put(l, BORDERS[b], "|", frameColor(row, BORDERS[b]));
  }
  return l;
}

function two(n) {
  return (n < 10 ? "0" : "") + n;
}

function draw() {
  v3.console.clear();
  for (var r = 0; r < LOGO_BODY; r++) {
    var line = blank();
    putStarRow(line, r);
    putLogoRow(line, r);
    v3.console.println(render(line));
  }
  var top = frameLine(".", "-", ".", "-", firstRow - 3);
  putLogoRow(top, LOGO_BODY);
  v3.console.println(render(top));
  var head = bordered(firstRow - 2);
  putLogoRow(head, LOGO_BODY + 1);
  putHead(head, COLS.who, "who");
  putHead(head, COLS.text, "oneliner");
  putHead(head, COLS.when, "when");
  v3.console.println(render(head));
  v3.console.println(render(frameLine("|", "+", "|", "-", firstRow - 1)));
  for (var i = 0; i < view; i++) {
    drawRow(i);
  }
  v3.console.gotoxy(1, firstRow + view);
  v3.console.println(render(frameLine("`", "-", "'", "-", firstRow + view)));
  drawScroller(0);
}

// drawScroller draws one frame of the bottom marquee with a greyscale
// shine sweeping through it.
function drawScroller(frame) {
  var off = ((frame / 2) | 0) % SCROLL.length;
  var hc = ((frame * 2) % (WIDTH + 12)) - 6;
  var out = "",
    cur = "";
  for (var x = 0; x < WIDTH; x++) {
    var ch = SCROLL.charAt((off + x) % SCROLL.length);
    var d = hc > x ? hc - x : x - hc;
    var col = d === 0 ? "|15" : d <= 2 ? "|07" : "|08";
    if (col !== cur) {
      out += col;
      cur = col;
    }
    out += ch === "|" ? "||" : ch;
  }
  v3.console.gotoxy(1, firstRow + view + 1);
  v3.console.print(out + "|07");
  parkCursor();
}

function parkCursor() {
  v3.console.gotoxy(WIDTH + 1, firstRow + view + 1);
}

// --- rows ----------------------------------------------------------------

function fit(s, w) {
  s = v3.util.stripPipe(String(s === undefined || s === null ? "" : s));
  if (s.length > w) {
    return s.substring(0, w);
  }
  return v3.util.padRight(s, w);
}

// mmdd shows a posted date as m/d, or blank for an unknown time.
function mmdd(unix) {
  if (!unix) {
    return "";
  }
  var d = new Date(unix * 1000);
  return two(d.getMonth() + 1) + "/" + two(d.getDate());
}

// rowLine builds the cell line for the oneliner at list index scroll + i.
function rowLine(i) {
  var line = bordered(firstRow + i);
  var o = liners[scroll + i];
  if (o) {
    var who = o.anonymous ? "Anonymous" : o.handle;
    put(line, COLS.who.at, fit(who, COLS.who.w), o.anonymous ? C_LO : C_HI);
    // The oneliner text is overlaid separately so its own pipe colours show.
    putCentered(line, COLS.when, mmdd(o.postedAt), C_MID);
  } else if (i === 0) {
    put(line, COLS.text.at, "no oneliners yet - press A to leave one", C_LO);
  }
  return line;
}

// clipPipe returns raw clipped to `width` visible columns and padded to it,
// keeping its embedded pipe colour codes intact so they render in place.
function clipPipe(raw, width) {
  raw = String(raw === undefined || raw === null ? "" : raw);
  var out = "",
    vis = 0,
    i = 0;
  while (i < raw.length && vis < width) {
    var ch = raw.charAt(i);
    if (ch === "|") {
      var n = raw.charAt(i + 1);
      if (n === "|") {
        out += "||";
        vis++;
        i += 2;
        continue;
      }
      out += raw.substr(i, 3); // |XX colour or |Bn background
      i += 3;
      continue;
    }
    out += ch;
    vis++;
    i++;
  }
  while (vis < width) {
    out += " ";
    vis++;
  }
  return out;
}

function drawRow(i) {
  v3.console.gotoxy(1, firstRow + i);
  v3.console.print(render(rowLine(i)));
  var o = liners[scroll + i];
  if (o) {
    v3.console.gotoxy(COLS.text.at, firstRow + i);
    v3.console.print("|07" + clipPipe(o.text, COLS.text.w) + "|07");
  }
}

// spanText returns the plain characters of cols from..to, doubling '|'.
function spanText(line, from, to) {
  var out = "";
  for (var col = from; col <= to; col++) {
    var ch = line[col - 1].c;
    out += ch === "|" ? "||" : ch;
  }
  return out;
}

// drawSelection paints the current row as one magenta bar, keeping the
// inner dividers and the outer frame borders.
function drawSelection(i, on) {
  var o = liners[scroll + i];
  if (!o) {
    return;
  }
  if (!on) {
    drawRow(i);
    parkCursor();
    return;
  }
  v3.console.gotoxy(2, firstRow + i);
  v3.console.print("|13|B5" + spanText(rowLine(i), 2, WIDTH - 1) + "|B0|07");
  // On the bar the text is shown plain-white so it stays readable.
  v3.console.gotoxy(COLS.text.at, firstRow + i);
  v3.console.print("|15|B5" + fit(o.text, COLS.text.w) + "|B0|07");
  parkCursor();
}

// --- animation -----------------------------------------------------------

// drawWave rolls a diagonal band of light across the logo letters,
// oscillating back and forth, flaring edges white and letting points ting.
function drawWave(frame) {
  var lo = -(LOGO_BODY - 1),
    hi = LOGO_W - 1;
  var span = hi - lo;
  var t = frame % (2 * span);
  var pc = (t < span ? t : 2 * span - t) + lo;
  for (var r = 0; r < LOGO_BODY; r++) {
    var jc = pc + r;
    var jt = jc - 7;
    var row = LOGO[r];
    var a = Math.max(0, jt - 4),
      b = Math.min(row.length - 1, jc + 4);
    if (a > b) {
      continue;
    }
    var out = "";
    for (var j = a; j <= b; j++) {
      var ch = row.charAt(j);
      if (ch === " ") {
        out += " ";
        continue;
      }
      var d1 = jc > j ? jc - j : j - jc;
      var d2 = jt > j ? jt - j : j - jt;
      var col;
      if (d1 === 0 && logoTip(r, j)) {
        col = TING[(frame + r + j) % TING.length];
      } else if (d1 <= 2 && logoEdge(r, j)) {
        col = "|15";
      } else if (d1 <= 1) {
        col = "|13";
      } else if (d2 <= 1 && logoEdge(r, j)) {
        col = "|07";
      } else if (d2 === 0) {
        col = "|05";
      } else {
        col = logoColour(ch, r);
      }
      out += col + (ch === "|" ? "||" : ch);
    }
    v3.console.gotoxy(LOGO_COL + a, r + 1);
    v3.console.print(out);
  }
}

function drawTwinkle(frame) {
  drawWave(frame);
  drawScroller(frame);
  var s = (frame / 3) | 0;
  v3.console.gotoxy(6, STAR_ROW + 1);
  v3.console.print(C_HI + SPARKLE[s % SPARKLE.length] + "|07");
  v3.console.gotoxy(6, STAR_ROW + 5);
  v3.console.print(C_MID + SPARKLE[(s + 3) % SPARKLE.length] + "|07");
  if (blinkL <= 0 && blinkR <= 0 && Math.random() < 0.07) {
    var dur = 2 + ((Math.random() * 2) | 0);
    var pick = Math.random();
    if (pick < 0.45) {
      blinkL = dur;
    } else if (pick < 0.9) {
      blinkR = dur;
    } else {
      blinkL = dur;
      blinkR = dur;
    }
  }
  v3.console.gotoxy(8, STAR_ROW + 2);
  v3.console.print("|15" + (blinkL > 0 ? "-" : "o") + (blinkR > 0 ? "-" : "O"));
  if (blinkL > 0) {
    blinkL--;
  }
  if (blinkR > 0) {
    blinkR--;
  }
  parkCursor();
}

// --- input ---------------------------------------------------------------

// addOneliner takes a line from the caller on the scroller row, stores it
// through v3.oneliners, then reloads the list with the new line on top.
function addOneliner() {
  var py = firstRow + view + 1;
  v3.console.gotoxy(1, py);
  v3.console.print("\x1b[2K|13 leave a line: |15\x1b[?25h");
  var text = v3.console.getstr(COLS.text.w);
  v3.console.print("\x1b[?25l");
  if (text && v3.util.stripPipe(text).replace(/\s/g, "") !== "") {
    v3.oneliners.add(text);
    liners = v3.oneliners.list(keep);
    pos = 0;
    scroll = 0;
  }
  draw();
  if (liners.length) {
    drawSelection(pos - scroll, true);
  }
}

function lightbar() {
  if (liners.length === 0) {
    for (;;) {
      var k0 = v3.console.readkey(TWINKLE_MS);
      if (k0 === "") {
        drawTwinkle(++frame);
        continue;
      }
      if (k0 === "a" || k0 === "A") {
        addOneliner();
        continue;
      }
      break;
    }
    finish();
    return;
  }
  drawSelection(pos - scroll, true);
  for (;;) {
    var key = v3.console.readkey(TWINKLE_MS);
    if (key === "") {
      drawTwinkle(++frame);
      continue;
    }
    var next = pos;
    if (key === "UP" || key === "8") {
      next = pos > 0 ? pos - 1 : liners.length - 1;
    } else if (key === "DOWN" || key === "2") {
      next = pos < liners.length - 1 ? pos + 1 : 0;
    } else if (key === "a" || key === "A") {
      addOneliner();
      continue;
    } else if (key === "q" || key === "Q" || key === "ESC" || key === "\r") {
      break;
    }
    if (next === pos) {
      continue;
    }
    drawSelection(pos - scroll, false);
    pos = next;
    if (pos < scroll || pos >= scroll + view) {
      scroll = pos < scroll ? pos : pos - view + 1;
      for (var i = 0; i < view; i++) {
        drawRow(i);
      }
    }
    drawSelection(pos - scroll, true);
  }
  finish();
}

function finish() {
  v3.console.gotoxy(1, firstRow + view + 1);
  v3.console.println("");
  v3.console.println("");
}
