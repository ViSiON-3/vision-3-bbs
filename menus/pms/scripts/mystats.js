// mystats.js — an on-theme personal stats screen (replaces YOURSTAT.ANS).
//
// Draws the caller's stats in the PMS section-menu style — a
// graffiti STATS title, a small spinning blue/teal morph shape, a data
// grid, a couple of bar meters, the dark frame with its metal glint, the
// star mascot in the lower-left, and the board name tucked lower-right.
// Any key continues.
//
// The theme's mnu/YOURSTAT.MNU names it, so the stats screen (USERSTATS in
// login.json, RUN:SHOWSTATS on a menu) is drawn with it for PMS callers:
//   { "TITLE": "Your Stats", "SCRIPT": "mystats.js" }

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

var TITLE = [
  "  _____________________________________________",
  " /   _____/\\__    ___/  _  \\__    ___/   _____/",
  " \\_____  \\   |    | /  /_\\  \\|    |  \\_____  \\",
  " /        \\  |    |/    |    \\    |  /        \\",
  "/_______  /  |____|\\____|__  /____| /_______  /",
  "        \\/                 \\/               \\/",
];

var W = PMS.W;
var H = v3.console.height;
var FRAME_MS = 66;

// Blue -> teal -> cyan depth ramp for the shape.
var SHADE = [
  "\x1b[38;2;12;34;72m", "\x1b[38;2;20;78;128m", "\x1b[38;2;26;140;160m",
  "\x1b[38;2;40;196;206m", "\x1b[38;2;120;236;236m", "\x1b[38;2;208;255;255m"];
var EDGES = [[0, 1], [2, 3], [4, 5], [6, 7], [0, 2], [1, 3], [4, 6], [5, 7],
  [0, 4], [1, 5], [2, 6], [3, 7]];
var CUBE_V = (function () { var v = []; for (var i = 0; i < 8; i++) v.push({ x: (i & 1) * 2 - 1, y: ((i >> 1) & 1) * 2 - 1, z: ((i >> 2) & 1) * 2 - 1 }); return v; })();

// The star mascot — the little guy from the main menu, lower-left.
var STAR = [
  "       /\\",
  "\\\\ ___/oO\\___",
  " \\\\ \\   -   /",
  " // /___ ___\\",
  "        \\/",
];
var C_HI = PMS.HI, C_LO = PMS.LO;
var MOUTH_SHAPES = ["^", "o", "~", ".", "O", "v"];
var blinkL = 0, blinkR = 0, mouth = "-", mouthHold = 0;

// --- gather stats --------------------------------------------------------
var u = v3.user;
var full = v3.users && v3.users.getByID ? v3.users.getByID(u.id) : null;

function two(n) { return (n < 10 ? "0" : "") + n; }
function dateOf(unix) {
  if (!unix) return "never";
  var d = new Date(unix * 1000);
  return d.getFullYear() + "|08.|07" + two(d.getMonth() + 1) + "|08.|07" + two(d.getDate());
}
function size(b) {
  if (!b) return "0";
  var un = ["K", "M", "G", "T"], v = b / 1024;
  for (var i = 0; i < un.length; i++) {
    if (v < 1024 || i === un.length - 1) return (v < 10 && i > 0 ? v.toFixed(1) : "" + Math.round(v)) + un[i];
    v /= 1024;
  }
}
function title(lvl) {
  return lvl >= 255 ? "SysOp" : lvl >= 200 ? "Co-SysOp" : lvl >= 100 ? "Elite" : lvl >= 50 ? "Member" : "New User";
}

var ulb = u.uploadedBytes || 0, dlb = u.downloadedBytes || 0;
var ratio = dlb > 0 ? ulb / dlb : (ulb > 0 ? 9.99 : 0);
var joined = full ? full.createdAt : 0;
var lastOn = full ? (full.previousLogin || full.lastLogin) : 0;
var days = joined ? Math.max(1, ((Date.now() / 1000 - joined) / 86400) | 0) : 0;

// label / value rows, placed in two columns.
var ROWS = [
  ["handle", "|15" + u.handle],
  ["level", "|15" + u.accessLevel + " |08/ |13" + title(u.accessLevel)],
  ["group", "|07" + (u.location || "-")],
  ["joined", "|07" + dateOf(joined) + " |08(|07" + days + "|08d)"],
  ["last on", "|07" + dateOf(lastOn)],
  ["calls", "|15" + u.timesCalled],
  ["posts", "|15" + u.messagesPosted],
  ["uploads", "|15" + u.uploads + " |08files |07" + size(ulb)],
  ["downloads", "|15" + u.downloads + " |08files |07" + size(dlb)],
  ["points", "|15" + u.filePoints],
];

// --- geometry (centred) --------------------------------------------------
// The data block is indented to leave a left margin the star stands in.
var titleLeft = 3, labelLeft = 18;
var titleW = PMS.titleWidth(TITLE);
var rowsPer = Math.ceil(ROWS.length / 2);
var totalH = 14 + rowsPer;
var boxTop = Math.max(2, (((H - totalH) / 2) | 0) + 1);
var titleTop = boxTop + 1;
var gridTop = titleTop + TITLE.length + 1;
var meterRow = gridTop + rowsPer + 1;
var boxBottom = meterRow + 2;
var panel = PMS.frame(boxTop, boxBottom);
var footer = PMS.footer(boxBottom);

var colW = ((W - labelLeft - 2) / 2) | 0;
var SLEFT = titleLeft + titleW + 2;
var SW = Math.max(10, W - 2 - SLEFT);
var STOP = titleTop;
var SH = Math.min(TITLE.length + 1, boxBottom - 1 - titleTop);

// star position — lower-left, inside the frame
var STAR_COL = 2;
var starTop = boxBottom - STAR.length;
var EYES_COL = STAR_COL + 7;
var MOUTH_COL = STAR_COL + STAR[2].indexOf("-");

var prevShape = {}, shapeStart = Date.now();

v3.console.print("\x1b[?25l");
draw();
pause();
v3.console.print("\x1b[?25h\x1b[0m");
// Run as YOURSTAT.MNU's script, the screen is done when the caller is.
if (v3.prompt && v3.prompt.kind === "menu") v3.prompt.answer("Q");

// --- shape ---------------------------------------------------------------
function clamp(v, a, b) { return v < a ? a : v > b ? b : v; }
function morphVerts(m) {
  var v = [];
  for (var i = 0; i < 8; i++) { var p = CUBE_V[i], x = p.x, z = p.z; if (p.y < 0) { x = x * (1 - m); z = z * (1 - m); } v.push({ x: x, y: p.y, z: z }); }
  return v;
}
function shade(x, y, z) {
  var len = Math.sqrt(x * x + y * y + z * z); if (len === 0) return 0;
  var nx = x / len, ny = y / len, nz = z / len;
  var near = clamp((1.75 - z) / 3.5, 0, 1);
  var diffuse = Math.max(0, -0.35 * nx - 0.5 * ny - 0.792 * nz);
  var spec = Math.pow(Math.max(0, -0.185 * nx - 0.264 * ny - 0.947 * nz), 18);
  return clamp(Math.floor((0.16 + 0.48 * near + 0.2 * diffuse + 0.3 * spec) * SHADE.length), 0, SHADE.length - 1);
}
function shapeCells(t) {
  var sx = Math.sin(t * 0.43 + 0.3), cx = Math.cos(t * 0.43 + 0.3), sy = Math.sin(t * 0.61 + 0.5), cy = Math.cos(t * 0.61 + 0.5), sz = Math.sin(t * 0.19 - 0.2), cz = Math.cos(t * 0.19 - 0.2);
  var scale = Math.min(SW / 2, SH) * 0.27, V = morphVerts((1 - Math.cos(t * 0.5)) / 2), verts = [], world = [];
  for (var i = 0; i < V.length; i++) {
    var x = V[i].x, y = V[i].y, z = V[i].z;
    var ny = y * cx - z * sx, nz = y * sx + z * cx; y = ny; z = nz;
    var nx = x * cy + z * sy; nz = -x * sy + z * cy; x = nx; z = nz;
    nx = x * cz - y * sz; ny = x * sz + y * cz; x = nx; y = ny;
    world[i] = { x: x, y: y, z: z }; var d = 4 / (4.5 + z);
    verts[i] = { x: SLEFT + (SW - 1) / 2 + x * d * scale * 2, y: STOP - 1 + (SH - 1) / 2 + y * d * scale, z: z };
  }
  var sub = SW * SH * 2, zbuf = [], cbuf = []; for (var k = 0; k < sub; k++) { zbuf[k] = Infinity; cbuf[k] = 0; }
  function plot(px, py, z, col) { var lx = px - SLEFT, ly = py - (STOP - 1) * 2; if (lx < 0 || lx >= SW || ly < 0 || ly >= SH * 2) return; var at = (((ly >> 1) * SW) + lx) * 2 + (ly & 1); if (z <= zbuf[at]) { zbuf[at] = z; cbuf[at] = col; } }
  for (var e = 0; e < EDGES.length; e++) {
    var a = verts[EDGES[e][0]], b = verts[EDGES[e][1]], ax = Math.round(a.x), ay = Math.round(a.y * 2), bx = Math.round(b.x), by = Math.round(b.y * 2);
    var steps = Math.max(1, Math.max(Math.abs(bx - ax), Math.abs(by - ay))), wa = world[EDGES[e][0]], wb = world[EDGES[e][1]];
    for (var s = 0; s <= steps; s++) { var uu = s / steps, denom = (1 - uu) / (4.5 + a.z) + uu / (4.5 + b.z), zz = 1 / denom - 4.5, vv = (uu / (4.5 + b.z)) / denom; plot(ax + Math.round(uu * (bx - ax)), ay + Math.round(uu * (by - ay)), zz, shade(wa.x + (wb.x - wa.x) * vv, wa.y + (wb.y - wa.y) * vv, zz)); }
  }
  var cells = {};
  for (var row = 0; row < SH; row++) for (var col = 0; col < SW; col++) {
    var base = ((row * SW) + col) * 2, up = zbuf[base], lo = zbuf[base + 1]; if (up === Infinity && lo === Infinity) continue;
    var ch, ci; if (up === Infinity) { ch = "."; ci = cbuf[base + 1]; } else if (lo === Infinity) { ch = "'"; ci = cbuf[base]; } else { ch = ":"; ci = lo < up ? cbuf[base + 1] : cbuf[base]; }
    cells[(STOP + row) * 1000 + (SLEFT + col + 1)] = SHADE[ci] + ch;
  }
  return cells;
}
function drawShape(t) {
  var cells = shapeCells(t), k;
  for (k in cells) if (cells[k] !== prevShape[k]) { v3.console.gotoxy(k % 1000, (k / 1000) | 0); v3.console.print(cells[k]); }
  for (k in prevShape) if (cells[k] === undefined) { v3.console.gotoxy(k % 1000, (k / 1000) | 0); v3.console.print(" "); }
  prevShape = cells; v3.console.print("|07");
}

// --- star ----------------------------------------------------------------
function drawStarRow(sidx) {
  var text = STAR[sidx], out = "", cur = "";
  for (var i = 0; i < text.length; i++) {
    var ch = text.charAt(i);
    if (ch === " ") { out += " "; continue; }
    var col = (ch === "o" || ch === "O" || ch === "-") ? C_HI : ch === "\\" ? C_LO : "|15";
    if (col !== cur) { out += col; cur = col; }
    out += ch;
  }
  v3.console.gotoxy(STAR_COL, starTop + sidx); v3.console.print(out + "|07");
}
function drawStar() { for (var s = 0; s < STAR.length; s++) drawStarRow(s); }
function animStar() {
  if (blinkL <= 0 && blinkR <= 0 && Math.random() < 0.07) {
    var dur = 2 + ((Math.random() * 2) | 0), pick = Math.random();
    if (pick < 0.45) blinkL = dur; else if (pick < 0.9) blinkR = dur; else { blinkL = dur; blinkR = dur; }
  }
  v3.console.gotoxy(EYES_COL, starTop + 1);
  v3.console.print("|15" + (blinkL > 0 ? "-" : "o") + (blinkR > 0 ? "-" : "O") + "|07");
  if (blinkL > 0) blinkL--;
  if (blinkR > 0) blinkR--;
  if (mouthHold <= 0) {
    if (Math.random() < 0.04) { mouth = MOUTH_SHAPES[(Math.random() * MOUTH_SHAPES.length) | 0]; mouthHold = 3 + ((Math.random() * 5) | 0); }
  } else { mouthHold--; if (mouthHold <= 0) mouth = "-"; }
  v3.console.gotoxy(MOUTH_COL, starTop + 2); v3.console.print(C_HI + mouth + "|07");
}

// --- content -------------------------------------------------------------
function drawRow(i) {
  var c = (i / rowsPer) | 0, r = i % rowsPer, col = labelLeft + c * colW, row = gridTop + r;
  var lab = ROWS[i][0];
  while (lab.length < 10) lab += " ";
  v3.console.gotoxy(col, row);
  v3.console.print("|08" + lab + ROWS[i][1] + "|07");
}
function bar(frac, width) {
  frac = Math.max(0, Math.min(1, frac)); var fill = Math.round(frac * width), o = "|08[|11";
  for (var i = 0; i < width; i++) { if (i === fill) o += "|08"; o += i < fill ? "=" : "."; }
  return o + "|08]|07";
}
function draw() {
  v3.console.clear();
  panel.draw();
  PMS.title(TITLE, titleLeft, titleTop);
  for (var i = 0; i < ROWS.length; i++) drawRow(i);
  // U/D ratio meter
  v3.console.gotoxy(labelLeft, meterRow);
  v3.console.print("|08up/dn  " + bar(ratio / (ratio + 1), 24) + "  |15" + (ratio >= 9.99 ? "9.9+" : ratio.toFixed(1)) + " |08: 1   |08left |15" + (((v3.session.timeLeft || 0) / 60) | 0) + "|08m");
  PMS.brand(boxBottom - 1);
  drawStar();
  drawShape((Date.now() - shapeStart) / 1000);
  footer.hint("|08press |07any key|08 to continue");
  footer.status("", true);
}

function pause() {
  for (;;) {
    var k = v3.console.getkey(FRAME_MS);
    if (k !== "") return;
    drawShape((Date.now() - shapeStart) / 1000);
    animStar();
    panel.glint();
    footer.status("", false);
    v3.console.gotoxy(W + 1, boxBottom);
  }
}
