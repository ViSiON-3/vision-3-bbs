// matrix.js — an animated "login matrix" entrance: a wireframe cube that
// zooms in from nothing and keeps rotating on the left, with the matrix
// options as colourful animated text on the right. A colour wave flows
// through the option text. Any listed key, or Enter, ends it.
//
// A VPL script runs after login, so this is the on-login entrance, not
// the pre-auth authenticator (that stays the Go matrix). Put it first in
// the logon sequence, or run it as a door:
//   { "code": "MATRIX", "name": "Matrix", "type": "v3_script",
//     "script": "matrix.js", "working_directory": "menus/pms/scripts" }

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

var W = v3.console.width;
var H = v3.console.height;
var FRAME_MS = 80;

// The cube's box, on the left.
var CW = Math.min(23, (W / 2) | 0);
var CH = Math.min(14, H - 2);
var CLEFT = 3;
var CTOP = (((H - CH) / 2) | 0) + 1;

var EDGES = [[0, 1], [2, 3], [4, 5], [6, 7], [0, 2], [1, 3], [4, 6], [5, 7],
    [0, 4], [1, 5], [2, 6], [3, 7]];
var SHADE = ["|08", "|01", "|03", "|09", "|11", "|15"]; // chrome, dark to bright
// A flowing colour wave for the option text.
var WAVE = ["|12", "|13", "|11", "|15", "|11", "|13", "|12", "|04"];

// The matrix options: hotkey, label. Shown as flavour here; the real
// account selection is the Go matrix for unauthenticated callers.
var OPTIONS = [
    "J . Journey onward",
    "C . Create an account",
    "A . Check your access",
    "D . Disconnect"
];
var OPT_KEYS = "JCADjcad\r";

function clamp(v, a, b) {
    return v < a ? a : (v > b ? b : v);
}

function smooth(p) {
    p = clamp(p, 0, 1);
    return p * p * (3 - 2 * p);
}

function shade(x, y, z, grow) {
    var len = Math.sqrt(x * x + y * y + z * z);
    if (len === 0) {
        return 0;
    }
    var nx = x / len, ny = y / len, nz = z / len;
    var near = clamp((1.75 - z) / 3.5, 0, 1);
    var diffuse = Math.max(0, -0.35 * nx - 0.5 * ny - 0.792 * nz);
    var spec = Math.pow(Math.max(0, -0.185 * nx - 0.264 * ny - 0.947 * nz), 18);
    var b = (0.16 + 0.48 * near + 0.2 * diffuse + 0.3 * spec) * (0.3 + 0.7 * grow);
    return clamp(Math.floor(b * SHADE.length), 0, SHADE.length - 1);
}

function cubeCells(t, grow) {
    var sx = Math.sin(t * 0.43 + 0.3), cx = Math.cos(t * 0.43 + 0.3);
    var sy = Math.sin(t * 0.61 + 0.5), cy = Math.cos(t * 0.61 + 0.5);
    var sz = Math.sin(t * 0.19 - 0.2), cz = Math.cos(t * 0.19 - 0.2);
    var scale = Math.min(CW / 2, CH) * 0.27 * grow;
    var verts = [], world = [];
    for (var i = 0; i < 8; i++) {
        var x = (i & 1) * 2 - 1, y = ((i >> 1) & 1) * 2 - 1, z = ((i >> 2) & 1) * 2 - 1;
        var ny = y * cx - z * sx, nz = y * sx + z * cx;
        y = ny; z = nz;
        var nx = x * cy + z * sy;
        nz = -x * sy + z * cy;
        x = nx; z = nz;
        nx = x * cz - y * sz; ny = x * sz + y * cz;
        x = nx; y = ny;
        world[i] = { x: x, y: y, z: z };
        var d = 4 / (4.5 + z);
        verts[i] = {
            x: CLEFT + (CW - 1) / 2 + x * d * scale * 2,
            y: CTOP - 1 + (CH - 1) / 2 + y * d * scale,
            z: z
        };
    }
    var sub = CW * CH * 2;
    var zbuf = [], cbuf = [];
    for (var k = 0; k < sub; k++) {
        zbuf[k] = Infinity;
        cbuf[k] = 0;
    }
    function put(px, py, z, col) {
        var lx = px - CLEFT, ly = py - (CTOP - 1) * 2;
        if (lx < 0 || lx >= CW || ly < 0 || ly >= CH * 2) {
            return;
        }
        var at = (((ly >> 1) * CW) + lx) * 2 + (ly & 1);
        if (z <= zbuf[at]) {
            zbuf[at] = z;
            cbuf[at] = col;
        }
    }
    for (var e = 0; e < EDGES.length; e++) {
        var a = verts[EDGES[e][0]], b = verts[EDGES[e][1]];
        var ax = Math.round(a.x), ay = Math.round(a.y * 2);
        var bx = Math.round(b.x), by = Math.round(b.y * 2);
        var steps = Math.max(1, Math.max(Math.abs(bx - ax), Math.abs(by - ay)));
        var wa = world[EDGES[e][0]], wb = world[EDGES[e][1]];
        for (var s = 0; s <= steps; s++) {
            var u = s / steps;
            var denom = (1 - u) / (4.5 + a.z) + u / (4.5 + b.z);
            var zz = 1 / denom - 4.5;
            var vv = (u / (4.5 + b.z)) / denom;
            put(ax + Math.round(u * (bx - ax)), ay + Math.round(u * (by - ay)), zz,
                shade(wa.x + (wb.x - wa.x) * vv, wa.y + (wb.y - wa.y) * vv, zz, grow));
        }
    }
    var cells = {};
    for (var row = 0; row < CH; row++) {
        for (var col = 0; col < CW; col++) {
            var base = ((row * CW) + col) * 2;
            var up = zbuf[base], lo = zbuf[base + 1];
            if (up === Infinity && lo === Infinity) {
                continue;
            }
            var ch, ci;
            if (up === Infinity) {
                ch = "."; ci = cbuf[base + 1];
            } else if (lo === Infinity) {
                ch = "'"; ci = cbuf[base];
            } else {
                ch = ":"; ci = lo < up ? cbuf[base + 1] : cbuf[base];
            }
            cells[(CTOP + row) * 1000 + (CLEFT + col + 1)] = SHADE[ci] + ch;
        }
    }
    return cells;
}

// optionCells lays the option labels on the right with a colour wave
// running through the letters.
function optionCells(t) {
    var cells = {};
    var left = CLEFT + CW + 6;
    var top = (((H - OPTIONS.length * 2) / 2) | 0) + 1;
    var wavepos = t * 11;
    for (var o = 0; o < OPTIONS.length; o++) {
        var text = OPTIONS[o];
        var row = top + o * 2;
        for (var i = 0; i < text.length && left + i <= W; i++) {
            var ch = text.charAt(i);
            if (ch === " ") {
                continue;
            }
            var col = WAVE[(Math.round(wavepos) + i + o) % WAVE.length];
            cells[row * 1000 + (left + i)] = col + ch;
        }
    }
    return cells;
}

function merge() {
    var out = {};
    for (var a = 0; a < arguments.length; a++) {
        var m = arguments[a];
        for (var k in m) {
            out[k] = m[k];
        }
    }
    return out;
}

function center(text, row, colour) {
    v3.console.gotoxy(((W - text.length) / 2 | 0) + 1, row);
    v3.console.print(colour + text);
}

function run() {
    v3.console.print("\x1b[?25l");
    v3.console.clear();
    center(v3.session.bbs.name, 1, "|11");
    center("[ press a key to continue ]", H, "|08");
    var start = Date.now();
    var prev = {};
    for (;;) {
        var t = (Date.now() - start) / 1000;
        var grow = t < 2 ? smooth(t / 2) : 1; // zoom in from nothing
        var scene = merge(cubeCells(t, grow), optionCells(t));
        var key;
        for (key in scene) {
            if (scene[key] !== prev[key]) {
                v3.console.gotoxy(key % 1000, (key / 1000) | 0);
                v3.console.print(scene[key]);
            }
        }
        for (key in prev) {
            if (scene[key] === undefined) {
                v3.console.gotoxy(key % 1000, (key / 1000) | 0);
                v3.console.print(" ");
            }
        }
        prev = scene;
        v3.console.gotoxy(W, H);
        var k = v3.console.getkey(FRAME_MS);
        if (k !== "" && OPT_KEYS.indexOf(k) >= 0) {
            break;
        }
    }
    v3.console.print("|07\x1b[?25h");
    v3.console.clear();
}

if (W < 50 || H < 16) {
    v3.console.println("|07Your terminal is too small for the matrix.");
    v3.console.pause();
} else {
    run();
}
