// submenu.js: the ViSiON/3 theme's section menus.
//
// A theme script: each menu reachable from Main names it in its .MNU with
// "SCRIPT": "submenu.js". The BBS hands it the menu in v3.menu (name,
// title, the items the caller may use, every shortcut) and it answers with
// v3.prompt.answer(key). It draws the menu's lettering in the main menu's
// gold-to-steel colours, the items in a frame under it, and on the message
// and file menus the caller's conference and area. It is text on every
// terminal; it sends no sixel.
//
// Lettering comes from ART below, by menu name; a menu without an entry
// shows its TITLE. Only labelled commands are listed, so give each command
// to show a LABEL in the .CFG; every shortcut still works when typed. Add
// "classic" after the script name ("submenu.js classic") for 16 colours on
// terminals without 24-bit colour. On a screen smaller than 80x24, or a menu
// with more items than fit, the script answers nothing and the menu shows
// its own screen and prompt.

// FIGlet Graffiti, the lettering of the main menu's text logo.
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
  "V3NETM": [
    "____   ____________   _______  ______________________",
    "\\   \\ /   /\\_____  \\  \\      \\ \\_   _____/\\__    ___/",
    " \\   Y   /   _(__  <  /   |   \\ |    __)_   |    |",
    "  \\     /   /       \\/    |    \\|        \\  |    |",
    "   \\___/   /______  /\\____|__  /_______  /  |____|",
    "                  \\/         \\/        \\/"
  ]
};

var shortcuts = v3.menu.shortcuts || [];
// A key listed twice runs the first command with it the caller may use (the
// sysop's file review queue ahead of Edit Upload on Files), so only that one
// is shown.
var items = (function () {
  var seen = {}, out = [], all = v3.menu.items || [];
  for (var i = 0; i < all.length; i++) {
    var k = String(all[i].key).toUpperCase();
    if (seen[k]) continue;
    seen[k] = true;
    out.push(all[i]);
  }
  return out;
})();
var w = v3.console.width, h = v3.console.height;
var truecolor = v3.args.indexOf('classic') < 0;
var lettering = ART[v3.menu.name] || [];
var title = String(v3.menu.title || v3.menu.name || '');
var selected = 0, tick = 0, startedAt = Date.now(), lastTick = -1, lastFrame = -1;

// Layout: the lettering, the caller's conference and area on the message
// and file menus, then the framed items in three columns (four when three
// do not fit), and a line of help under the frame.
var letterTop = 2;
var whereRow = letterTop + Math.max(lettering.length, 1);
var top = whereRow + 1;
var columns = 3, rows = Math.ceil(items.length / columns);
if (top + rows + 3 > h) { columns = 4; rows = Math.ceil(items.length / columns); }
var cell = Math.floor(76 / columns);
var bottom = top + rows + 1;

function rgb(r, g, b) { return '\x1b[38;2;' + r + ';' + g + ';' + b + 'm'; }
function rawAt(x, y, text) { v3.console.gotoxy(x, y); v3.console.write(text); }
function at(x, y, text) { v3.console.gotoxy(x, y); v3.console.print(text); }
function center(y, text) {
  var width = v3.util.stripPipe(text).length;
  at(Math.max(1, Math.floor((80 - width) / 2) + 1), y, text);
}

// The lettering runs from hot gold on the left to steel on the right, as
// the main menu's text logo does.
function drawLettering() {
  if (!lettering.length) {
    center(letterTop, (truecolor ? '' : '|14') + title);
    return;
  }
  var width = 0;
  for (var r = 0; r < lettering.length; r++) width = Math.max(width, lettering[r].length);
  var left = Math.floor((80 - width) / 2) + 1;
  for (var y = 0; y < lettering.length; y++) {
    var line = lettering[y], out = '\x1b[0;40m';
    for (var x = 0; x < line.length; x++) {
      if (truecolor) {
        var steel = Math.min(1, Math.max(0, (x / width - 0.30) / 0.55));
        out += rgb(Math.round(255 - 75 * steel), Math.round(157 + 20 * steel), Math.round(33 + 150 * steel));
      } else {
        out += x < width / 2 ? '\x1b[1;33m' : '\x1b[0;37m';
      }
      out += line.charAt(x);
    }
    rawAt(left, letterTop + y, out + '\x1b[0m');
  }
}

// On the message and file menus, the caller's conference and area.
function drawWhere() {
  var codes = { MSGMENU: ['|CC', '|CAN'], FILEM: ['|FC', '|CFAN'] }[v3.menu.name];
  if (!codes || !v3.menu.expand) return;
  var text = v3.menu.expand(codes[0]) + ' / ' + v3.menu.expand(codes[1]);
  text = v3.util.fitColumns(text, 70).replace(/\s+$/, '');
  var left = Math.max(1, Math.floor((80 - text.length) / 2) + 1);
  v3.console.gotoxy(left, whereRow);
  v3.console.print(truecolor ? rgb(185, 175, 160) : '|07');
  v3.console.write(text);
  v3.console.print('|07');
}

// The frame, with the menu's title spaced out in its top edge. A glint runs
// once round it every half minute; between laps it is not redrawn.
function frame(step) {
  var left = 1, right = 79, width = right - left + 1, side = bottom - top - 1;
  var perimeter = 2 * width + 2 * side;
  var glint = step >= 0 && step * 2 < perimeter ? step * 2 : -1;
  var label = ' ' + title.toUpperCase().split('').join(' ') + ' ';
  if (label.length > width - 4) label = ' ' + title.toUpperCase() + ' ';
  var labelStart = Math.floor((width - label.length) / 2);
  function shade(index, x, letter) {
    if (!truecolor) return letter ? '\x1b[1;33m' : '\x1b[0;37m';
    var edge = Math.min(1, Math.max(0, (x - left) / (right - left)));
    var gap = (glint - index + perimeter) % perimeter;
    var shine = glint >= 0 && gap < 7 ? 1 - gap / 7 : 0;
    var hot = 1 - edge;
    var r = letter ? 255 : 95 + 105 * hot;
    var g = letter ? 190 : 95 + 35 * hot;
    var b = letter ? 90 : 110 - 55 * hot;
    return rgb(Math.round(r + (255 - r) * shine), Math.round(g + (245 - g) * shine), Math.round(b + (215 - b) * shine));
  }
  var out = '\x1b[0;40m';
  for (var i = 0; i < width; i++) {
    var isLabel = i >= labelStart && i < labelStart + label.length;
    var ch = isLabel ? label.charAt(i - labelStart) : i === 0 || i === width - 1 ? '.' : '-';
    out += shade(i, left + i, isLabel) + ch;
  }
  rawAt(left, top, out + '\x1b[0m');
  for (var y = 0; y < side; y++) {
    rawAt(right, top + 1 + y, shade(width + y, right, false) + '|\x1b[0m');
    rawAt(left, top + 1 + y, shade(2 * width + side + (side - 1 - y), left, false) + '|\x1b[0m');
  }
  out = '\x1b[0;40m';
  for (var j = 0; j < width; j++) {
    out += shade(width + side + (width - 1 - j), left + j, false) + (j === 0 ? '`' : j === width - 1 ? "'" : '-');
  }
  rawAt(left, bottom, out + '\x1b[0m');
}

// item draws command i: its key in gold, its label in warm grey, with a slow
// glow moving along the row; the selected one is bright.
function item(i, active) {
  if (i < 0 || i >= items.length) return;
  var col = Math.floor(i / rows), row = i % rows;
  var key = String(items[i].key), label = String(items[i].label || items[i].activity || key);
  var width = cell - 2, text = v3.util.fitColumns((active ? '>' : ' ') + key + ' ' + label, width);
  var out = '\x1b[0;40m';
  for (var j = 0; j < text.length; j++) {
    if (!truecolor) out += active ? '\x1b[1;37;43m' : j <= key.length ? '\x1b[1;33m' : '\x1b[0;37m';
    else if (active) out += j <= key.length ? rgb(255, 220, 155) : rgb(245, 240, 225);
    else {
      var wave = (Math.sin(j * 0.48 - tick * 0.14 + row * 0.65 + col) + 1) / 2;
      out += j <= key.length ? rgb(255, Math.round(115 + 115 * wave), Math.round(20 + 140 * wave)) :
        rgb(Math.round(175 + 65 * wave), Math.round(160 + 40 * wave), Math.round(145 - 55 * wave));
    }
    out += text.charAt(j);
  }
  rawAt(3 + col * cell, top + 1 + row, out + '\x1b[0m');
}

// across returns the item beside item i in the next column (dir 1) or the
// one before (dir -1) on the same row, wrapping round the columns and
// skipping a column with no item on that row.
function across(i, dir) {
  var cols = Math.ceil(items.length / rows), col = Math.floor(i / rows), row = i % rows;
  for (var step = 1; step < cols; step++) {
    var j = ((col + dir * step) % cols + cols) % cols * rows + row;
    if (j < items.length) return j;
  }
  return i;
}

function answer(key) {
  v3.console.print('|B0|07\x1b[?25h');
  v3.console.gotoxy(1, h);
  v3.prompt.answer(key);
}

// command reads a command's key, more than one character when a shortcut
// starts with what has been typed, and returns it, or '' when no command the
// caller may use matches. An exact key wins over a longer one.
function command(first) {
  if (first.length !== 1) return '';
  var key = first.toUpperCase();
  for (var n = 0; n < 32; n++) {
    var prefix = false, i, candidate;
    for (i = 0; i < items.length; i++) if (String(items[i].key).toUpperCase() === key) return key;
    for (i = 0; i < shortcuts.length; i++) {
      candidate = String(shortcuts[i]).toUpperCase();
      if (candidate === key) return key;
      if (candidate.indexOf(key) === 0) prefix = true;
    }
    if (!prefix) return '';
    // As at the board's own prompt, a key that starts a longer command
    // waits for the rest with no time limit.
    var next = v3.console.readkey();
    if (next === '\x10') return '^P';
    if (next.length !== 1 || next === '\r' || next === 'ESC') return '';
    key += next.toUpperCase();
  }
  return '';
}

// On a screen smaller than 80x24, or a menu too long for it, answer nothing:
// the menu shows its own screen and prompt.
if (w >= 80 && h >= 24 && items.length && bottom + 2 <= h) {
  v3.console.print('\x1b[?25l');
  v3.console.clear();
  drawLettering();
  drawWhere();
  frame(-1);
  for (var i = 0; i < items.length; i++) item(i, false);
  item(selected, true);
  center(bottom + 2, '|08ARROWS |07move  |08/  |14ENTER |07select  |08/  |07type a key to jump');
  for (;;) {
    var key = v3.console.readkey(80);
    if (!key) {
      if (!truecolor) continue;
      var elapsed = (Date.now() - startedAt) / 1000;
      tick = Math.floor(elapsed / 0.16);
      var lap = Math.floor(elapsed * 10) % 300;
      if (lap !== lastFrame && lap < 120) { lastFrame = lap; frame(lap); }
      if (tick !== lastTick) {
        lastTick = tick;
        for (var c = 0; c < columns; c++) {
          var warm = c * rows + (tick % rows);
          if (warm !== selected) item(warm, false);
        }
      }
      continue;
    }
    // Ctrl+P goes back to the previous menu, as at the board's own prompt.
    if (key === '\x10') { answer('^P'); break; }
    var cmd = command(key === 'ESC' ? 'Q' : key);
    if (cmd) { answer(cmd); break; }
    var next = selected;
    if (key === 'UP') next = (selected + items.length - 1) % items.length;
    else if (key === 'DOWN') next = (selected + 1) % items.length;
    else if (key === 'LEFT') next = across(selected, -1);
    else if (key === 'RIGHT') next = across(selected, 1);
    else if (key === '\r' || key === ' ') { answer(items[selected].key); break; }
    if (next !== selected) { item(selected, false); selected = next; item(selected, true); }
  }
  v3.console.print('\x1b[?25h');
}
