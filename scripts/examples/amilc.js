// amilc.js — AmiExpress-style last callers
//
// The last callers screen Amiga /X boards ran as doors: a scene logo
// shaded in magenta, purple and dark grey with a star cartoon beside it,
// a framed table with one row per call, a [DUW] column lit where the
// caller downloaded, uploaded or wrote a message on that call, the bytes
// and messages of the call, and a status bar. UP/DOWN (or 8/2, A/Z)
// moves the lightbar and scrolls, SPACE shows the caller's stats, ENTER
// or Q leaves.
//
// The per-call figures come from snapshots of each caller's upload,
// download and post counts that this script stores when it runs, so run
// it in the logon sequence (login.json: {"command": "DOOR:AMILC"}).
// A menu key can run it too.
//
// Configure in doors.json:
//   { "code": "AMILC", "name": "Last Callers", "type": "v3_script",
//     "script": "amilc.js", "working_directory": "scripts/examples",
//     "args": ["20"] }
// The optional argument is how many calls to keep in the list (default
// 20); as many as fit on the caller's screen show at once.

// "CALLERS" in the AMC AAA01 figlet shape, redrawn with only the dots
// : ; . , . The last two rows are the tails that hang into the table's
// top border and header.
var LOGO = [
  "  .::. .:,:::.  :.   :.     .::. .:,.::.    .::.",
  " :;;::.::;::::: ::.  ::.   :;;::.::;::;;:  :;;::",
  ":;:.  :;:   :::::;:  :;:  :;:.  :;:   .:;::;:.",
  ":;:   :;:    :;::;:  :;:  :;:   :;:    :;::;:",
  ":;:   :;: ::::;::;:  :;:  :;:   :;:    :;::;:",
  ":;:   :;:  :::;::;:  :;:  :;:,:.:;:   .:;::;:.",
  ":;:   :;:    :;::;:  :;:  :;:;:::;:,.:::: .:;;:",
  ":;:   :;:    :;::;:  :;:  :;:   :;:;:::;:   .:;:",
  ":;:   :;:    :;::;:  :;:  :;:   :;:   .:;:   :;:",
  ":;:.  :;:    :;::;:. :;:. :;:.  :;:    :;:  .:;:",
  " ::::.:;:    :;: ::::.::::.::::.:;:    :;:.::;:",
  "  :::::::    :;:  :::: :::: :::::;:    ::::::.",
  "             ::                 ::",
  "             :                  :",
];
var LOGO_COL = 17; // centred; puts the tails in blank table cells
var LOGO_BODY = LOGO.length - 2;
var LOGO_W = 0;
for (var _lw = 0; _lw < LOGO.length; _lw++) {
  if (LOGO[_lw].length > LOGO_W) {
    LOGO_W = LOGO[_lw].length;
  }
}

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
// Twinkle: two sparkles fade in and out beside the star and the eyes
// blink now and then. A frame advances each time readkey times out.
var TWINKLE_MS = 140;
var SPARKLE = ["*", "+", ".", " ", ".", "+"];
// TING colours: a quick white/cyan/magenta shimmer for a glinting point.
var TING = ["|15", "|11", "|15", "|13"];

var WIDTH = 79;
var VERSION = "amilc v1.3";
var KEEP_SNAPS = 20;

// The bottom marquee text. It loops, so it ends with spacing that runs
// back into its start.
var SCROLL =
  "   " +
  VERSION +
  "   ::   " +
  v3.session.bbs.name +
  "   ::   UP/DN move    ENTER view a caller    Q quits   ::   ";

// Colours: logo shades, frame, labels.
var C_HI = "|13",
  C_MID = "|05",
  C_LO = "|08";
var C_FRAME = "|01",
  C_LABEL = "|07";
// The lightbar and status bar: dark-magenta background, high-magenta text.
var C_BAR = "|B5",
  C_BARTEXT = "|13";

// Table columns, 1-based, between borders at 1, 22, 38, 55, 62 and 79.
var COLS = {
  node: { at: 3, w: 2 },
  handle: { at: 6, w: 15 },
  where: { at: 24, w: 13 },
  did: { at: 40, w: 3 },
  min: { at: 44, w: 3 },
  login: { at: 49, w: 5 },
  from: { at: 57, w: 5 },
  up: { at: 64, w: 5 },
  dn: { at: 70, w: 5 },
  wr: { at: 76, w: 2 },
};
var BORDERS = [1, 22, 38, 55, 62, 79];

var keep = parseInt(v3.args[0], 10) || 20;
var view = Math.max(1, v3.console.height - LOGO_BODY - 5);

recordSnapshot();
var calls = v3.users.lastCallers(keep);
var snaps = v3.data.get("snaps") || {};
var firstRow = LOGO_BODY + 4; // screen row of the first table row
var scroll = 0;
var pos = 0;
var frame = 0;      // animation frame, shared by the list and the pop-up
var boxOpen = false; // the caller pop-up is up; pause the logo wave behind it
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

// logoColour shades in three bands, bright at the top and dark at the
// bottom; within a band the %&* fill sits one shade below the S strokes.
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

// logoInk reports whether the logo has a drawn character at row r, col c.
function logoInk(r, c) {
  if (r < 0 || r >= LOGO.length || c < 0) {
    return false;
  }
  return c < LOGO[r].length && LOGO[r].charAt(c) !== " ";
}

// logoInkCount counts a cell's drawn orthogonal neighbours.
function logoInkCount(r, c) {
  return (
    (logoInk(r - 1, c) ? 1 : 0) +
    (logoInk(r + 1, c) ? 1 : 0) +
    (logoInk(r, c - 1) ? 1 : 0) +
    (logoInk(r, c + 1) ? 1 : 0)
  );
}

// logoEdge is true for a drawn cell that borders empty space, i.e. a cell
// on the outline of a letter, where a glint of light would catch.
function logoEdge(r, c) {
  return logoInk(r, c) && logoInkCount(r, c) < 4;
}

// logoTip is true for a drawn cell that is a point: an endpoint of a
// stroke or an isolated dot, the pointy bits a glint makes "ting".
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

// The star: white body with grey motion lines, drawn to the left of the
// logo.
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
    // Light from the upper left: "/" and "_" are leading edges and read
    // white; "\\" is a trailing edge and reads grey. The face stays
    // magenta.
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

// frameColor keeps the box dark grey with a short high-magenta glint at
// the upper-left and lower-right corners, where the light catches, fading
// in a couple of cells back to grey.
function frameColor(row, col) {
  var up = (col - 1) + (row - (firstRow - 3)); // steps from the upper-left corner
  var lr = (WIDTH - col) + (firstRow + view - row); // steps from the lower-right corner
  var d = up < lr ? up : lr;
  if (d <= 1) {
    return "|13"; // high magenta glint
  }
  if (d <= 3) {
    return "|05"; // dark magenta
  }
  return "|08"; // dark grey
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

// putCentered writes text centred within a column, clipped to its width.
function putCentered(line, col, text, colour) {
  text = v3.util.stripPipe(String(text === undefined || text === null ? "" : text));
  if (text.length > col.w) {
    text = text.substring(0, col.w);
  }
  var off = ((col.w - text.length) / 2) | 0;
  put(line, col.at + off, text, colour);
}

// putHead centres a column header within its column width.
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

// fullDate is today as m.d.yyyy with the zeros written as the letter o.
function fullDate() {
  var d = new Date();
  var s =
    two(d.getMonth() + 1) + "." + two(d.getDate()) + "." + d.getFullYear();
  return s.replace(/0/g, "o");
}

function draw() {
  v3.console.clear();
  for (var r = 0; r < LOGO_BODY; r++) {
    var line = blank();
    putStarRow(line, r);
    putLogoRow(line, r);
    if (r === 1) {
      put(line, 3, "'", C_LO);
      put(line, 4, "lAsT", C_HI);
      put(line, 8, "'", C_LO);
    }
    if (r === 9) {
      var dt = fullDate();
      var dstart = 78 - dt.length - 1;
      put(line, dstart - 2, "'", C_LO);
      for (var di = 0; di < dt.length; di++) {
        var dc = dt.charAt(di);
        put(line, dstart + di, dc, dc === "." ? C_HI : C_MID);
      }
      put(line, 78, "'", C_LO);
    }
    v3.console.println(render(line));
  }
  var top = frameLine(".", "-", ".", "-", firstRow - 3);
  putLogoRow(top, LOGO_BODY);
  v3.console.println(render(top));
  var head = bordered(firstRow - 2);
  putLogoRow(head, LOGO_BODY + 1);
  putHead(head, COLS.node, "nd");
  putHead(head, COLS.handle, "handle");
  putHead(head, COLS.where, "grp/loc");
  putHead(head, COLS.did, "did");
  putHead(head, COLS.min, "min");
  putHead(head, COLS.login, "time");
  putHead(head, COLS.from, "from");
  putHead(head, COLS.up, "up");
  putHead(head, COLS.dn, "dn");
  putHead(head, COLS.wr, "wr");
  v3.console.println(render(head));
  v3.console.println(render(frameLine("|", "+", "|", "-", firstRow - 1)));
  for (var i = 0; i < view; i++) {
    drawRow(i);
  }
  v3.console.gotoxy(1, firstRow + view);
  v3.console.println(render(frameLine("`", "-", "'", "-", firstRow + view)));
  drawScroller(0);
}

// drawScroller draws one frame of the bottom marquee: the scroll text
// windowed at the current offset, with a greyscale shine (dark grey,
// grey, white, grey, dark grey) sweeping through it.
function drawScroller(frame) {
  var off = ((frame / 2) | 0) % SCROLL.length;
  var hc = ((frame * 2) % (WIDTH + 12)) - 6; // the shine's moving centre
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

// parkCursor keeps the cursor off the lightbar, at the end of the bar.
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

function right(s, w) {
  s = String(s);
  return s.length >= w ? s.substring(0, w) : v3.util.padLeft(s, w);
}

// countryOf keeps only the country from a place label: "MO/US" -> "US",
// "GB" -> "GB", "LAN" -> "LAN". Callers see the country, not the state.
function countryOf(s) {
  s = String(s || "");
  var slash = s.indexOf("/");
  return slash >= 0 ? s.substring(slash + 1) : s;
}

// when shows the call's login time, or its date if it was not today.
function when(unix) {
  var d = new Date(unix * 1000);
  if (d.toDateString() === new Date().toDateString()) {
    return two(d.getHours()) + ":" + two(d.getMinutes());
  }
  return two(d.getMonth() + 1) + "/" + two(d.getDate());
}

// size renders a byte count in at most five columns: 340K, 1.2M, 12M, 3.4G.
function size(b) {
  if (!b) {
    return "-";
  }
  var units = ["K", "M", "G", "T"];
  var v = b / 1024;
  for (var i = 0; i < units.length; i++) {
    if (v < 1024 || i === units.length - 1) {
      if (v < 1) {
        v = 1;
      }
      return (
        (v < 10 && i > 0 ? v.toFixed(1) : String(Math.round(v))) + units[i]
      );
    }
    v /= 1024;
  }
}

// rowLine builds the cell line for the call at list index scroll + i.
function rowLine(i) {
  var line = bordered(firstRow + i);
  var c = calls[scroll + i];
  if (c) {
    var st = callStats(c);
    put(line, COLS.node.at, right(c.node, 2), C_LO);
    put(line, COLS.handle.at, fit(c.handle, COLS.handle.w), C_HI);
    putCentered(line, COLS.where, c.location, C_MID);
    var flags = [
      ["D", "dn"],
      ["U", "up"],
      ["W", "wr"],
    ];
    for (var f = 0; f < flags.length; f++) {
      // The snapshot tells us what the caller did; for calls from before
      // snapshots, fall back to the record's stored actions string.
      var lit = st
        ? st[flags[f][1]] > 0
        : !!c.actions && c.actions.indexOf(flags[f][0]) >= 0;
      put(line, COLS.did.at + f, lit ? flags[f][0] : "-", lit ? C_HI : C_LO);
    }
    put(line, COLS.min.at, right(c.minutes, 3), C_LABEL);
    var tm = when(c.connectTime);
    for (var ti = 0; ti < tm.length; ti++) {
      put(line, COLS.login.at + ti, tm.charAt(ti), tm.charAt(ti) === ":" ? "|07" : "|15");
    }
    put(line, COLS.from.at, fit(countryOf(c.from), COLS.from.w), C_MID);
    if (st) {
      put(line, COLS.up.at, right(size(st.ub), 5), st.ub ? C_LABEL : C_LO);
      put(line, COLS.dn.at, right(size(st.db), 5), st.db ? C_LABEL : C_LO);
      put(line, COLS.wr.at, right(st.wr || "-", 2), st.wr ? C_LABEL : C_LO);
    }
  } else if (i === 0) {
    put(line, COLS.handle.at, "nobody yet", C_LO);
  }
  return line;
}

function drawRow(i) {
  v3.console.gotoxy(1, firstRow + i);
  v3.console.print(render(rowLine(i)));
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

// drawSelection paints the current row. Selected, everything inside the
// outer frame becomes one bar: dark-magenta background, high-magenta
// text, the inner column dividers kept inside it; the outer borders at
// columns 1 and 79 stay the frame colour. The background follows the
// foreground, since a foreground code clears it.
function drawSelection(i, on) {
  var c = calls[scroll + i];
  if (!c) {
    return;
  }
  if (!on) {
    drawRow(i);
    parkCursor();
    return;
  }
  v3.console.gotoxy(2, firstRow + i);
  v3.console.print("|13|B5" + spanText(rowLine(i), 2, WIDTH - 1) + "|B0|07");
  parkCursor();
}

// --- per-call figures ----------------------------------------------------

function recordSnapshot() {
  var all = v3.data.get("snaps") || {};
  var id = String(v3.user.id);
  var mine = all[id] || [];
  var last = mine[mine.length - 1];
  if (last && last.t >= v3.session.startTime) {
    return;
  }
  mine.push({
    t: Math.floor(Date.now() / 1000),
    up: v3.user.uploads,
    dn: v3.user.downloads,
    wr: v3.user.messagesPosted,
    ub: v3.user.uploadedBytes,
    db: v3.user.downloadedBytes,
  });
  all[id] = mine.slice(-KEEP_SNAPS);
  v3.data.set("snaps", all);
}

// callStats works out what a caller did on a call: their counts at their
// next logon (or now) against their counts at this call's logon. It
// returns null for a call with no logon snapshot, such as one from
// before the script was installed.
function callStats(c) {
  var mine = snaps[String(c.userID)] || [];
  var start = null,
    end = null;
  for (var i = 0; i < mine.length; i++) {
    var s = mine[i];
    if (s.t >= c.connectTime && s.t <= c.disconnectTime) {
      start = s;
    } else if (s.t > c.disconnectTime && end === null) {
      end = s;
    }
  }
  if (start === null) {
    return null;
  }
  if (end === null) {
    var u = v3.users.getByID(c.userID);
    end = u
      ? {
          up: u.uploads,
          dn: u.downloads,
          wr: u.messagesPosted,
          ub: u.uploadedBytes,
          db: u.downloadedBytes,
        }
      : start;
  }
  function gain(key) {
    return Math.max(0, (end[key] || 0) - (start[key] || 0));
  }
  return {
    up: gain("up"),
    dn: gain("dn"),
    wr: gain("wr"),
    ub: gain("ub"),
    db: gain("db"),
  };
}

// --- input ---------------------------------------------------------------

// drawTwinkle redraws just the star's sparkles and eyes for one frame,
// leaving the rest of the screen alone.
// drawWave rolls a diagonal band of light across the CALLERS letters,
// bottom-left to top-right, oscillating back and forth. A cell's phase is
// its column minus its row, so a line of constant brightness runs on the
// diagonal; the bright centre is a triangle wave that bounces from one
// corner to the other. Each row redraws the nine-column window around its
// own bright column — white core, magenta shoulders, the row's normal
// shade at the edges — so cells restore themselves as the band moves on.
function drawWave(frame) {
  var lo = -(LOGO_BODY - 1),
    hi = LOGO_W - 1;
  var span = hi - lo;
  var t = frame % (2 * span);
  var pc = (t < span ? t : 2 * span - t) + lo; // oscillates lo..hi..lo
  for (var r = 0; r < LOGO_BODY; r++) {
    var jc = pc + r; // bright leading glint for this row
    var jt = jc - 7; // a dimmer second glint trailing behind
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
      // Glinting metal: as a band crosses a cell the edges of the
      // letters flare white where light catches, the solid interior
      // gets a soft sheen, and a point lets off a coloured "ting".
      // The trailing band repeats it fainter. Off both bands a cell
      // falls back to its base shade.
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
  // The logo wave sits behind the caller pop-up, so pause only it while
  // the box is open; the star and the bottom scroller stay clear of the
  // box and keep moving.
  if (!boxOpen) {
    drawWave(frame);
  }
  drawScroller(frame);
  // The sparkles and eyes step a third as fast as the wave.
  var s = (frame / 3) | 0;
  v3.console.gotoxy(6, STAR_ROW + 1);
  v3.console.print(C_HI + SPARKLE[s % SPARKLE.length] + "|07");
  v3.console.gotoxy(6, STAR_ROW + 5);
  v3.console.print(C_MID + SPARKLE[(s + 3) % SPARKLE.length] + "|07");
  // Blink at random, usually one eye at a time and now and then both, so
  // the cadence never feels like a clock.
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

function lightbar() {
  if (calls.length === 0) {
    while (v3.console.readkey(TWINKLE_MS) === "") {
      drawTwinkle(++frame);
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
    if (key === "UP" || key === "8" || key === "a" || key === "A") {
      next = pos > 0 ? pos - 1 : calls.length - 1;
    } else if (key === "DOWN" || key === "2" || key === "z" || key === "Z") {
      next = pos < calls.length - 1 ? pos + 1 : 0;
    } else if (key === " " || key === "\r") {
      showCaller(calls[pos]);
      draw();
      drawSelection(pos - scroll, true);
      continue;
    } else if (key === "q" || key === "Q" || key === "ESC") {
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

// --- caller box ----------------------------------------------------------

function isAlnum(ch) {
  return (
    (ch >= "0" && ch <= "9") ||
    (ch >= "A" && ch <= "Z") ||
    (ch >= "a" && ch <= "z")
  );
}

// boxRenderRow draws one row of the pop-up's cell grid. Every
// non-alphanumeric character — the frame and all the punctuation — is
// drawn white, with a high-magenta glint in the upper-left and
// lower-right corners; letters and digits keep their own colour.
function boxRenderRow(cells, brow, boxH) {
  var out = "", cur = "", last = cells.length - 1;
  for (var i = 0; i < cells.length; i++) {
    var ch = cells[i].c;
    if (ch === " ") {
      out += " ";
      continue;
    }
    var k;
    if (cells[i].f) {
      // The frame is styled like the main table box: dark grey with a
      // high-magenta glint at the upper-left and lower-right corners.
      var ul = brow + i, lr = boxH - 1 - brow + (last - i);
      var d = ul < lr ? ul : lr;
      k = d <= 1 ? "|13" : d <= 3 ? "|05" : "|08";
    } else if (isAlnum(ch)) {
      k = cells[i].k;
    } else {
      k = "|15"; // content punctuation stays white
    }
    if (k !== cur) {
      out += k;
      cur = k;
    }
    out += ch === "|" ? "||" : ch;
  }
  return out + "|07";
}

function date(unix) {
  if (!unix) {
    return "never";
  }
  var d = new Date(unix * 1000);
  return two(d.getMonth() + 1) + "/" + two(d.getDate()) + "/" + d.getFullYear();
}

// showCaller pops a shaded panel up over the current screen, centred, and
// waits for esc (or enter/q). It does not clear: the caller redraws the
// screen underneath once this returns, so the box simply vanishes.
function showCaller(c) {
  var u = v3.users.getByID(c.userID);
  var BW = 38;
  var rows = [];
  function blankRow() {
    var r = [];
    for (var i = 0; i < BW; i++) {
      r.push({ c: " ", k: "|07" });
    }
    return r;
  }
  function bput(r, col, text, colour) {
    text = String(text === undefined || text === null ? "" : text);
    for (var i = 0; i < text.length && col + i < BW; i++) {
      r[col + i] = { c: text.charAt(i), k: colour };
    }
  }
  function rule(left, fill, end) {
    var r = blankRow();
    for (var i = 0; i < BW; i++) {
      r[i] = { c: fill, k: "|15", f: true };
    }
    r[0] = { c: left, k: "|15", f: true };
    r[BW - 1] = { c: end, k: "|15", f: true };
    return r;
  }
  function sides() {
    var r = blankRow();
    r[0] = { c: "|", k: "|15", f: true };
    r[BW - 1] = { c: "|", k: "|15", f: true };
    return r;
  }
  function field(label, value) {
    var r = sides();
    if (label) {
      bput(r, 2, v3.util.padLeft(label, 15), C_MID);
      bput(r, 17, ":", "|15");
      bput(r, 19, fit(value, 17), "|07");
    }
    return r;
  }

  rows.push(rule(".", "-", "."));
  var title = sides();
  bput(title, 2, fit(c.handle, 34), C_HI);
  rows.push(title);
  rows.push(rule("+", "-", "+"));
  if (!u) {
    rows.push(field("", ""));
    var nr = sides();
    bput(nr, 2, fit("No longer on this board.", 34), C_LO);
    rows.push(nr);
  } else {
    rows.push(field("Group/Location", u.location));
    rows.push(field("", ""));
    rows.push(field("First call", date(u.createdAt)));
    rows.push(field("Last call", date(u.lastLogin)));
    rows.push(field("Total calls", u.timesCalled));
    rows.push(field("", ""));
    rows.push(field("Uploads", u.uploads + " files, " + size(u.uploadedBytes)));
    rows.push(
      field("Downloads", u.downloads + " files, " + size(u.downloadedBytes)),
    );
    rows.push(field("Messages", u.messagesPosted));
  }
  rows.push(field("", ""));
  rows.push(field("This call from", countryOf(c.from) || "?"));
  // The did-column legend.
  rows.push(rule("+", "-", "+"));
  var leg = sides();
  bput(leg, 2, "did:  D down   U up   W wrote", C_MID);
  rows.push(leg);
  var bottom = rule("`", "-", "'");
  bput(bottom, 15, "[ esc ]", "|07");
  rows.push(bottom);

  var boxH = rows.length;
  var left = (((WIDTH - BW) / 2) | 0) + 1;
  var top = Math.max(1, ((v3.console.height - boxH) / 2) | 0);
  boxOpen = true;
  for (var i = 0; i < boxH; i++) {
    v3.console.gotoxy(left, top + i);
    v3.console.print(boxRenderRow(rows[i], i, boxH));
  }
  parkCursor();
  // Wait for esc/enter/q, animating the star and scroller around the box
  // on each idle tick. A blank key with no timeout means a disconnect.
  for (;;) {
    var k = v3.console.readkey(TWINKLE_MS);
    if (k === "") {
      drawTwinkle(++frame);
      continue;
    }
    if (k === "ESC" || k === "\r" || k === "q" || k === "Q") {
      break;
    }
  }
  boxOpen = false;
}
