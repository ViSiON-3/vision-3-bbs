// mainmenu.js — an animated main menu in the amilc / 1liners style.
//
// A script-driven menu: the BBS hands the menu's options in v3.menu.items
// ({key,label,activity}); this script groups them into sections, draws them
// in a framed panel with the dotted PMS logo lower-left and the star mascot
// tucked into the lower-right corner, and reports the caller's pick with
// v3.prompt.answer(key). The key is dispatched through the menu's .CFG exactly
// like a keypress, so GOTO/RUN/DOOR all work unchanged.
//
// Wire it on a menu by naming it in the menu's .MNU, as in
// "SCRIPT": "mainmenu.js", and giving each command to show a short "LABEL". Arrow keys / 8 2 4 6 move the bar;
// ENTER opens the highlighted option; any option's hotkey opens it directly.

// pms-ui begin
// PMS house style: the palette, the dashed frame and its glint, the edge-lit
// title and the two-line footer. A V3 script cannot load another file, so
// every PMS script carries a copy of this block. The source is
// menus/pms/scripts/lib/pms-ui.js: edit it there and run
// scripts/sync-pms-ui.sh to paste it into every script.
// TestPMSUIRegionsMatchLib fails while a copy differs from the source.
var PMS = (function () {
  var con = v3.console;

  // Palette.
  var HI = "|13", MID = "|05", LO = "|08", LABEL = "|07", WHITE = "|15";
  var BAR = "|13|B5", BAR_END = "|B0|07";
  var BOARD = "pirate mind station";

  // W is the drawing width: the terminal's, up to 80 columns.
  var W = con.width >= 80 ? 80 : con.width;

  function two(n) { return (n < 10 ? "0" : "") + n; }

  // glintColor is the glint at d cells behind its head: white, cooling
  // through magenta to the frame's grey.
  function glintColor(d) { return d === 0 ? WHITE : d <= 1 ? HI : d <= 3 ? LABEL : d <= 5 ? MID : LO; }

  // frame is the dark grey dashed panel from row top to row bottom across
  // the drawing width. draw() paints it. glint() is called once a frame: now
  // and then it starts a glint that runs around the panel, and it moves a
  // running glint on, restoring the grey behind its tail. oneColour writes
  // each edge row's colour once, as mainmenu always has; without it the
  // colour is written again before the closing corner. Both look the same.
  function frame(top, bottom, oneColour) {
    var perim = [], c, r, pos = -1, TAIL = 6, STEP = 3;
    for (c = 1; c <= W; c++) perim.push({ r: top, c: c, ch: c === 1 || c === W ? "." : "-" });
    for (r = top + 1; r < bottom; r++) perim.push({ r: r, c: W, ch: "|" });
    for (c = W; c >= 1; c--) perim.push({ r: bottom, c: c, ch: c === 1 ? "`" : c === W ? "'" : "-" });
    for (r = bottom - 1; r > top; r--) perim.push({ r: r, c: 1, ch: "|" });
    function paint(i, k) {
      var p = perim[i];
      con.gotoxy(p.c, p.r);
      con.print(k + (p.ch === "|" ? "||" : p.ch) + LABEL);
    }
    function edge(left, end) {
      var o = LO + left;
      for (var i = 2; i < W; i++) o += "-";
      return o + (oneColour ? "" : LO) + end;
    }
    return {
      draw: function () {
        con.gotoxy(1, top); con.print(edge(".", "."));
        for (var row = top + 1; row < bottom; row++) {
          con.gotoxy(1, row); con.print(LO + "||");
          con.gotoxy(W, row); con.print(LO + "||");
        }
        con.gotoxy(1, bottom); con.print(edge("`", "'"));
      },
      glint: function () {
        if (pos < 0 && Math.random() < 0.004) pos = 0;
        if (pos < 0) return;
        var last = perim.length - 1, lo = pos - STEP - TAIL - 1, hi = pos;
        if (lo < 0) lo = 0;
        if (hi > last) hi = last;
        for (var i = lo; i <= hi; i++) {
          var d = pos - i;
          paint(i, d >= 0 && d <= TAIL ? glintColor(d) : LO);
        }
        pos += STEP;
        if (pos - TAIL > last) {
          for (var j = Math.max(0, last - TAIL); j <= last; j++) paint(j, LO);
          pos = -1;
        }
      },
    };
  }

  function titleWidth(rows) {
    var w = 0;
    for (var i = 0; i < rows.length; i++) if (rows[i].length > w) w = rows[i].length;
    return w;
  }

  // title draws figlet lettering with its top row at (left, top), lit from
  // the upper left: a stroke cell open above and to its left is bright
  // magenta, one open below and to its right falls to dark grey, and the
  // rest is magenta.
  function title(rows, left, top) {
    function ink(r, c) {
      if (r < 0 || r >= rows.length || c < 0) return false;
      return c < rows[r].length && rows[r].charAt(c) !== " ";
    }
    for (var i = 0; i < rows.length; i++) {
      var row = rows[i], o = "", cur = "";
      for (var j = 0; j < row.length; j++) {
        var ch = row.charAt(j);
        if (ch === " ") { o += " "; continue; }
        var lit = !ink(i - 1, j) && !ink(i, j - 1);
        var shadow = !ink(i + 1, j) && !ink(i, j + 1);
        var col = lit ? HI : shadow ? LO : MID;
        if (col !== cur) { o += col; cur = col; }
        o += ch === "|" ? "||" : ch;
      }
      con.gotoxy(left, top + i);
      con.print(o + LABEL);
    }
  }

  // brand writes the board name dim at the right end of row, inside the frame.
  function brand(row) {
    con.gotoxy(W - 1 - BOARD.length, row);
    con.print(LO + BOARD + LABEL);
  }

  // visWidth is a pipe-coded string's visible width, and centreAt the
  // column a string that wide starts at when centred.
  function visWidth(piped) { return v3.util.stripPipe(String(piped)).length; }
  function centreAt(vis) { var at = (((W - vis) / 2) | 0) + 1; return at < 1 ? 1 : at; }

  // centre prints a pipe-coded string centred on row by its visible width.
  function centre(row, piped) {
    con.gotoxy(centreAt(visWidth(piped)), row);
    con.print(piped + LABEL);
  }

  // MENU_KEYS is the key hint under a menu.
  var MENU_KEYS = "|07arrows|08 move     |07enter|08 open     |07hot-key|08 jumps";

  // footer is the two lines under every framed screen, two and three rows
  // below the frame's bottom edge: the key hint, then the status line. The
  // row between is the screen's own, for prompts and notices. The status
  // line is the time, then mid (the date when mid is empty), the minutes
  // left and the node. status() redraws only when the text changes, so the
  // clock ticks without flicker, and blanks the old line first when the new
  // one is narrower. after, when given, runs after each line is drawn.
  function footer(frameBottom, after) {
    var hintRow = frameBottom + 2, statusRow = frameBottom + 3, last = "";
    function statusText(mid) {
      var d = new Date(), left = v3.session.timeLeft, lm = left > 0 ? (left / 60) | 0 : 0;
      if (!mid) mid = "|15" + two(d.getMonth() + 1) + "." + two(d.getDate()) + "." + d.getFullYear();
      return "|08[ |15" + two(d.getHours()) + ":" + two(d.getMinutes()) + "|08 ]     " + mid +
        "|08     |15" + lm + "|08 min left     node |15" + v3.session.node;
    }
    return {
      hintRow: hintRow,
      statusRow: statusRow,
      hint: function (piped) {
        centre(hintRow, piped);
        if (after) after();
      },
      status: function (mid, force) {
        var text = statusText(mid);
        if (!force && text === last) return;
        var was = visWidth(last);
        if (visWidth(text) < was) {
          con.gotoxy(centreAt(was), statusRow);
          con.write(new Array(was + 1).join(" "));
        }
        last = text;
        centre(statusRow, text);
        if (after) after();
      },
    };
  }

  return {
    HI: HI, MID: MID, LO: LO, LABEL: LABEL, WHITE: WHITE, BAR: BAR, BAR_END: BAR_END,
    BOARD: BOARD, W: W, MENU_KEYS: MENU_KEYS,
    two: two, frame: frame, title: title, titleWidth: titleWidth, brand: brand,
    centre: centre, footer: footer,
  };
})();
// pms-ui end

var ITEMS = v3.menu.items || [];
var SHORTCUTS = v3.menu.shortcuts || [];

// Sections and which command keys belong to each, laid out in three
// columns. Items are sorted alphabetically by label within a section.
var COLUMNS = [
  ["Msgs & Files", "Account"],
  ["Community"],
  ["System"],
];
var SECTIONS = {
  "Msgs & Files": ["E", "M", "N", "D", "T"],
  Community: ["B", "C", "W", "L", "O", "R", "Y", "/V", "H", "/W", "I"],
  Account: ["K", "+", "U", "V", "^"],
  System: ["G", "/G", "P", "S", "X"],
};

// "PMS" in the AMC AAA01 figlet shape, transliterated to dots.
var LOGO = [
  " .:..::.     .:.:.:.:.     .::.",
  ".::..:;;.   .::.:;:.::.   .;;:.",
  ":;:   .:;.  :;: ..' :;:  .;:'",
  ":;:    :;:  :;:     :;:  :;:",
  ":;:    .;:  :;:     :;:  :;:",
  ":;:   .:;:  :;:     :;:  .;:.",
  ":;:...:::   :;:     :;:  .:;;:",
  ":;:..::.    :;:     :;:    .:;:",
  ":;:         :;:     :;:     :;:",
  ":;:         :;:     :;:    .:;.",
  ":;:         :;:     :;:  .::;:",
  ":;:         :::     :;:  .::'",
  ":.                  :.",
  ".                   .",
];
var LOGO_BODY = LOGO.length - 2;
var LOGO_W = 0;
for (var _lw = 0; _lw < LOGO.length; _lw++) {
  if (LOGO[_lw].length > LOGO_W) {
    LOGO_W = LOGO[_lw].length;
  }
}

var STAR = [
  "       /\\",
  "\\\\ ___/oO\\___",
  " \\\\ \\   -   /",
  " // /___ ___\\",
  "        \\/",
];
var TWINKLE_MS = 66;
var SPARKLE = ["*", "+", ".", " ", ".", "+"];
var TING = ["|15", "|11", "|15", "|13"];

var W = PMS.W;
var H = v3.console.height;

// On a terminal that draws sixel the logo is the graffiti picture LOGO_ART
// (in the theme's data/), sized to the cells the text logo covers at 8x16 pixels
// a cell. Any other terminal, or a picture that fails to draw, gets the
// text logo.
var LOGO_ART = "pms_graffiti.png";
var GFX = !!v3.gfx && v3.gfx.sixel;


var C_HI = PMS.HI,
  C_MID = PMS.MID,
  C_LO = PMS.LO,
  C_LABEL = PMS.LABEL,
  C_HEAD = "|11";

// --- geometry ------------------------------------------------------------
// Centre the panel vertically in the terminal, its footer below it.
var boxTop = Math.max(2, (((H - (LOGO_BODY + 4)) / 2) | 0) + 1);
var boxBottom = boxTop + LOGO_BODY + 1;
var logoTop = boxTop + 1;
var LOGO_COL = 3;
var menuLeft = LOGO_COL + LOGO_W + 2;
var menuRows = LOGO_BODY;
var menuCols = COLUMNS.length;
var colW = Math.floor((W - 1 - menuLeft) / menuCols);
var panel = PMS.frame(boxTop, boxBottom, true);
var footer = PMS.footer(boxBottom, parkCursor);

// The star sits in the lower-right, inside the frame, under the last column.
var STAR_COL = menuLeft + (menuCols - 1) * colW;
if (STAR_COL + 13 > W - 1) {
  STAR_COL = W - 14;
}
var starTop = boxBottom - STAR.length;
var EYES_COL = STAR_COL + 7;
var MOUTH_COL = STAR_COL + STAR[2].indexOf("-");
var MOUTH_SHAPES = ["^", "o", "~", ".", "O", "v"];
var mouth = "-",
  mouthHold = 0;

// --- build the placed layout --------------------------------------------
// cols[ci] is a list of {key,label,row} (screen row); rows[] interleaves
// headers and items for drawing.
var cols = [];
var drawList = [];

(function buildLayout() {
  function labelFor(k) {
    for (var i = 0; i < ITEMS.length; i++) {
      if (String(ITEMS[i].key) === k) {
        return String(ITEMS[i].label || ITEMS[i].activity || k);
      }
    }
    return null;
  }
  var seen = {};
  for (var ci = 0; ci < COLUMNS.length; ci++) {
    cols[ci] = [];
    var row = 0;
    var colX = menuLeft + ci * colW;
    var secs = COLUMNS[ci];
    for (var si = 0; si < secs.length; si++) {
      var title = secs[si];
      var keys = SECTIONS[title] || [];
      // resolve to present items, alphabetical by label
      var entries = [];
      for (var ki = 0; ki < keys.length; ki++) {
        var lab = labelFor(keys[ki]);
        if (lab !== null) {
          entries.push({ key: keys[ki], label: lab });
          seen[keys[ki]] = true;
        }
      }
      if (entries.length === 0) {
        continue;
      }
      entries.sort(function (a, b) {
        return a.label < b.label ? -1 : a.label > b.label ? 1 : 0;
      });
      // the last column leaves room for the star at the bottom
      if (ci === menuCols - 1 && logoTop + row + 1 + entries.length > starTop - 1) {
        // still draw; the header + items will sit above the star
      }
      drawList.push({ head: title, col: colX, row: logoTop + row });
      row++;
      for (var e = 0; e < entries.length; e++) {
        var cell = { key: entries[e].key, label: entries[e].label, col: colX, row: logoTop + row };
        cols[ci].push(cell);
        drawList.push(cell);
        row++;
      }
    }
  }
  // any labelled item not placed in a section: ignore (keys still work).
})();

var ci = 0;
var ri = 0;
var frame = 0;
var blinkL = 0,
  blinkR = 0;

// pick the first non-empty column
while (ci < cols.length && cols[ci].length === 0) {
  ci++;
}
if (ci >= cols.length) {
  ci = 0;
}

v3.console.print("\x1b[?25l"); // hide the cursor for a steady redraw
draw();
lightbar();
v3.console.print("\x1b[?25h");

// --- cell/line helpers ---------------------------------------------------

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

function logoEdge(r, c) {
  return logoInk(r, c) && logoInkCount(r, c) < 4;
}

function logoTip(r, c) {
  return logoInk(r, c) && logoInkCount(r, c) <= 1;
}

function parkCursor() {
  v3.console.gotoxy(W + 1, footer.hintRow);
}

// --- static draw ---------------------------------------------------------

function drawStarRow(sidx) {
  var text = STAR[sidx];
  var out = "",
    cur = "";
  for (var i = 0; i < text.length; i++) {
    var ch = text.charAt(i);
    if (ch === " ") {
      out += " ";
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
    if (col !== cur) {
      out += col;
      cur = col;
    }
    out += ch;
  }
  v3.console.gotoxy(STAR_COL, starTop + sidx);
  v3.console.print(out + "|07");
}

function drawStar() {
  for (var s = 0; s < STAR.length; s++) {
    drawStarRow(s);
  }
}

function drawLogoRow(r) {
  var text = LOGO[r];
  var out = "",
    cur = "";
  for (var i = 0; i < text.length; i++) {
    var ch = text.charAt(i);
    if (ch === " ") {
      out += " ";
      continue;
    }
    var col = logoColour(ch, r);
    if (col !== cur) {
      out += col;
      cur = col;
    }
    out += ch === "|" ? "||" : ch;
  }
  v3.console.gotoxy(LOGO_COL, logoTop + r);
  v3.console.print(out + "|07");
}

function clip(s, w) {
  s = v3.util.stripPipe(String(s === undefined || s === null ? "" : s));
  if (s.length > w) {
    s = s.substring(0, w);
  }
  while (s.length < w) {
    s += " ";
  }
  return s;
}

// cellWidth is the width of a menu cell starting at col: a column's width
// less the gap before the next, and for the last column everything up to
// the column before the frame.
function cellWidth(col) {
  return col >= menuLeft + (menuCols - 1) * colW ? W - 1 - col : colW - 1;
}

function drawItem(cell, selected) {
  var cellW = cellWidth(cell.col);
  var key = String(cell.key);
  var text = " " + key + " " + v3.util.stripPipe(cell.label);
  v3.console.gotoxy(cell.col, cell.row);
  if (selected) {
    v3.console.print(PMS.BAR + clip(text, cellW) + PMS.BAR_END);
  } else {
    // " K " before the label: a key of two characters, as /G, takes one more.
    v3.console.print(" |13" + key + "|07 " + clip(cell.label, cellW - key.length - 2) + "|07");
  }
}

// drawHead shades a section title with a soft centre-shine: dark magenta at
// the ends, bright magenta on the shoulders, white through the middle.
function drawHead(h) {
  var word = h.head;
  var mid = (word.length - 1) / 2;
  var out = "",
    cur = "";
  for (var i = 0; i < word.length; i++) {
    var dist = mid > 0 ? Math.abs(i - mid) / mid : 0;
    var col = dist < 0.34 ? "|15" : dist < 0.72 ? "|13" : "|05";
    if (col !== cur) {
      out += col;
      cur = col;
    }
    out += word.charAt(i);
  }
  v3.console.gotoxy(h.col, h.row);
  v3.console.print(out + "|07");
}

function draw() {
  v3.console.clear();
  panel.draw();
  if (GFX) {
    v3.console.gotoxy(LOGO_COL, logoTop);
    GFX = v3.gfx.image(LOGO_ART);
  }
  if (!GFX) {
    for (var r = 0; r < LOGO_BODY; r++) {
      drawLogoRow(r);
    }
  }
  for (var d = 0; d < drawList.length; d++) {
    if (drawList[d].head) {
      drawHead(drawList[d]);
    } else {
      drawItem(drawList[d], false);
    }
  }
  drawStar();
  drawFooter();
  drawStatus(true);
}

// drawFooter is the static movement hint.
function drawFooter() {
  footer.hint(PMS.MENU_KEYS);
}

// drawStatus redraws the status line when its text changes (the minute or
// the time left flips).
function drawStatus(force) {
  footer.status("", force);
}

// --- animation -----------------------------------------------------------

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
    v3.console.gotoxy(LOGO_COL + a, logoTop + r);
    v3.console.print(out);
  }
}

function drawTwinkle(frame) {
  // The wave repaints the text logo's cells, which would wipe the picture.
  if (!GFX) {
    drawWave(frame);
  }
  var s = (frame / 3) | 0;
  v3.console.gotoxy(STAR_COL + 5, starTop - 1);
  v3.console.print(C_HI + SPARKLE[s % SPARKLE.length] + "|07");
  // The lower sparkle sits on the star's last row, left of its tip: the row
  // below the star is the frame's bottom border.
  v3.console.gotoxy(STAR_COL + 5, starTop + STAR.length - 1);
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
  v3.console.gotoxy(EYES_COL, starTop + 1);
  v3.console.print("|15" + (blinkL > 0 ? "-" : "o") + (blinkR > 0 ? "-" : "O"));
  if (blinkL > 0) {
    blinkL--;
  }
  if (blinkR > 0) {
    blinkR--;
  }
  // The mouth pulls a different face now and then, then settles back to "-".
  if (mouthHold <= 0) {
    if (Math.random() < 0.04) {
      mouth = MOUTH_SHAPES[(Math.random() * MOUTH_SHAPES.length) | 0];
      mouthHold = 3 + ((Math.random() * 5) | 0);
    }
  } else {
    mouthHold--;
    if (mouthHold <= 0) {
      mouth = "-";
    }
  }
  v3.console.gotoxy(MOUTH_COL, starTop + 2);
  v3.console.print(C_HI + mouth + "|07");
  panel.glint();
  drawStatus(false);
  parkCursor();
}

// --- input ---------------------------------------------------------------

function hotkeyItem(k) {
  var up = k.toUpperCase();
  for (var c = 0; c < cols.length; c++) {
    for (var r = 0; r < cols[c].length; r++) {
      if (String(cols[c][r].key).toUpperCase() === up) {
        return cols[c][r];
      }
    }
  }
  return null;
}

function choose(cell) {
  v3.console.print("|B0|07\x1b[?25h");
  v3.console.gotoxy(1, H);
  v3.prompt.answer(cell.key);
}

// Match authorized keys independently of which commands have visible labels.
// Exact keys win over prefixes, as they do in the native hotkey reader.
function commandKey(first) {
  if (first.length !== 1) return "";
  var key = first.toUpperCase();
  for (var read = 0; read < 32; read++) {
    var prefix = false;
    for (var i = 0; i < ITEMS.length; i++) {
      if (String(ITEMS[i].key).toUpperCase() === key) return key;
    }
    for (var j = 0; j < SHORTCUTS.length; j++) {
      var candidate = String(SHORTCUTS[j]).toUpperCase();
      if (candidate === key) return key;
      if (candidate.indexOf(key) === 0) prefix = true;
    }
    if (!prefix) return "";
    var next = v3.console.readkey(500);
    if (next.length !== 1 || next === "\r" || next === "ESC") return "";
    key += next.toUpperCase();
  }
  return "";
}

function lightbar() {
  var total = 0;
  for (var c = 0; c < cols.length; c++) {
    total += cols[c].length;
  }
  if (total === 0) {
    return;
  }
  drawItem(cols[ci][ri], true);
  for (;;) {
    var key = v3.console.readkey(TWINKLE_MS);
    if (key === "") {
      drawTwinkle(++frame);
      continue;
    }
    var command = commandKey(key);
    if (command) { choose({key: command}); return; }
    var nci = ci,
      nri = ri;
    if (key === "UP" || key === "8") {
      nri = ri > 0 ? ri - 1 : cols[ci].length - 1;
    } else if (key === "DOWN" || key === "2") {
      nri = ri < cols[ci].length - 1 ? ri + 1 : 0;
    } else if (key === "LEFT" || key === "4") {
      nci = ci;
      for (var lc = ci - 1; lc >= 0; lc--) {
        if (cols[lc].length > 0) {
          nci = lc;
          break;
        }
      }
      nri = Math.min(ri, cols[nci].length - 1);
    } else if (key === "RIGHT" || key === "6") {
      nci = ci;
      for (var rc = ci + 1; rc < cols.length; rc++) {
        if (cols[rc].length > 0) {
          nci = rc;
          break;
        }
      }
      nri = Math.min(ri, cols[nci].length - 1);
    } else if (key === "\r" || key === " ") {
      choose(cols[ci][ri]);
      return;
    } else {
      var hit = hotkeyItem(key);
      if (hit) {
        choose(hit);
        return;
      }
      continue;
    }
    if (nci === ci && nri === ri) {
      continue;
    }
    drawItem(cols[ci][ri], false);
    ci = nci;
    ri = nri;
    drawItem(cols[ci][ri], true);
    parkCursor();
  }
}
