// submenu.js — a compact section menu in the PMS style.
//
// A menu script (SCRIPT in a .MNU, or menuScript in theme.json): the BBS
// hands it the menu in v3.menu (name, title, items, shortcuts) and it answers
// with v3.prompt.answer(key). It draws the menu's title as figlet lettering
// upper-left, a small spinning wireframe in the open space, the options
// spread across the width, and the board name dim in the lower-right.
//
// Wire it in the .MNU as a theme script:
//   "SCRIPT": "submenu.js sphere"
// The word after the script name is the shape: cube, pyramid, sphere or morph.
// Title lettering comes from ART below, by menu name; a menu without an
// entry shows its TITLE as plain text. Give each command to show a short
// LABEL in the .CFG.
//
// It also draws two built-in screens when their .MNU names it: User Konfig
// (KONFIG.MNU), two columns of settings with their values from
// v3.menu.items[].value, and the Twit List (TWITLIST.MNU), whose entries come
// in v3.menu.body. Both show v3.menu.notice after an action.

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

var ART = {
  "ADMIN": [
    "   _____  ________      _____  .___ _______",
    "  /  _  \\ \\______ \\    /     \\ |   |\\      \\",
    " /  /_\\  \\ |    |  \\  /  \\ /  \\|   |/   |   \\",
    "/    |    \\|    `   \\/    Y    \\   /    |    \\",
    "\\____|__  /_______  /\\____|__  /___\\____|__  /",
    "        \\/        \\/         \\/            \\/"
  ],
  "BBSLISTM": [
    "____________________  ____________________ _________",
    "\\______   \\______   \\/   _____/\\_   _____//   _____/",
    " |    |  _/|    |  _/\\_____  \\  |    __)_ \\_____  \\",
    " |    |   \\|    |   \\/        \\ |        \\/        \\",
    " |______  /|______  /_______  //_______  /_______  /",
    "        \\/        \\/        \\/         \\/        \\/"
  ],
  "DOORSM": [
    "________   ________   ________ __________  _________",
    "\\______ \\  \\_____  \\  \\_____  \\\\______   \\/   _____/",
    " |    |  \\  /   |   \\  /   |   \\|       _/\\_____  \\",
    " |    `   \\/    |    \\/    |    \\    |   \\/        \\",
    "/_______  /\\_______  /\\_______  /____|_  /_______  /",
    "        \\/         \\/         \\/       \\/        \\/"
  ],
  "EMAILM": [
    "___________   _____      _____  .___.____",
    "\\_   _____/  /     \\    /  _  \\ |   |    |",
    " |    __)_  /  \\ /  \\  /  /_\\  \\|   |    |",
    " |        \\/    Y    \\/    |    \\   |    |___",
    "/_______  /\\____|__  /\\____|__  /___|_______ \\",
    "        \\/         \\/         \\/            \\/"
  ],
  "FASTLOGN": [
    ".____     ________     ________ ________    _______",
    "|    |    \\_____  \\   /  _____/ \\_____  \\   \\      \\",
    "|    |     /   |   \\ /   \\  ___  /   |   \\  /   |   \\",
    "|    |___ /    |    \\\\    \\_\\  \\/    |    \\/    |    \\",
    "|_______ \\\\_______  / \\______  /\\_______  /\\____|__  /",
    "        \\/        \\/         \\/         \\/         \\/"
  ],
  "FILEM": [
    "___________.___.____     ___________ _________",
    "\\_   _____/|   |    |    \\_   _____//   _____/",
    " |    __)  |   |    |     |    __)_ \\_____  \\",
    " |     \\   |   |    |___  |        \\/        \\",
    " \\___  /   |___|_______ \\/_______  /_______  /",
    "     \\/                \\/        \\/        \\/"
  ],
  "INFORMM": [
    ".___ _______  ___________________",
    "|   |\\      \\ \\_   _____/\\_____  \\",
    "|   |/   |   \\ |    __)   /   |   \\",
    "|   /    |    \\|     \\   /    |    \\",
    "|___\\____|__  /\\___  /   \\_______  /",
    "            \\/     \\/            \\/"
  ],
  "KONFIG": [
    "____  __. ________    _______   ___________.___   ________",
    "|    |/ _|\\_____  \\   \\      \\  \\_   _____/|   | /  _____/",
    "|      <   /   |   \\  /   |   \\  |    __)  |   |/   \\  ___",
    "|    |  \\ /    |    \\/    |    \\ |     \\   |   |\\    \\_\\  \\",
    "|____|__ \\\\_______  /\\____|__  / \\___  /   |___| \\______  /",
    "        \\/        \\/         \\/      \\/                 \\/"
  ],
  "MSGMENU": [
    "   _____    _________ ________  _________",
    "  /     \\  /   _____//  _____/ /   _____/",
    " /  \\ /  \\ \\_____  \\/   \\  ___ \\_____  \\",
    "/    Y    \\/        \\    \\_\\  \\/        \\",
    "\\____|__  /_______  /\\______  /_______  /",
    "        \\/        \\/        \\/        \\/"
  ],
  "QWKM": [
    "________  __      __ ____  __.",
    "\\_____  \\/  \\    /  \\    |/ _|",
    " /  / \\  \\   \\/\\/   /      <",
    "/   \\_/.  \\        /|    |  \\",
    "\\_____\\ \\_/\\__/\\  / |____|__ \\",
    "       \\__>     \\/          \\/"
  ],
  "RUMORM": [
    "__________ ____ ___  _____   ________ __________  _________",
    "\\______   \\    |   \\/     \\  \\_____  \\\\______   \\/   _____/",
    " |       _/    |   /  \\ /  \\  /   |   \\|       _/\\_____  \\",
    " |    |   \\    |  /    Y    \\/    |    \\    |   \\/        \\",
    " |____|_  /______/\\____|__  /\\_______  /____|_  /_______  /",
    "        \\/                \\/         \\/       \\/        \\/"
  ],
  "SPONSORM": [
    "   _____ _____________________   _____",
    "  /  _  \\\\______   \\_   _____/  /  _  \\",
    " /  /_\\  \\|       _/|    __)_  /  /_\\  \\",
    "/    |    \\    |   \\|        \\/    |    \\",
    "\\____|__  /____|_  /_______  /\\____|__  /",
    "        \\/       \\/        \\/         \\/"
  ],
  "TWITLIST": [
    "___________ __      __ .___ ___________",
    "\\__    ___//  \\    /  \\|   |\\__    ___/",
    "  |    |   \\   \\/\\/   /|   |  |    |",
    "  |    |    \\        / |   |  |    |",
    "  |____|     \\__/\\  /  |___|  |____|",
    "                  \\/"
  ],
  "V3NETM": [
    "____   ____________   _______  ______________________",
    "\\   \\ /   /\\_____  \\  \\      \\ \\_   _____/\\__    ___/",
    " \\   Y   /   _(__  <  /   |   \\ |    __)_   |    |",
    "  \\     /   /       \\/    |    \\|        \\  |    |",
    "   \\___/   /______  /\\____|__  /_______  /  |____|",
    "                  \\/         \\/        \\/"
  ]
};

var ITEMS = v3.menu.items || [];
var SHORTCUTS = v3.menu.shortcuts || [];
var TITLE = ART[v3.menu.name] || (v3.menu.title ? [v3.menu.title] : []);
var SHAPE = (v3.args[0] || "morph").toLowerCase();
var SETTINGS = v3.menu.name === "KONFIG";
var MANAGED_LIST = v3.menu.name === "TWITLIST";
var NOTICE = v3.menu.notice || "";
// The Twit List's entries run down three columns.
var BODY = (function () {
  var rows = v3.menu.body || [];
  if (!MANAGED_LIST || rows.length < 2) return rows;
  var n = Math.ceil(rows.length / 3), out = [];
  for (var r = 0; r < n; r++) {
    var line = "";
    for (var c = 0; c < 3; c++) {
      var cell = rows[c * n + r];
      if (cell === undefined) break;
      cell = cell.substring(0, 24);
      while (cell.length < 25) cell += " ";
      line += cell;
    }
    out.push(line);
  }
  return out;
})();

var W = PMS.W;
var H = v3.console.height;
var FRAME_MS = 66;

// Blue -> teal -> cyan depth ramp (truecolor): deep navy up to icy cyan.
var SHADE = [
  "\x1b[38;2;12;34;72m", "\x1b[38;2;20;78;128m", "\x1b[38;2;26;140;160m",
  "\x1b[38;2;40;196;206m", "\x1b[38;2;120;236;236m", "\x1b[38;2;208;255;255m"];

// --- geometry ------------------------------------------------------------
var titleLeft = 3;
var itemLeft = 4;
// A setting's value starts VALUE_AT columns into its cell and is VALUE_W
// wide: " K  ", the label in 17 and a space come first.
var VALUE_AT = 22, VALUE_W = 14;
var titleW = PMS.titleWidth(TITLE);
var usable = W - itemLeft - 2;
var ncols = Math.min(ITEMS.length, Math.max(2, Math.floor(usable / 17)));
if (SETTINGS) ncols = 2;
var rowsPer = Math.ceil(ITEMS.length / Math.max(1, ncols));
var colW = Math.floor(usable / Math.max(1, ncols));
// The board name sits in the lower-right corner on the last item row. When every
// column is full that corner holds an item, so give the name a row of its own.
var brandRow = ITEMS.length >= ncols * rowsPer ? 1 : 0;
// Centre the whole composition (panel + two footer lines) in the terminal.
var bodyH = BODY.length ? BODY.length + 1 : 0;
var totalH = TITLE.length + bodyH + rowsPer + brandRow + 6;
var boxTop = Math.max(1, (((H - totalH) / 2) | 0) + 1);
var titleTop = boxTop + 1;
var bodyTop = titleTop + TITLE.length + 1;
var itemTop = bodyTop + bodyH;
var boxBottom = itemTop + rowsPer + brandRow;
var panel = PMS.frame(boxTop, boxBottom);
var footer = PMS.footer(boxBottom);

// The shape box: the open space right of the title.
var SLEFT = titleLeft + titleW + 2;
var SW = Math.max(10, W - 2 - SLEFT);
var STOP = titleTop;
var SH = Math.min(TITLE.length + 1, boxBottom - 1 - titleTop);

var EDGES = [[0, 1], [2, 3], [4, 5], [6, 7], [0, 2], [1, 3], [4, 6], [5, 7],
  [0, 4], [1, 5], [2, 6], [3, 7]];

function makeSphere(lats, lons) {
  var verts = [{ x: 0, y: -1, z: 0 }, { x: 0, y: 1, z: 0 }];
  var grid = [], i, j;
  for (i = 0; i < lats; i++) {
    var phi = (Math.PI * (i + 1)) / (lats + 1), row = [];
    for (j = 0; j < lons; j++) {
      var th = (2 * Math.PI * j) / lons;
      row.push(verts.length);
      verts.push({ x: Math.sin(phi) * Math.cos(th), y: -Math.cos(phi), z: Math.sin(phi) * Math.sin(th) });
    }
    grid.push(row);
  }
  var edges = [];
  for (i = 0; i < lats; i++) for (j = 0; j < lons; j++) edges.push([grid[i][j], grid[i][(j + 1) % lons]]);
  for (i = 0; i < lats - 1; i++) for (j = 0; j < lons; j++) edges.push([grid[i][j], grid[i + 1][j]]);
  for (j = 0; j < lons; j++) { edges.push([0, grid[0][j]]); edges.push([1, grid[lats - 1][j]]); }
  return { verts: verts, edges: edges };
}

var SHAPES = {
  cube: {
    verts: (function () { var v = []; for (var i = 0; i < 8; i++) v.push({ x: (i & 1) * 2 - 1, y: ((i >> 1) & 1) * 2 - 1, z: ((i >> 2) & 1) * 2 - 1 }); return v; })(),
    edges: EDGES,
  },
  pyramid: {
    verts: [{ x: -1, y: 1, z: -1 }, { x: 1, y: 1, z: -1 }, { x: 1, y: 1, z: 1 }, { x: -1, y: 1, z: 1 }, { x: 0, y: -1, z: 0 }],
    edges: [[0, 1], [1, 2], [2, 3], [3, 0], [0, 4], [1, 4], [2, 4], [3, 4]],
  },
  sphere: makeSphere(3, 6),
};
var POLY = SHAPES[SHAPE] || SHAPES.cube;
var CUBE_V = SHAPES.cube.verts;

var pos = 0, frame = 0, prevShape = {}, shapeStart = Date.now();
if (SETTINGS) {
  for (var selected = 0; selected < ITEMS.length; selected++) {
    if (ITEMS[selected].key === v3.menu.selected) pos = selected;
  }
}

v3.console.print("\x1b[?25l"); // hide the cursor for a steady redraw
if (SETTINGS) markFields();
draw();
lightbar();
v3.console.print("\x1b[?25h");

// morphAmount oscillates 0 (cube) .. 1 (pyramid) and dwells at each end.
function morphAmount(t) {
  return (1 - Math.cos(t * 0.5)) / 2;
}
// morphVerts is the cube with its top four corners drawn toward the apex by
// m, so the shape slides between a cube (m=0) and a pyramid (m=1).
function morphVerts(m) {
  var v = [];
  for (var i = 0; i < 8; i++) {
    var p = CUBE_V[i], x = p.x, z = p.z;
    if (p.y < 0) { x = x * (1 - m); z = z * (1 - m); }
    v.push({ x: x, y: p.y, z: z });
  }
  return v;
}

// --- shape renderer ------------------------------------------------------

function clamp(v, a, b) { return v < a ? a : v > b ? b : v; }

function shade(x, y, z) {
  var len = Math.sqrt(x * x + y * y + z * z);
  if (len === 0) return 0;
  var nx = x / len, ny = y / len, nz = z / len;
  var near = clamp((1.75 - z) / 3.5, 0, 1);
  var diffuse = Math.max(0, -0.35 * nx - 0.5 * ny - 0.792 * nz);
  var spec = Math.pow(Math.max(0, -0.185 * nx - 0.264 * ny - 0.947 * nz), 18);
  var b = 0.16 + 0.48 * near + 0.2 * diffuse + 0.3 * spec;
  return clamp(Math.floor(b * SHADE.length), 0, SHADE.length - 1);
}

function shapeCells(t) {
  var sx = Math.sin(t * 0.43 + 0.3), cx = Math.cos(t * 0.43 + 0.3);
  var sy = Math.sin(t * 0.61 + 0.5), cy = Math.cos(t * 0.61 + 0.5);
  var sz = Math.sin(t * 0.19 - 0.2), cz = Math.cos(t * 0.19 - 0.2);
  var scale = Math.min(SW / 2, SH) * 0.27;
  var V = SHAPE === "morph" ? morphVerts(morphAmount(t)) : POLY.verts;
  var E = SHAPE === "morph" ? EDGES : POLY.edges;
  var verts = [], world = [];
  for (var i = 0; i < V.length; i++) {
    var x = V[i].x, y = V[i].y, z = V[i].z;
    var ny = y * cx - z * sx, nz = y * sx + z * cx; y = ny; z = nz;
    var nx = x * cy + z * sy; nz = -x * sy + z * cy; x = nx; z = nz;
    nx = x * cz - y * sz; ny = x * sz + y * cz; x = nx; y = ny;
    world[i] = { x: x, y: y, z: z };
    var d = 4 / (4.5 + z);
    verts[i] = { x: SLEFT + (SW - 1) / 2 + x * d * scale * 2, y: STOP - 1 + (SH - 1) / 2 + y * d * scale, z: z };
  }
  var sub = SW * SH * 2, zbuf = [], cbuf = [];
  for (var k = 0; k < sub; k++) { zbuf[k] = Infinity; cbuf[k] = 0; }
  function plot(px, py, z, col) {
    var lx = px - SLEFT, ly = py - (STOP - 1) * 2;
    if (lx < 0 || lx >= SW || ly < 0 || ly >= SH * 2) return;
    var at = (((ly >> 1) * SW) + lx) * 2 + (ly & 1);
    if (z <= zbuf[at]) { zbuf[at] = z; cbuf[at] = col; }
  }
  for (var e = 0; e < E.length; e++) {
    var a = verts[E[e][0]], b = verts[E[e][1]];
    var ax = Math.round(a.x), ay = Math.round(a.y * 2), bx = Math.round(b.x), by = Math.round(b.y * 2);
    var steps = Math.max(1, Math.max(Math.abs(bx - ax), Math.abs(by - ay)));
    var wa = world[E[e][0]], wb = world[E[e][1]];
    for (var s = 0; s <= steps; s++) {
      var u = s / steps;
      var denom = (1 - u) / (4.5 + a.z) + u / (4.5 + b.z);
      var zz = 1 / denom - 4.5, vv = (u / (4.5 + b.z)) / denom;
      plot(ax + Math.round(u * (bx - ax)), ay + Math.round(u * (by - ay)), zz,
        shade(wa.x + (wb.x - wa.x) * vv, wa.y + (wb.y - wa.y) * vv, zz));
    }
  }
  var cells = {};
  for (var row = 0; row < SH; row++) for (var col = 0; col < SW; col++) {
    var base = ((row * SW) + col) * 2, up = zbuf[base], lo = zbuf[base + 1];
    if (up === Infinity && lo === Infinity) continue;
    var ch, ci;
    if (up === Infinity) { ch = "."; ci = cbuf[base + 1]; }
    else if (lo === Infinity) { ch = "'"; ci = cbuf[base]; }
    else { ch = ":"; ci = lo < up ? cbuf[base + 1] : cbuf[base]; }
    cells[(STOP + row) * 1000 + (SLEFT + col + 1)] = SHADE[ci] + ch;
  }
  return cells;
}

function drawShape(t) {
  var cells = shapeCells(t), k;
  for (k in cells) {
    if (cells[k] !== prevShape[k]) { v3.console.gotoxy(k % 1000, (k / 1000) | 0); v3.console.print(cells[k]); }
  }
  for (k in prevShape) {
    if (cells[k] === undefined) { v3.console.gotoxy(k % 1000, (k / 1000) | 0); v3.console.print(" "); }
  }
  prevShape = cells;
  v3.console.print("|07");
}

// --- frame / helpers -----------------------------------------------------

function stripP(s) { return v3.util.stripPipe(String(s == null ? "" : s)); }

// fit cuts or pads s to n terminal columns, measured as the caller's
// terminal draws them and as the Konfig editor measures its fields.
function fit(s, n) {
  return v3.util.fitColumns(String(s == null ? "" : s), n);
}
function itemCell(i) { var c = (i / rowsPer) | 0, r = i % rowsPer; return { col: itemLeft + c * colW, row: itemTop + r }; }
// markFields tells the screen that each setting's editor goes where
// drawItem shows the setting's value, and its prompts on the hint row.
function markFields() {
  v3.menu.promptRow(boxBottom + 1);
  for (var i = 0; i < ITEMS.length; i++) {
    var cell = itemCell(i);
    v3.menu.field(ITEMS[i].key, cell.col + VALUE_AT, cell.row, VALUE_W);
  }
}
function drawItem(i, sel) {
  var it = ITEMS[i], cell = itemCell(i), lab = stripP(it.label);
  v3.console.gotoxy(cell.col, cell.row);
  if (SETTINGS) {
    // Write caller values literally: a location containing |13 is text.
    var name = fit(it.label, 17), value = fit(it.value, VALUE_W);
    if (sel) {
      v3.console.print(PMS.BAR);
      v3.console.write(" " + it.key + "  " + name + " " + value);
    } else {
      v3.console.print(" |13" + it.key + "|07  ");
      v3.console.write(name + " ");
      v3.console.print("|05");
      v3.console.write(value);
    }
    v3.console.print("|B0|07");
    return;
  }
  if (sel) {
    var text = " " + it.key + "  " + lab;
    if (text.length > colW - 1) text = text.substring(0, colW - 1);
    while (text.length < colW - 1) text += " ";
    v3.console.print(PMS.BAR + text + PMS.BAR_END);
  } else {
    var pad = "", used = 3 + String(it.key).length + lab.length;
    for (var p = 0; p < colW - 1 - used; p++) pad += " ";
    v3.console.print(" |13" + it.key + "|07  " + lab + pad);
  }
}

function drawFooter() {
  if (SETTINGS) {
    footer.hint("|07arrows|08 move   |07enter|08 change   |07Q/ESC|08 done");
    drawHint();
    return;
  }
  if (MANAGED_LIST) {
    footer.hint("|07arrows|08 move   |07enter|08 select   |07Q/ESC|08 back   |07[/]|08 page");
    drawHint();
    return;
  }
  footer.hint(PMS.MENU_KEYS);
}

function drawHint() {
  if (!SETTINGS && !MANAGED_LIST) return;
  var text = stripP(NOTICE || (MANAGED_LIST ? v3.menu.hint : ITEMS[pos].activity) || "").replace(/[\x00-\x1f\x7f]/g, " ");
  text = text.substring(0, W - 4);
  while (text.length < W - 4) text += " ";
  v3.console.gotoxy(3, boxBottom + 1);
  v3.console.print(NOTICE ? "|13" : "|08");
  v3.console.write(text);
  v3.console.print("|07");
}
// On the message and file menus the status line names the caller's
// conference and area in place of the date. The menu is drawn from code
// above this point, so the text is worked out on first use.
var whereText;
function where() {
  if (whereText === undefined) {
    var codes = { MSGMENU: ["|CC", "|CAN"], FILEM: ["|FC", "|CFAN"] }[v3.menu.name];
    whereText = "";
    if (codes && v3.menu.expand) {
      var conf = v3.menu.expand(codes[0]).substring(0, 12);
      var area = v3.menu.expand(codes[1]).substring(0, 36 - conf.length - 3);
      whereText = "|13" + conf + " |05/ |13" + area;
    }
  }
  return whereText;
}
function drawStatus(force) {
  footer.status(where(), force);
}

function draw() {
  v3.console.clear();
  panel.draw();
  PMS.title(TITLE, titleLeft, titleTop);
  for (var b = 0; b < BODY.length; b++) {
    v3.console.gotoxy(3, bodyTop + b);
    v3.console.print("|07");
    v3.console.write(BODY[b]);
  }
  for (var i = 0; i < ITEMS.length; i++) drawItem(i, false);
  PMS.brand(boxBottom - 1);
  drawShape((Date.now() - shapeStart) / 1000);
  drawFooter();
  drawStatus(true);
}

// --- input ---------------------------------------------------------------

function chooseKey(key) { v3.console.print("|B0|07"); v3.console.gotoxy(1, H); v3.prompt.answer(key); }
function choose(i) { chooseKey(ITEMS[i].key); }

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
  if (ITEMS.length === 0) return;
  drawItem(pos, true);
  for (;;) {
    var key = v3.console.readkey(FRAME_MS);
    if (key === "") {
      drawShape((Date.now() - shapeStart) / 1000);
      panel.glint();
      drawStatus(false);
      v3.console.gotoxy(W + 1, footer.statusRow);
      continue;
    }
    var next = pos;
    if ((SETTINGS || MANAGED_LIST) && (key === "ESC" || key.toUpperCase() === "Q")) {
      chooseKey("Q"); return;
    }
    var command = commandKey(key === "ESC" ? "Q" : key);
    if (command) { chooseKey(command); return; }
    if (key === "UP" || key === "8") next = pos % rowsPer === 0 ? pos : pos - 1;
    else if (key === "DOWN" || key === "2") next = (pos % rowsPer === rowsPer - 1 || pos + 1 >= ITEMS.length) ? pos : pos + 1;
    else if (key === "LEFT" || key === "4") next = pos - rowsPer >= 0 ? pos - rowsPer : pos;
    else if (key === "RIGHT" || key === "6") next = pos + rowsPer < ITEMS.length ? pos + rowsPer : pos;
    else if (key === "\r" || key === " ") { choose(pos); return; }
    else {
      for (var i = 0; i < ITEMS.length; i++) {
        if (String(ITEMS[i].key).toUpperCase() === key.toUpperCase()) { choose(i); return; }
      }
      continue;
    }
    if (next === pos) continue;
    drawItem(pos, false); pos = next; drawItem(pos, true);
    if (SETTINGS) { NOTICE = ""; drawHint(); }
    v3.console.gotoxy(W + 1, footer.statusRow);
  }
}
