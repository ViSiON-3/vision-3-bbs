// PMS crew directory. A borderless ledger, deliberately separate from the
// animated menu renderer. Data comes from the public LISTUSERS projection.
// USERLIST.MNU names userlist.js, so the BBS runs this script in place
// of the built-in user list. The script pages and searches the list itself
// and answers "Q" (v3.prompt.answer) when the caller leaves.
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

(function () {
    var c = v3.console;
    var title = clean(v3.menu && v3.menu.title || "Crew Directory").toLowerCase();
    var width = Math.max(80, c.width || 80);
    var height = Math.max(24, c.height || 24);
    var right = Math.min(width - 3, 76);
    var rows = Math.max(1, height - 14);
    var users = v3.users.publicList();
    var matches = users;
    var page = 0;
    var query = "";
    var pending = users.filter(function (u) { return !u.validated && u.accessLevel > 0; }).length;
    var frame = 0;
    var starX = 26;
    var star = [
        "       /\\",
        "\\\\ ___/oO\\___",
        " \\\\ \\   ^   /",
        " // /___ ___\\",
        "        \\/"
    ];

    // Caller text stays literal, including strings resembling pipe codes.
    function clean(s) {
        return v3.util.stripAnsi(String(s || "")).replace(/[\x00-\x1f\x7f-\x9f]/g, " ").trim();
    }
    function fit(s, n) {
        var chars = Array.from(clean(s));
        if (chars.length > n) chars = chars.slice(0, n - 1).concat("~");
        return chars.join("") + new Array(Math.max(0, n - chars.length) + 1).join(" ");
    }
    function at(x, y, color, text) {
        c.gotoxy(x, y);
        c.print(color);
        c.write(text);
    }
    function centered(y, color, text) {
        at(Math.floor((width - Array.from(text).length) / 2) + 1, y, color, text);
    }
    function drawStar() {
        // The familiar PMS star floats between the masthead and board name.
        // Redraw only his little patch while idle, leaving the ledger still.
        for (var s = 0; s < star.length; s++) {
            var line = star[s];
            if (s === 1) line = line.replace("oO", frame % 24 === 12 || frame % 24 === 13 ? "-O" : "oO");
            if (s === 2) line = line.replace("^", frame % 32 >= 24 ? "o" : "^");
            c.gotoxy(starX, s + 1);
            var out = "";
            for (var j = 0; j < line.length; j++) {
                out += (j < 3 ? "|08" : "|15") + line[j];
            }
            c.print(out + "|07");
        }
        var sparkles = ["*", "+", ".", " ", ".", "+"];
        at(24, 2, "|05", sparkles[Math.floor(frame / 2) % sparkles.length]);
        at(41, 4, "|03", sparkles[(Math.floor(frame / 2) + 3) % sparkles.length]);
    }
    function draw() {
        c.print("|00|B0");
        c.clear();
        at(4, 2, "|05", "▄▀▀ █▀▄ █▀▀ █   █");
        at(4, 3, "|13", "█   █▀▄ █▀  █▄█▄█");
        at(4, 4, "|08", "▀▀▀ ▀ ▀ ▀▀▀  ▀ ▀");
        if (width >= 76) {
            at(46, 2, "|05", "P M S  /  P E O P L E");
            at(46, 3, "|07", "pirate mind station");
            at(46, 4, "|08", fit(title + "  /  a-z", right - 45));
        }
        drawStar();
        at(4, 6, "|07", users.length + " on file");
        at(23, 6, "|08", fit(pending + " awaiting validation", right - 22));
        var handleWidth = 30;
        var locationX = 8 + handleWidth + 3;
        var locationWidth = right - locationX - 7;
        at(4, 8, "|08", "NO.");
        at(8, 8, "|03", "HANDLE");
        at(locationX, 8, "|03", "LOCATION / GROUP");
        at(right - 2, 8, "|03", "LVL");
        at(4, 9, "|08", new Array(right - 2).join("·"));
        var start = page * rows;
        var end = Math.min(start + rows, matches.length);
        for (var i = start; i < end; i++) {
            var u = matches[i];
            var y = 10 + i - start;
            at(4, y, "|05", fit(String(i + 1), 3));
            at(8, y, "|07", fit(clean(u.handle) + (u.validated ? "" : " [NV]"), handleWidth));
            at(locationX, y, "|08", fit(clean(u.location) || "-", locationWidth));
            var level = String(u.accessLevel);
            at(right - 2, y, "|03", ("   " + level).slice(-3));
        }
        if (!matches.length) at(8, 11, "|08", "No matching crew on file.");
        var totalPages = Math.max(1, Math.ceil(matches.length / rows));
        var status = matches.length ? (start + 1) + "-" + end + " / " + matches.length : "0 / 0";
        if (query) status += "   find: " + query;
        status += "   page " + (page + 1) + "/" + totalPages;
        centered(height - 3, "|07", status);
        var controls = "|13N|08/|13P |07page  |13/ |07find  |13C |07clear  |13ENTER|08/|13Q |07back";
        var controlsWidth = v3.util.stripPipe(controls).length;
        c.gotoxy(Math.floor((width - controlsWidth) / 2) + 1, height - 2);
        c.print(controls);
        centered(height - 1, "|08", "[NV] awaiting validation");
    }

    c.write("\x1b[?25l");
    try {
        draw();
        while (true) {
            var key = c.readkey(140);
            if (!key) {
                frame++;
                drawStar();
                continue;
            }
            var upper = key.toUpperCase();
            if (key === "\r" || key === "\n" || key === "ESC" || upper === "Q") break;
            var pages = Math.max(1, Math.ceil(matches.length / rows));
            if (upper === "N" || key === " " || key === "RIGHT" || key === "DOWN") {
                page = Math.min(pages - 1, page + 1);
            } else if (upper === "P" || key === "LEFT" || key === "UP") {
                page = Math.max(0, page - 1);
            } else if (key === "/") {
                c.gotoxy(Math.floor((width - 57) / 2) + 1, height - 2);
                c.write("\x1b[2K");
                c.print("|13find |08(handle/location): |07");
                c.write("\x1b[?25h");
                query = clean(c.getstr(Math.min(32, right - 29)));
                c.write("\x1b[?25l");
                var needle = query.toLowerCase();
                matches = users.filter(function (u) {
                    return clean(u.handle).toLowerCase().indexOf(needle) >= 0 || clean(u.location).toLowerCase().indexOf(needle) >= 0;
                });
                page = 0;
            } else if (upper === "C") {
                query = "";
                matches = users;
                page = 0;
            } else {
                continue;
            }
            draw();
        }
        v3.prompt.answer("Q");
    } finally {
        c.print("|07|B0");
        c.write("\x1b[?25h");
    }
})();
